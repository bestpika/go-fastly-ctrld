package app

import (
	"context"
	"sync"
	"testing"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/probe"
)

func TestEmptyListAlwaysRunsTwelveBaselinesWithoutAccountWrites(t *testing.T) {
	var mu sync.Mutex
	seen := []string{}
	runner := func(ctx context.Context, e domain.DomainEntry, s domain.Settings, d domain.DetectionResult, r domain.RangeSnapshot, n domain.NetworkSnapshot, current *domain.Rule, b *probe.Budget, progress func(int, int, string)) domain.TestResult {
		mu.Lock()
		seen = append(seen, e.Hostname)
		mu.Unlock()
		return successfulProbe(ctx, e, s, d, r, n, current, b, progress)
	}
	c, api := newTestCore(t, runner)
	if result := c.DeleteControlDToken(); !result.OK {
		t.Fatal(result.Error)
	}
	for range 2 {
		started := c.StartTest(nil, 100_000_000)
		if !started.OK {
			t.Fatal(started.Error)
		}
		job := waitJob(t, c, started.Data)
		if job.Total != 12 || job.Completed != 12 {
			t.Fatal("空清單沒有十二個實測基準")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 24 {
		t.Fatal("基準被快取或順序不一致")
	}
	for i, entry := range probe.BaselineEntries() {
		if seen[i] != entry.Hostname || seen[i+12] != entry.Hostname {
			t.Fatal("基準缺域或重複作業未重新測試")
		}
	}
	for _, result := range c.GetAppSnapshot().Data.Results {
		if !result.BaselineService || result.AutomaticEligible {
			t.Fatal("基準被當成管理授權")
		}
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.writes != 0 || api.deletes != 0 {
		t.Fatal("基準測試修改帳號")
	}
}

func TestCustomFileIsPrioritizedAndDoesNotReplaceBaselines(t *testing.T) {
	var seen []domain.DomainEntry
	var mu sync.Mutex
	runner := func(ctx context.Context, e domain.DomainEntry, s domain.Settings, d domain.DetectionResult, r domain.RangeSnapshot, n domain.NetworkSnapshot, current *domain.Rule, b *probe.Budget, progress func(int, int, string)) domain.TestResult {
		mu.Lock()
		seen = append(seen, e)
		mu.Unlock()
		return successfulProbe(ctx, e, s, d, r, n, current, b, progress)
	}
	c, _ := newTestCore(t, runner)
	id := importTestDomain(t, c)
	s, _ := c.manager.Snapshot()
	if result := c.UpdateDomain(s.Revision, id, true, "https://www.twitch.tv/test.bin"); !result.OK {
		t.Fatal(result.Error)
	}
	started := c.StartTest(nil, 100_000_000)
	if !started.OK {
		t.Fatal(started.Error)
	}
	job := waitJob(t, c, started.Data)
	mu.Lock()
	defer mu.Unlock()
	if job.Total != 13 || len(seen) != 13 || seen[0].TestURL == "" || seen[1].TestURL != "" || seen[2].TestURL != "" || job.ManagedCompleted != 1 || job.BaselinesCompleted != 12 {
		t.Fatal("自訂檔案替代了基準，沒有額外測試")
	}
}

func TestAutomaticTestOnlyNeedsNoTokenAndCannotWrite(t *testing.T) {
	c, api := newTestCore(t, successfulProbe)
	if result := c.DeleteControlDToken(); !result.OK {
		t.Fatal(result.Error)
	}
	s, _ := c.manager.Snapshot()
	a := s.Automation
	a.Enabled = true
	a.TestOnly = true
	a.Grants = nil
	a.ProfileID = s.ProfileID
	a.CredentialRevision = s.CredentialRevision
	if result := c.ConfigureAutomation(s.Revision, a); !result.OK {
		t.Fatal(result.Error)
	}
	started := c.startTest(nil, a.BudgetBytes, "automatic", "test")
	if !started.OK {
		t.Fatal(started.Error)
	}
	waitJob(t, c, started.Data)
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.writes != 0 || api.deletes != 0 {
		t.Fatal("只測試模式仍修改帳號")
	}
}
