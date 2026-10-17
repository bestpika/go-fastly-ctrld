package app

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/probe"
)

func TestTaskOrderDependenciesAndSafeDeduplication(t *testing.T) {
	s := domain.DefaultSettings()
	entry := domain.DomainEntry{ID: "managed", Hostname: "release-assets.githubusercontent.com", Enabled: true}
	tasks, dependencies := testTasks(s, []domain.DomainEntry{entry}, nil, "test")
	if dependencies != 0 || tasks[0].entry.ID != entry.ID || !tasks[1].baseline || tasks[1].reuse != 0 {
		t.Fatal("未優先管理網域或未重用同域基準")
	}
	rule := domain.Rule{Hostname: entry.Hostname, Via: "151.101.8.49"}
	tasks, _ = testTasks(s, []domain.DomainEntry{entry}, map[string]domain.Rule{entry.Hostname: rule}, "test")
	if tasks[0].current == nil || tasks[0].current.Via != rule.Via || tasks[1].reuse != -1 {
		t.Fatal("獨立現用規則被去重省略")
	}
	entry.TestURL = "https://" + entry.Hostname + "/test.bin"
	tasks, _ = testTasks(s, []domain.DomainEntry{entry}, nil, "test")
	if tasks[1].reuse != -1 {
		t.Fatal("自訂檔案被當成基準成績")
	}
	entry.Hostname, entry.TestURL = "example.com", ""
	s.Probe.NoResourceMode = "shared"
	tasks, dependencies = testTasks(s, []domain.DomainEntry{entry}, nil, "test")
	if dependencies != 2 || tasks[0].entry.ID != "baseline-github" || tasks[1].entry.ID != "baseline-twitch" || tasks[2].entry.ID != entry.ID {
		t.Fatal("必要共享來源未優先完成")
	}
}

func TestNativePipelinePersistsRefreshedDNSAndBoundsResultExpiry(t *testing.T) {
	c, api := newTestCore(t, successfulProbe)
	c.nativeProbe = true
	c.probeEngine = localVerifier(t, nil)
	c.probeEngine.Lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("151.101.0.214")}}, nil
	}
	c.probeEngine.TCP = func(context.Context, string) (float64, error) { return 10, nil }
	initial := time.Now().Add(-30 * time.Second)
	c.dns.Now = func() time.Time { return initial }
	refreshes := 0
	c.probeEngine.RefreshDNS = func(ctx context.Context, entry domain.DomainEntry, r domain.RangeSnapshot) domain.DetectionResult {
		refreshes++
		c.dns.Now = time.Now
		return c.dns.Detect(ctx, entry, r)
	}
	s, _ := c.manager.Snapshot()
	s.Probe.Services, s.Probe.ScanMode = []string{}, "full"
	entry := domain.DomainEntry{ID: "managed", Hostname: "example.com", Enabled: true}
	ranges := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/23"}, FetchedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	ranges.Digest = domain.Digest(ranges.CIDRs)
	j := domain.Job{ID: domain.ID(), Kind: "test", Total: 1}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.probeEngine.BeginScanSession()
	defer c.probeEngine.EndScanSession()
	incomplete, err := c.runTasks(ctx, cancel, &j, s, ranges, testNetwork{}.Snapshot(ctx), &probe.Budget{}, []domain.DomainEntry{entry}, nil)
	if err != nil || incomplete || refreshes != 1 || j.Completed != 1 {
		t.Fatalf("原生流程沒有更新證據：%+v incomplete=%v err=%v", j, incomplete, err)
	}
	var detection domain.DetectionResult
	if err := c.cache.Read("detections/"+entry.ID+".json", &detection); err != nil {
		t.Fatal(err)
	}
	result := c.results[entry.ID]
	if !result.Complete || !detection.ObservedAt.After(initial) || !result.ExpiresAt.Equal(detection.ExpiresAt) || !c.detections[entry.ID].ObservedAt.Equal(detection.ObservedAt) {
		t.Fatal("保存舊 DNS 或結果期限超出最新 TTL")
	}
	if api.writes != 0 || api.deletes != 0 {
		t.Fatal("重新偵測擅自變更帳號規則")
	}
}

