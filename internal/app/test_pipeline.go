package app

import (
	"context"
	"net/netip"
	"sync"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/fastly"
	"github.com/bestpika/go-fastly-ctrld/internal/probe"
)

type testTask struct {
	entry     domain.DomainEntry
	current   *domain.Rule
	baseline  bool
	reuse     int
	detection domain.DetectionResult
	prepared  *probe.Prepared
}

// 依賴先完成；其餘從管理網域起交錯。自動作業的管理順序已按實際處理游標輪替。
func testTasks(s domain.Settings, entries []domain.DomainEntry, rules map[string]domain.Rule, kind string) ([]testTask, int) {
	baselines := []domain.DomainEntry{}
	if kind == "test" {
		baselines = probe.SelectedBaselines(s.Probe, entries)
	}
	tasks := []testTask{}
	shared := false
	for _, entry := range entries {
		shared = shared || probe.WantsReference(entry, s.Probe)
	}
	remaining := []domain.DomainEntry{}
	for _, entry := range baselines {
		if shared && (entry.ID == "baseline-github" || entry.ID == "baseline-twitch") {
			tasks = append(tasks, testTask{entry: entry, baseline: true, reuse: -1})
		} else {
			remaining = append(remaining, entry)
		}
	}
	dependencies := len(tasks)
	if len(remaining) > 0 {
		offset := s.Cursor % len(remaining)
		remaining = append(remaining[offset:], remaining[:offset]...)
	}
	for i := 0; i < max(len(entries), len(remaining)); i++ {
		if i < len(entries) {
			task := testTask{entry: entries[i], reuse: -1}
			if rule, ok := rules[task.entry.Hostname]; ok {
				task.current = &rule
			}
			tasks = append(tasks, task)
		}
		if i < len(remaining) {
			tasks = append(tasks, testTask{entry: remaining[i], baseline: true, reuse: -1})
		}
	}
	// 只重用同作業、同域、無自訂檔、無獨立現用規則、非共享目標的完整資格與樣本。
	seen := map[string]int{}
	for i := range tasks {
		t := &tasks[i]
		if t.current != nil || t.entry.TestURL != "" || probe.WantsReference(t.entry, s.Probe) {
			continue
		}
		if first, ok := seen[t.entry.Hostname]; ok {
			t.reuse = first
		} else {
			seen[t.entry.Hostname] = i
		}
	}
	return tasks, dependencies
}

