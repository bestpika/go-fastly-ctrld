package app

import (
	"context"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/probe"
)

func selected(entries []domain.DomainEntry, ids []string) ([]domain.DomainEntry, error) {
	allowed := map[string]bool{}
	for _, id := range ids {
		if allowed[id] {
			return nil, domain.Fail("INVALID_INPUT", "網域選擇重複")
		}
		allowed[id] = true
	}
	out := []domain.DomainEntry{}
	for _, e := range entries {
		if e.Enabled && (len(ids) == 0 || allowed[e.ID]) {
			out = append(out, e)
			delete(allowed, e.ID)
		}
	}
	if len(allowed) > 0 || len(out) == 0 {
		return nil, domain.Fail("INVALID_INPUT", "請選擇存在且已啟用的網域")
	}
	return out, nil
}

func (c *Core) StartDetection(ids []string) domain.Result[string] {
	return c.startTest(ids, -1, "manual", "detect")
}
func (c *Core) StartTest(ids []string, budget int64) domain.Result[string] {
	return c.startTest(ids, budget, "manual", "test")
}
func (c *Core) startTest(ids []string, budget int64, trigger, kind string) domain.Result[string] {
	c.gate.Lock()
	s, ds := c.manager.Snapshot()
	entries, err := selected(ds, ids)
	if kind == "test" && len(ids) == 0 && len(entries) == 0 && len(probe.SelectedBaselines(s.Probe, entries)) > 0 {
		err = nil
	}
	if err != nil {
		c.gate.Unlock()
		return domain.Failure[string](err)
	}
	if c.stopping || c.testID != "" || c.writerID != "" || c.recovery {
		c.gate.Unlock()
		return domain.Failure[string](domain.Fail("STATE_RECOVERY_REQUIRED", "已有作業或未決寫入，不能重複啟動"))
	}
	if budget == -1 {
		budget = s.Automation.BudgetBytes
	}
	if !domain.ValidBudget(budget) {
		c.gate.Unlock()
		return domain.Failure[string](domain.Fail("INVALID_INPUT", "下載預算須為 0 不限，或 1 MB 至 100 GB"))
	}
	if trigger == "automatic" {
		if !s.Automation.Enabled || c.automationError != "" {
			c.gate.Unlock()
			return domain.Failure[string](domain.Fail("INVALID_INPUT", "自動功能未啟用"))
		}
		if len(entries) > 0 {
			cursor := s.Cursor % len(entries)
			entries = append(entries[cursor:], entries[:cursor]...)
		}
	}
	id := domain.ID()
	ctx, cancel := probe.JobContext(context.Background(), s.Probe.JobSeconds)
	c.testID = id
	c.testCancel = cancel
	c.testDone = make(chan struct{})
	c.gate.Unlock()
	j := domain.Job{ID: id, Kind: kind, Trigger: trigger, State: "queued", StartedAt: time.Now().UTC(), Total: len(entries), Budget: budget, ProfileID: s.ProfileID, CredentialRevision: s.CredentialRevision, Items: []domain.JournalItem{}, ResultIDs: []string{}}
	if kind == "test" {
		j.Total += len(probe.SelectedBaselines(s.Probe, entries))
		j.EstimatedBytes, j.EstimatedSeconds = testEstimate(s, entries, domain.RangeSnapshot{})
	}
	if err = c.saveJob(j); err != nil {
		if trigger == "automatic" {
			c.blockAutomatic("作業前置紀錄保存失敗；自動執行已暫停")
		}
		cancel()
		c.gate.Lock()
		c.testID = ""
		c.testCancel = nil
		close(c.testDone)
		c.testDone = nil
		c.gate.Unlock()
		return domain.Failure[string](domain.Fail("STORAGE_ERROR", "無法保存作業，未開始測試"))
	}
	if trigger == "automatic" {
		err = c.armAutomaticCheckpoint()
		if err == nil {
			a := s.Automation
			a.LastRun = j.StartedAt
			err = c.manager.RecordAutomation(a, s.Cursor)
		}
		if err != nil {
			c.blockAutomatic("排程前置紀錄保存失敗，未開始測試；請重新確認授權")
			cancel()
			j.State = "failed"
			j.Message = "排程前置紀錄保存失敗，未開始測試"
			j.FinishedAt = time.Now().UTC()
			_ = c.saveJob(j)
			c.releaseTest(j.ID)
			return domain.Failure[string](domain.Fail("STORAGE_ERROR", "排程前置紀錄無法安全保存"))
		}
	}
	go c.runTest(ctx, cancel, j, s, entries)
	return domain.Success(id)
}