func TestElapsedTimeDoesNotRerunWholeMeasurementForJobReuse(t *testing.T) {
	c, api := newTestCore(t, successfulProbe)
	c.nativeProbe = true
	var heads atomic.Int32
	c.probeEngine = localVerifier(t, func(*http.Request) { heads.Add(1) })
	c.probeEngine.Lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("151.101.0.214")}}, nil
	}
	c.probeEngine.TCP = func(context.Context, string) (float64, error) { return 10, nil }
	refreshes := 0
	var expireBeforeAlias time.Time
	c.probeEngine.RefreshDNS = func(ctx context.Context, entry domain.DomainEntry, r domain.RangeSnapshot) domain.DetectionResult {
		refreshes++
		d := c.dns.Detect(ctx, entry, r)
		if refreshes == 1 {
			expireBeforeAlias = time.Now().Add(time.Second)
			d.ExpiresAt = expireBeforeAlias
		}
		return d
	}
	// 在第一份完整結果保存時等其短 TTL 到期，避免依賴排程速度或真實 DNS。
	c.cache.BeforeReplace = func() error {
		if !expireBeforeAlias.IsZero() {
			time.Sleep(max(0, time.Until(expireBeforeAlias)) + time.Millisecond)
			expireBeforeAlias = time.Time{}
		}
		return nil
	}
	s, _ := c.manager.Snapshot()
	s.Probe.Services, s.Probe.ScanMode = []string{}, "full"
	entries := []domain.DomainEntry{{ID: "source", Hostname: "example.com", Enabled: true}, {ID: "alias", Hostname: "example.com", Enabled: true}}
	ranges := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/23"}, FetchedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	ranges.Digest = domain.Digest(ranges.CIDRs)
	j := domain.Job{ID: domain.ID(), Kind: "test", Total: 2}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.probeEngine.BeginScanSession()
	defer c.probeEngine.EndScanSession()
	incomplete, err := c.runTasks(ctx, cancel, &j, s, ranges, testNetwork{}.Snapshot(ctx), &probe.Budget{}, entries, nil)
	source, alias := c.results["source"], c.results["alias"]
	if err != nil || incomplete || j.Completed != 2 || refreshes != 1 || heads.Load() != 2 || !alias.Complete || alias.ReusedFrom != source.ID || source.ID == alias.ID || alias.ExpiresAt.After(time.Now()) {
		t.Fatalf("時間造成同作業重測或錯誤拒絕：source=%+v alias=%+v heads=%d refreshes=%d err=%v", source, alias, heads.Load(), refreshes, err)
	}
	if api.writes != 0 || api.deletes != 0 {
		t.Fatal("重測變更了帳號規則")
	}
}

func TestLimitedBudgetReachesManagedDomainBeforeOptionalBaselines(t *testing.T) {
	var first string
	c, _ := newTestCore(t, func(ctx context.Context, e domain.DomainEntry, s domain.Settings, d domain.DetectionResult, r domain.RangeSnapshot, n domain.NetworkSnapshot, rule *domain.Rule, b *probe.Budget, progress func(int, int, string)) domain.TestResult {
		if first == "" {
			first = e.ID
		}
		result := successfulProbe(ctx, e, s, d, r, n, rule, b, progress)
		if !b.CanSample(8 << 20) {
			result.Complete, result.AutomaticEligible, result.BestIPv4 = false, false, ""
			return result
		}
		nbytes, release := b.Reserve(8 << 20)
		release(nbytes)
		return result
	})
	id := importTestDomain(t, c)
	started := c.StartTest(nil, 8<<20)
	if !started.OK {
		t.Fatal(started.Error)
	}
	job := waitJob(t, c, started.Data)
	if first != id || job.State != "partial" || job.ManagedCompleted != 1 || job.BaselinesCompleted != 12 || job.Bytes != 8<<20 {
		t.Fatalf("有限額度仍先餓死管理網域：%+v first=%s", job, first)
	}
	if !c.results[id].Complete {
		t.Fatal("管理網域未取得先行額度")
	}
}

