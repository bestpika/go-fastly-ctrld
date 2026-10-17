package app

import (
	"context"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/dns"
	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/fastly"
	"github.com/bestpika/go-fastly-ctrld/internal/probe"
	rulesync "github.com/bestpika/go-fastly-ctrld/internal/sync"
)

func (c *Core) BuildSyncPreview(ids []string) domain.Result[domain.SyncPreview] {
	return c.BuildSyncPreviewContext(context.Background(), ids)
}
func (c *Core) BuildSyncPreviewContext(parent context.Context, ids []string) domain.Result[domain.SyncPreview] {
	s, ds := c.manager.Snapshot()
	entries, err := selected(ds, ids)
	if err != nil {
		return domain.Failure[domain.SyncPreview](err)
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	n := c.net.Snapshot(ctx)
	ranges, err := c.fastly.Get(ctx)
	if err != nil {
		return domain.Failure[domain.SyncPreview](readFailure(parent, err))
	}
	rules, groups, err := c.api.Rules(ctx, s.ProfileID)
	if err != nil {
		return domain.Failure[domain.SyncPreview](readFailure(parent, err))
	}
	if !domain.RegularFolder(groups, s.DestinationFolderID) {
		return domain.Failure[domain.SyncPreview](domain.Fail("CONFLICT", "目的資料夾無效，請重新選擇"))
	}
	c.gate.Lock()
	results := copyOf(c.results)
	c.gate.Unlock()
	originals := map[string]domain.DetectionResult{}
	if s.ForceWrite {
		for _, entry := range entries {
			if r, ok := results[entry.ID]; ok && r.Complete && r.BestIPv4 == "" && r.BestIPv6 == "" {
				originals[entry.ID] = c.dns.Detect(ctx, entry, ranges)
			}
		}
	}
	p := rulesync.Build(s, n, entries, results, ranges, rules, groups, originals)
	for index := range p.Items {
		i := &p.Items[index]
		if i.Applicable && i.OriginalDNS != nil {
			if err := c.verifyOriginal(ctx, *i, s, n, ranges, *i.OriginalDNS); err != nil {
				i.Applicable = false
				i.Reason = domain.Failure[bool](err).Error.Message
			}
		}
		if result, ok := results[i.DomainID]; ok {
			if i.Applicable {
				if err := c.validateReference(result, s, ranges, n); err != nil {
					i.Applicable = false
					i.Reason = "共享參考失效，請重新測試"
				}
			}
			if i.Applicable && !result.AutomaticEligible {
				i.Reason += "；僅限手動"
			}
			if result.Mode == "shared-reference" {
				i.Reason += "；共享路線參考"
			}
		}
	}
	c.gate.Lock()
	current, _ := c.manager.Snapshot()
	if ctx.Err() != nil {
		c.gate.Unlock()
		return domain.Failure[domain.SyncPreview](readFailure(parent, ctx.Err()))
	}
	if current.Revision != s.Revision {
		c.gate.Unlock()
		return domain.Failure[domain.SyncPreview](domain.Fail("CONFLICT", "預覽期間設定已變更，請重新讀取"))
	}
	for _, item := range p.Items {
		if item.ResultID != "" {
			r, exists := c.results[item.DomainID]
			if !exists || r.ID != item.ResultID {
				c.gate.Unlock()
				return domain.Failure[domain.SyncPreview](domain.Fail("PREVIEW_EXPIRED", "測速結果已刪除或更新，請重新建立預覽"))
			}
		}
	}
	c.previews[p.ID] = copyOf(p)
	c.gate.Unlock()
	return domain.Success(uiPreview(p))
}
func (c *Core) BuildRestorePreview(id string) domain.Result[domain.SyncPreview] {
	return c.BuildRestorePreviewContext(context.Background(), id)
}
func (c *Core) BuildRestorePreviewContext(parent context.Context, id string) domain.Result[domain.SyncPreview] {
	c.gate.Lock()
	j, ok := c.jobs[id]
	c.gate.Unlock()
	if !ok {
		return domain.Failure[domain.SyncPreview](domain.Fail("INVALID_INPUT", "找不到歷史作業"))
	}
	s, _ := c.manager.Snapshot()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	n := c.net.Snapshot(ctx)
	rules, _, err := c.api.Rules(ctx, s.ProfileID)
	if err != nil {
		return domain.Failure[domain.SyncPreview](readFailure(parent, err))
	}
	p := rulesync.BuildRestore(s, n, j, rules)
	c.gate.Lock()
	current, _ := c.manager.Snapshot()
	if parent.Err() != nil || current.Revision != s.Revision {
		c.gate.Unlock()
		return domain.Failure[domain.SyncPreview](readFailure(parent, domain.Fail("CONFLICT", "讀取期間設定已改變，請重新建立復原預覽")))
	}
	c.previews[p.ID] = copyOf(p)
	c.gate.Unlock()
	return domain.Success(uiPreview(p))
}
func (c *Core) ApplySyncPreview(id string) domain.Result[string] {
	return c.apply(id, "apply", "manual")
}
func (c *Core) ApplyRestorePreview(id string) domain.Result[string] {
	return c.apply(id, "restore", "manual")
}
func (c *Core) apply(id, kind, trigger string) domain.Result[string] {
	c.gate.Lock()
	p, ok := c.previews[id]
	s, _ := c.manager.Snapshot()
	if !ok || p.Kind != kind {
		c.gate.Unlock()
		return domain.Failure[string](domain.Fail("PREVIEW_EXPIRED", "預覽不存在，請重新建立"))
	}
	if c.stopping || c.recovery || c.writerID != "" || c.testID != "" {
		c.gate.Unlock()
		return domain.Failure[string](domain.Fail("STATE_RECOVERY_REQUIRED", "請先等待其他作業或核對未決寫入"))
	}
	if trigger == "automatic" && c.automationError != "" {
		c.gate.Unlock()
		return domain.Failure[string](domain.Fail("STORAGE_ERROR", "自動執行已因安全紀錄故障暫停"))
	}
	if s.Revision != p.SettingsRevision || s.CredentialRevision != p.CredentialRevision || s.ProfileID != p.ProfileID {
		c.gate.Unlock()
		return domain.Failure[string](domain.Fail("PREVIEW_EXPIRED", "設定或帳號已變更，請重新預覽"))
	}
	jobID := domain.ID()
	ctx, cancel := context.WithCancel(context.Background())
	c.writerID = jobID
	c.writerCancel = cancel
	c.writerDone = make(chan struct{})
	delete(c.previews, id)
	c.gate.Unlock()
	if trigger == "automatic" {
		if err := c.armAutomaticCheckpoint(); err != nil {
			cancel()
			c.releaseWriter()
			return domain.Failure[string](err)
		}
	}
	go func() {
		defer c.releaseWriter()
		defer cancel()
		c.gate.Lock()
		generation := c.automationGeneration
		c.gate.Unlock()
		e := rulesync.Engine{API: c.api, Store: c.data, Validate: c.validateItem, Changed: c.publish}
		j := e.Apply(ctx, p, domain.Job{ID: jobID, Trigger: trigger})
		c.publish(j)
		if trigger == "automatic" {
			current, _ := c.manager.Snapshot()
			a := current.Automation
			if a.LastChanged == nil {
				a.LastChanged = map[string]time.Time{}
			}
			for _, i := range j.Items {
				if i.State == "verified" {
					a.LastChanged[i.Preview.DomainID] = time.Now().UTC()
				}
			}
			if err := c.manager.RecordAutomation(a, current.Cursor); err != nil {
				c.blockAutomatic("冷卻紀錄保存失敗，已暫停自動執行")
				j.Message = "套用成功，冷卻紀錄保存失敗；請核對後重新授權"
				_ = c.saveJob(j)
				return
			}
			if j.State == "failed" || j.State == "partial" || j.State == "needs_recovery" {
				c.blockAutomatic("自動套用未完整成功；請核對結果後重新確認授權")
				_ = c.changed(c.manager.Pause())
			} else if err := c.clearAutomaticCheckpoint(generation, false); err != nil {
				j.Message = "帳號核對完成，但自動暫停紀錄無法更新"
				_ = c.saveJob(j)
			}
		}
	}()
	return domain.Success(jobID)
}

func (c *Core) validateItem(ctx context.Context, p domain.SyncPreview, i domain.PreviewItem) error {
	s, ds := c.manager.Snapshot()
	if s.Revision != p.SettingsRevision || s.ProfileID != p.ProfileID || s.CredentialRevision != p.CredentialRevision {
		return domain.Fail("PREVIEW_EXPIRED", "設定或帳號變更")
	}
	n := c.net.Snapshot(ctx)
	if n.Fingerprint != p.NetworkFingerprint || n.Error != "" {
		return domain.Fail("NETWORK_CHANGED", "網路狀態已變更或無法確認")
	}
	if p.Kind == "restore" {
		return nil
	}
	var entry domain.DomainEntry
	for _, d := range ds {
		if d.ID == i.DomainID && d.Enabled {
			entry = d
			break
		}
	}
	if entry.ID == "" {
		return domain.Fail("INVALID_INPUT", "網域已停用或移除")
	}
	c.gate.Lock()
	r, ok := c.results[i.DomainID]
	c.gate.Unlock()
	if !ok || r.BaselineService || !r.Complete || r.StrategyVersion != domain.ProbeStrategyVersion || r.ID != i.ResultID || r.DomainID != entry.ID || r.Hostname != entry.Hostname || r.SettingsRevision != s.Revision || r.NetworkFingerprint != n.Fingerprint {
		return domain.Fail("PREVIEW_EXPIRED", "測試已失效")
	}
	ranges, err := c.fastly.Get(ctx)
	if err != nil {
		return err
	}
	if !fastly.Valid(ranges, time.Now()) {
		return domain.Fail("NO_VALID_RANGES", "官方網段資料無效")
	}
	if err = c.validateReference(r, s, ranges, n); err != nil {
		return err
	}
	if i.After == nil || i.After.Do != 2 || i.After.Status != 1 {
		return domain.Fail("INVALID_INPUT", "套用目標不是有效指定目標規則")
	}
	if i.ForceWrite != s.ForceWrite || i.OriginalDNS != nil && (!s.ForceWrite || r.BestIPv4 != "" || r.BestIPv6 != "") {
		return domain.Fail("PREVIEW_EXPIRED", "強制寫入設定或原始 DNS 用途不一致")
	}
	if i.OriginalDNS == nil && (r.BestIPv4 == "" && r.BestIPv6 == "" || r.BestIPv4 != "" && i.After.Via != r.BestIPv4 || r.BestIPv6 != "" && i.After.ViaV6 != r.BestIPv6) {
		return domain.Fail("UNSAFE_TARGET", "預覽目標不符合測試選定結果")
	}
	if r.BestIPv4 != "" && fastly.Match(r.BestIPv4, ranges) == "" {
		return domain.Fail("UNSAFE_TARGET", "IPv4 候選已不在官方網段")
	}
	if r.BestIPv6 != "" && (!s.Probe.EnableIPv6 || !n.IPv6 || fastly.Match(r.BestIPv6, ranges) == "") {
		return domain.Fail("UNSAFE_TARGET", "IPv6 候選已失效")
	}
	d := c.dns.Detect(ctx, entry, ranges)
	if i.OriginalDNS != nil {
		if err = c.verifyOriginal(ctx, i, s, n, ranges, d); err != nil {
			return err
		}
	}
	c.gate.Lock()
	automatic := c.jobs[c.writerID].Trigger == "automatic"
	c.gate.Unlock()
	if automatic && r.Mode == "shared-reference" && !s.Automation.AllowSharedReference {
		return domain.Fail("INVALID_INPUT", "未授權共享參考自動套用")
	}
	manualMixed := !automatic && r.DNSClassification == "mixed" && d.Classification == "mixed" && len(d.Candidates) > 0
	for _, source := range d.Sources {
		if source.Error != "" {
			manualMixed = false
		}
	}
	if d.Classification != "confirmed" && !manualMixed {
		return domain.Fail("NETWORK_CHANGED", "套用前雙來源未一致確認 Fastly，保留目前規則")
	}
	if err = c.cache.Write("detections/"+entry.ID+".json", d); err != nil {
		return domain.Fail("STORAGE_ERROR", "最新 DNS 證據無法安全保存")
	}
	if i.OriginalDNS == nil {
		engine := c.probeEngine
		if engine == nil {
			engine = probe.New()
		}
		for _, ip := range []string{r.BestIPv4, r.BestIPv6} {
			if ip != "" {
				if err = engine.VerifyTarget(ctx, entry, ip, s, ranges); err != nil {
					return err
				}
			}
		}
	}
	// DNS 與 TLS 驗證後再核實際執行環境，不以資料年齡判定資格。
	latestSettings, _ := c.manager.Snapshot()
	latestNetwork := c.net.Snapshot(ctx)
	if ctx.Err() != nil || latestSettings.Revision != s.Revision || latestNetwork.Fingerprint != n.Fingerprint || latestNetwork.Error != "" || !fastly.Valid(ranges, time.Now()) {
		return domain.Fail("PREVIEW_EXPIRED", "作業已取消，或設定與網路已變更")
	}
	c.gate.Lock()
	c.detections[entry.ID] = copyOf(d)
	c.gate.Unlock()
	return nil
}

func (c *Core) verifyOriginal(ctx context.Context, i domain.PreviewItem, s domain.Settings, n domain.NetworkSnapshot, ranges domain.RangeSnapshot, fresh domain.DetectionResult) error {
	entry := domain.DomainEntry{ID: i.DomainID, Hostname: i.Hostname}
	via, v6, _, err := dns.OriginalTarget(*i.OriginalDNS, entry, ranges, s.Probe.EnableIPv6 && n.IPv6, time.Now())
	if err != nil {
		return err
	}
	currentVia, currentV6, ips, err := dns.OriginalTarget(fresh, entry, ranges, s.Probe.EnableIPv6 && n.IPv6, time.Now())
	if err != nil {
		return err
	}
	if i.After == nil || i.After.Via != via || v6 != "" && i.After.ViaV6 != v6 || currentVia != via || currentV6 != v6 || originalCNAMEs(*i.OriginalDNS) != originalCNAMEs(fresh) {
		return domain.Fail("NETWORK_CHANGED", "原始 DNS 目標已變更，請重新預覽")
	}
	engine := c.probeEngine
	if engine == nil {
		engine = probe.New()
	}
	for _, ip := range ips {
		if err := engine.VerifyOriginalTarget(ctx, entry, ip, s, ranges); err != nil {
			return err
		}
	}
	return nil
}

// 第一跳相同仍可能換了後續鏈結；確保最後的跨規則檢查使用同一條鏈。
func originalCNAMEs(d domain.DetectionResult) string {
	links := map[string]map[string]string{}
	for _, source := range d.Sources {
		links[source.Source] = map[string]string{}
		for _, record := range source.Records {
			if record.Type == "CNAME" {
				links[source.Source][record.Name] = record.Value
			}
		}
	}
	return domain.Digest(links)
}