func (c *Core) runTest(ctx context.Context, cancel context.CancelFunc, j domain.Job, s domain.Settings, entries []domain.DomainEntry) {
	defer cancel()
	defer c.releaseTest(j.ID)
	if j.Trigger == "automatic" {
		defer func() {
			ok := c.finishAutomaticTest(&j, s, len(entries))
			c.releaseTest(j.ID)
			if ok && ctx.Err() == nil && j.State != "failed" {
				// 只套用本作業確實完成的管理結果，不借未開始網域的舊快取。
				fresh := []domain.DomainEntry{}
				c.gate.Lock()
				for _, entry := range entries {
					result := c.results[entry.ID]
					for _, id := range j.ResultIDs {
						if result.ID == id && result.Complete && !result.BaselineService {
							fresh = append(fresh, entry)
							break
						}
					}
				}
				c.gate.Unlock()
				c.autoApply(fresh, s)
			}
		}()
	}
	j.State, j.Phase = "running", "preparing"
	if c.probeEngine != nil {
		c.probeEngine.BeginScanSession()
		defer c.probeEngine.EndScanSession()
	}
	if err := c.saveJob(j); err != nil {
		j.State = "failed"
		j.Message = "作業紀錄保存失敗，未開始測試"
		if j.Trigger == "automatic" {
			c.blockAutomatic(j.Message)
		}
		return
	}
	ranges, err := c.fastly.Get(ctx)
	if err != nil {
		j.State = "failed"
		j.Message = "沒有有效官方網段，未繼續測試"
		j.FinishedAt = time.Now().UTC()
		_ = c.saveJob(j)
		return
	}
	n := c.net.Snapshot(ctx)
	c.updateNetwork(n)
	rules := map[string]domain.Rule{}
	budget := &probe.Budget{Limit: j.Budget}
	if j.Kind == "test" {
		j.EstimatedBytes, j.EstimatedSeconds = testEstimate(s, entries, ranges)
	}
	if j.Kind == "test" && len(entries) > 0 && !(j.Trigger == "automatic" && s.Automation.TestOnly) && j.State != "failed" && s.ProfileID != "" && c.manager.CredentialStatus().Saved {
		rules, _, err = c.api.Rules(ctx, s.ProfileID)
		if err != nil {
			j.State = "failed"
			j.Message = "現用規則讀取失敗，未開始測試；保留既有規則"
			j.FinishedAt = time.Now().UTC()
			_ = c.saveJob(j)
			if j.Trigger == "automatic" {
				c.blockAutomatic("帳號規則無法安全讀取；請核對權杖後重新確認自動授權")
				_ = c.manager.Pause()
			}
			return
		}
	}
	incomplete, err := c.runTasks(ctx, cancel, &j, s, ranges, n, budget, entries, rules)
	if err != nil {
		j.State, j.Message = "failed", "測試結果或進度證據保存失敗；未自動套用"
	}
	if j.State != "failed" {
		if ctx.Err() != nil {
			j.State = "cancelled"
			j.CancelRequested = true
			j.Message = "測試已取消；尚未套用"
			if ctx.Err() == context.DeadlineExceeded {
				j.Message = "已達測試總時間上限；尚未套用"
			}
		} else if j.Completed < j.Total || incomplete {
			j.State = "partial"
			j.Message = "部分測試未完成，請查看網域詳情"
		} else {
			j.State = "completed"
			j.Message = "測試完成，尚未套用"
		}
	}
	j.FinishedAt = time.Now().UTC()
	j.Phase = "finished"
	j.Bytes = budget.Used()
	if err := c.saveJob(j); err != nil {
		j.State = "failed"
		j.Message = "測試結果紀錄保存失敗，未自動套用"
		if j.Trigger == "automatic" {
			c.blockAutomatic(j.Message)
		}
		c.publish(j)
	}
}

func (c *Core) updateNetwork(n domain.NetworkSnapshot) {
	c.gate.Lock()
	changed := c.network.Fingerprint != n.Fingerprint
	c.network = copyOf(n)
	c.gate.Unlock()
	if changed {
		c.event("network:changed", nil)
	}
}

func (c *Core) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		schedule := scheduleState{}
		for {
			if ctx.Err() != nil {
				return
			}
			now := time.Now()
			n := c.net.Snapshot(ctx)
			c.gate.Lock()
			changed := c.network.Fingerprint != "" && c.network.Fingerprint != n.Fingerprint
			if changed && c.testCancel != nil {
				c.testCancel()
			}
			c.gate.Unlock()
			c.updateNetwork(n)
			schedule.observe(now, n.Fingerprint)
			s, _ := c.manager.Snapshot()
			if schedule.ready(now, s.Automation, n.Error) {
				c.gate.Lock()
				free := !c.stopping && !c.recovery && c.automationError == "" && c.testID == "" && c.writerID == ""
				c.gate.Unlock()
				if free {
					ids := []string{}
					for _, g := range s.Automation.Grants {
						ids = append(ids, g.DomainID)
					}
					if s.Automation.TestOnly {
						ids = []string{}
					}
					r := c.startTest(ids, s.Automation.BudgetBytes, "automatic", "test")
					if r.OK {
						schedule.pending = false
					}
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (c *Core) autoApply(entries []domain.DomainEntry, captured domain.Settings) {
	c.gate.Lock()
	blocked := c.automationError != ""
	c.gate.Unlock()
	if blocked {
		return
	}
	s, _ := c.manager.Snapshot()
	if !s.Automation.Enabled || s.Automation.TestOnly || s.Revision != captured.Revision || len(entries) == 0 {
		return
	}
	ids := []string{}
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	r := c.BuildSyncPreview(ids)
	if !r.OK {
		c.blockAutomatic("自動預覽失敗，請核對帳號與測試後重新授權")
		_ = c.manager.Pause()
		return
	}
	c.gate.Lock()
	p := copyOf(c.previews[r.Data.ID])
	c.gate.Unlock()
	c.gate.Lock()
	results := copyOf(c.results)
	c.gate.Unlock()
	limitAutomatic(&p, s.Automation, results, time.Now())
	count := 0
	for _, i := range p.Items {
		if i.Applicable {
			count++
		}
	}
	if count == 0 {
		return
	}
	c.gate.Lock()
	c.previews[p.ID] = p
	c.gate.Unlock()
	_ = c.apply(p.ID, "apply", "automatic")
}

func (c *Core) CleanExpiredCaches() domain.Result[int] {
	n, err := c.cache.CleanExpired(time.Now())
	if err != nil {
		return domain.Failure[int](domain.Fail("SCHEMA_UNSUPPORTED", "保留未知快取，清理未完成"))
	}
	c.gate.Lock()
	for id, d := range c.detections {
		if !d.ExpiresAt.After(time.Now()) {
			delete(c.detections, id)
		}
	}
	c.gate.Unlock()
	c.event("settings:changed", nil)
	return domain.Success(n)
}