func TestAutomaticCursorUsesActualManagedCountForEveryBaselineSelection(t *testing.T) {
	for _, services := range [][]string{{}, {"github"}, {"x"}, {"twitch"}, {"github", "twitch", "x"}} {
		c, _ := newTestCore(t, successfulProbe)
		s, _ := c.manager.Snapshot()
		s.Probe.Services = services
		j := domain.Job{ID: domain.ID(), Kind: "test", StartedAt: time.Now(), Completed: 2 + len(probe.SelectedBaselines(s.Probe, nil)), ManagedCompleted: 2, BaselinesCompleted: len(probe.SelectedBaselines(s.Probe, nil))}
		if !c.finishAutomaticTest(&j, s, 10) {
			t.Fatal("游標保存失敗")
		}
		next, _ := c.manager.Snapshot()
		if next.Cursor != 2 {
			t.Fatalf("基準數 %d 影響管理游標：%d", j.BaselinesCompleted, next.Cursor)
		}
	}
}

func TestWorkEstimateDoesNotAssumeDeduplicationWithoutCurrentRules(t *testing.T) {
	s := domain.DefaultSettings()
	base, _ := testEstimate(s, nil, domain.RangeSnapshot{})
	managed, _ := testEstimate(s, []domain.DomainEntry{{ID: "managed", Hostname: "release-assets.githubusercontent.com", Enabled: true}}, domain.RangeSnapshot{})
	if base <= 0 || managed <= base {
		t.Fatal("開始前工作量低估獨立現用規則比較")
	}
}

func TestJobReuseHasUniqueEvidenceAndDoesNotSkipCurrentRule(t *testing.T) {
	for _, current := range []bool{false, true} {
		t.Run(map[bool]string{false: "可安全重用", true: "保留獨立現用比較"}[current], func(t *testing.T) {
			calls, currentSeen := 0, false
			c, api := newTestCore(t, func(ctx context.Context, entry domain.DomainEntry, s domain.Settings, d domain.DetectionResult, r domain.RangeSnapshot, n domain.NetworkSnapshot, rule *domain.Rule, b *probe.Budget, progress func(int, int, string)) domain.TestResult {
				calls++
				if entry.Hostname == "release-assets.githubusercontent.com" && rule != nil {
					currentSeen = rule.Via == "151.101.8.49"
				}
				size, release := b.Reserve(1024)
				release(size)
				result := successfulProbe(ctx, entry, s, d, r, n, rule, b, progress)
				result.Bytes = size
				return result
			})
			preview := c.PreviewDomainImport("release-assets.githubusercontent.com", "paste", "")
			if !preview.OK || !c.ConfirmDomainImport(preview.Data.ID).OK {
				t.Fatal("匯入失敗")
			}
			_, entries := c.manager.Snapshot()
			entry := entries[0]
			if current {
				if !c.SetControlDToken("local-test-token").OK {
					t.Fatal("假權杖設定失敗")
				}
				s, _ := c.manager.Snapshot()
				if err := c.manager.SelectProfile(s.Revision, "profile"); err != nil {
					t.Fatal(err)
				}
				api.rules[entry.Hostname] = domain.Rule{Hostname: entry.Hostname, Via: "151.101.8.49", Safe: true}
			}
			started := c.StartTest(nil, 0)
			if !started.OK {
				t.Fatal(started.Error)
			}
			job := waitJob(t, c, started.Data)
			managed, baseline := c.results[entry.ID], c.results["baseline-github"]
			wantCalls := 12
			if current {
				wantCalls = 13
			}
			if job.State != "completed" || job.Completed != 13 || calls != wantCalls || job.Bytes != int64(wantCalls)*1024 || managed.ID == baseline.ID || !managed.AutomaticEligible || baseline.AutomaticEligible || !baseline.BaselineService {
				t.Fatalf("同作業證據／流量或資格錯誤：%+v managed=%+v baseline=%+v calls=%d", job, managed, baseline, calls)
			}
			if current && (!currentSeen || baseline.ReusedFrom != "" || baseline.Bytes != 1024) || !current && (baseline.ReusedFrom != managed.ID || baseline.Bytes != 0) {
				t.Fatal("現用比較被省略，或重用來源／流量錯誤")
			}
		})
	}
}