func (c *Core) runTasks(ctx context.Context, cancel context.CancelFunc, j *domain.Job, s domain.Settings, ranges domain.RangeSnapshot, n domain.NetworkSnapshot, budget *probe.Budget, entries []domain.DomainEntry, rules map[string]domain.Rule) (bool, error) {
	tasks, dependencies := testTasks(s, entries, rules, j.Kind)
	var reference *domain.SharedReference
	results := make([]domain.TestResult, len(tasks))
	incomplete := false
	saveDetection := func(t *testTask) error {
		if err := c.cache.Write("detections/"+t.entry.ID+".json", t.detection); err != nil {
			return err
		}
		c.gate.Lock()
		c.detections[t.entry.ID] = copyOf(t.detection)
		c.gate.Unlock()
		return nil
	}
	var progressMu sync.Mutex
	progress := func(done, total int, host string) {
		progressMu.Lock()
		defer progressMu.Unlock()
		j.ProbeCompleted, j.ProbeTotal, j.CurrentHostname, j.Bytes = done, total, host, budget.Used()
		c.publish(*j)
	}
	for start := 0; start < len(tasks) && ctx.Err() == nil; {
		end := min(start+4, len(tasks))
		if start < dependencies {
			end = dependencies
		}
		j.Phase, j.Message = "qualification", "跨網域資格探測；完成後依序下載"
		j.ProbeCompleted, j.ProbeTotal = 0, 0
		c.publish(*j)
		var wg sync.WaitGroup
		for i := start; i < end; i++ {
			if tasks[i].reuse >= 0 {
				continue
			}
			wg.Go(func() {
				t := &tasks[i]
				t.detection = c.dns.Detect(ctx, t.entry, ranges)
				if j.Kind == "test" && c.nativeProbe {
					scanDetection := copyOf(t.detection)
					c.gate.Lock()
					prior, hasPrior := c.results[t.entry.ID]
					c.gate.Unlock()
					if hasPrior && prior.NetworkFingerprint == n.Fingerprint && prior.SettingsRevision == s.Revision {
						for _, candidate := range prior.Candidates {
							if !candidate.Baseline && fastly.Match(candidate.IP, ranges) != "" {
								scanDetection.Candidates = append(scanDetection.Candidates, candidate.IP)
							}
						}
					}
					engine := c.probeEngine.Fork(progress)
					t.prepared = engine.PrepareWithReference(ctx, t.entry, s, scanDetection, ranges, n, t.current, reference, true)
				}
			})
		}
		wg.Wait()
		for i := start; i < end && ctx.Err() == nil; i++ {
			t := &tasks[i]
			if t.reuse >= 0 {
				t.detection = copyOf(tasks[t.reuse].detection)
				t.detection.DomainID = t.entry.ID
			}
			j.CurrentHostname, j.ProbeCompleted, j.ProbeTotal = t.entry.Hostname, 0, 0
			if j.Kind == "test" {
				j.Phase, j.Message = "connection", "整理自身連線與延遲結果"
				if probe.HasDownload(t.entry) {
					j.Phase, j.Message = "download", "依序進行三輪自身下載"
				}
				if t.reuse >= 0 {
					j.Message = "重用同作業量測，流量不重計"
				}
				c.publish(*j)
				var result domain.TestResult
				if t.reuse >= 0 {
					result = copyOf(results[t.reuse])
					result.ReusedFrom = result.ID
					result.ID, result.DomainID, result.Bytes = domain.ID(), t.entry.ID, 0
				} else if c.nativeProbe {
					result = c.probeEngine.Fork(progress).Finish(ctx, t.prepared, budget)
					t.detection = t.prepared.Detection()
				} else {
					result = c.runProbe(ctx, t.entry, s, t.detection, ranges, n, t.current, budget, progress)
				}
				latestSettings, _ := c.manager.Snapshot()
				latestNetwork := c.net.Snapshot(ctx)
				if ctx.Err() != nil || latestNetwork.Fingerprint != n.Fingerprint || latestNetwork.Error != n.Error || latestSettings.Revision != s.Revision {
					result.Complete, result.AutomaticEligible = false, false
					result.BestIPv4, result.BestIPv6 = "", ""
					switch {
					case ctx.Err() == context.DeadlineExceeded:
						result.Reason = "已達測試總時間上限，結果不可套用"
					case ctx.Err() != nil:
						result.Reason = "測試已取消，結果不可套用"
					case latestSettings.Revision != s.Revision:
						result.Reason = "測試途中設定已變更，結果不可套用"
					default:
						result.Reason = "測試途中網路已變更，結果不可套用"
					}
					cancel()
				}
				results[i] = copyOf(result)
				result.BaselineService = t.baseline
				if t.baseline {
					result.AutomaticEligible = false
				}
				if err := saveDetection(t); err != nil {
					return true, err
				}
				if err := c.cache.Write("tests/"+t.entry.ID+".json", result); err != nil {
					return true, err
				}
				c.gate.Lock()
				c.results[t.entry.ID] = copyOf(result)
				c.gate.Unlock()
				j.ResultIDs = append(j.ResultIDs, result.ID)
				incomplete = incomplete || !result.Complete
			} else if err := saveDetection(t); err != nil {
				return true, err
			}
			j.Completed++
			if t.baseline {
				j.BaselinesCompleted++
			} else {
				j.ManagedCompleted++
			}
			j.Bytes = budget.Used()
			if err := c.saveJob(*j); err != nil {
				return true, err
			}
		}
		if end == dependencies && dependencies > 0 && ctx.Err() == nil {
			ref, err := probe.ReferenceFromBaselines(results[:dependencies], s, ranges, n)
			if err == nil {
				if err = c.cache.Write("references/"+probe.ReferenceKey(s, ranges, n)+".json", ref); err != nil {
					return true, err
				}
				reference = &ref
			}
		}
		start = end
	}
	return incomplete || j.Completed < j.Total, nil
}

// 估計值只作工作量提示：不含協定開銷，慢速串流與逐次 I/O 可讓實際時間更長。
func testEstimate(s domain.Settings, entries []domain.DomainEntry, ranges domain.RangeSnapshot) (int64, int64) {
	tasks, _ := testTasks(s, entries, nil, "test")
	var bytes, seconds int64
	families := int64(1)
	if s.Probe.EnableIPv6 {
		families = 2
	}
	for _, task := range tasks {
		// 尚未讀取現用規則，不假設管理網域可與基準去重。
		if probe.HasDownload(task.entry) {
			samples := int64(s.Probe.Candidates)*families + 16
			bytes += samples*int64(s.Probe.Rounds)*(8<<20) + (8 << 20)
			seconds += samples * int64(s.Probe.Rounds) * 12
		}
	}
	// 尚未取得官方池時不假造掃描時間。這是 timeout 工作量參考，不是完成期限保證。
	var blocks int64
	for _, cidr := range ranges.CIDRs {
		if prefix, err := netip.ParsePrefix(cidr); err == nil && prefix.Addr().Is4() {
			blocks += int64(1) << max(0, 24-prefix.Bits())
		}
	}
	workers, perRoute := int64(16), int64(18)
	if s.Probe.ScanMode == "full" {
		workers, perRoute = 48, 12
	}
	seconds += ((blocks*int64(len(tasks))+workers-1)/workers)*perRoute + int64(len(tasks))*15
	return bytes, seconds
}
