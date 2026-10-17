package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/probe"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
)

func enableTestAutomation(t *testing.T, c *Core, id string) {
	t.Helper()
	if !c.SetControlDToken("test-secret").OK {
		t.Fatal("測試金鑰設定失敗")
	}
	s := c.GetAppSnapshot().Data.Settings
	if !c.SelectProfile(s.Revision, "profile").OK {
		t.Fatal("測試設定檔選擇失敗")
	}
	s = c.GetAppSnapshot().Data.Settings
	a := s.Automation
	a.Enabled = true
	a.ProfileID = s.ProfileID
	a.CredentialRevision = s.CredentialRevision
	a.Grants = []domain.Grant{{DomainID: id, Overwrite: true}}
	if r := c.ConfigureAutomation(s.Revision, a); !r.OK {
		t.Fatal(r.Error)
	}
}

func TestAutomaticCheckpointPreventsRunAfterConfigurationFailure(t *testing.T) {
	var probes atomic.Int64
	c, api := newTestCore(t, func(argsContext context.Context, e domain.DomainEntry, s domain.Settings, d domain.DetectionResult, r domain.RangeSnapshot, n domain.NetworkSnapshot, rule *domain.Rule, b *probe.Budget, progress func(int, int, string)) domain.TestResult {
		probes.Add(1)
		return successfulProbe(argsContext, e, s, d, r, n, rule, b, progress)
	})
	id := importTestDomain(t, c)
	enableTestAutomation(t, c, id)
	c.config.BeforeReplace = func() error { return errors.New("私密儲存錯誤") }
	r := c.startTest([]string{id}, 1_000_000, "automatic", "test")
	if r.OK || probes.Load() != 0 || api.writes != 0 {
		t.Fatal("排程紀錄失敗仍開始測試或寫入")
	}
	snapshot := c.GetAppSnapshot().Data
	if snapshot.Settings.Automation.Enabled || snapshot.AutomationError == "" {
		t.Fatal("沒有顯示實際暫停狀態")
	}
	if !c.Stop() {
		t.Fatal("無法安全停止")
	}
	reopened, err := New(Options{Paths: storage.Paths{Config: c.config.Root, Data: c.data.Root, Cache: c.cache.Root}, Vault: &testVault{token: "test-secret"}, Network: testNetwork{}, API: api})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Stop()
	if reopened.GetAppSnapshot().Data.Settings.Automation.Enabled {
		t.Fatal("重開後默默恢復自動執行")
	}
	s := reopened.managerSnapshotForTest()
	a := s.Automation
	a.Enabled = true
	if result := reopened.ConfigureAutomation(s.Revision, a); !result.OK {
		t.Fatal(result.Error)
	}
	if !reopened.GetAppSnapshot().Data.Settings.Automation.Enabled {
		t.Fatal("修復後明確授權不能恢復")
	}
}

func (c *Core) managerSnapshotForTest() domain.Settings { s, _ := c.manager.Snapshot(); return s }

func TestAutomaticCooldownFailureIsDurableAndRebuiltFromVerifiedJournal(t *testing.T) {
	c, api := newTestCore(t, successfulProbe)
	id := importTestDomain(t, c)
	enableTestAutomation(t, c, id)
	c.config.BeforeReplace = func() error {
		api.mu.Lock()
		defer api.mu.Unlock()
		if api.writes > 0 {
			return errors.New("冷卻保存故障")
		}
		return nil
	}
	r := c.startTest([]string{id}, 1_000_000, "automatic", "test")
	if !r.OK {
		t.Fatal(r.Error)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		s := c.GetAppSnapshot().Data
		if s.AutomationError != "" && s.ActiveTestID == "" && s.ActiveWriterID == "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	snap := c.GetAppSnapshot().Data
	api.mu.Lock()
	writes := api.writes
	api.mu.Unlock()
	if snap.AutomationError == "" || snap.Settings.Automation.Enabled || writes != 1 {
		t.Fatalf("故障後未暫停或重送：%s，%d", snap.AutomationError, writes)
	}
	var checkpoint automaticCheckpoint
	if err := c.data.Read("automation-checkpoint.json", &checkpoint); err != nil || !checkpoint.Blocked {
		t.Fatal("套用前未保留持久安全標記")
	}
	c.config.BeforeReplace = nil
	s := c.managerSnapshotForTest()
	a := s.Automation
	a.Enabled = true
	if result := c.ConfigureAutomation(s.Revision, a); !result.OK {
		t.Fatal(result.Error)
	}
	if c.GetAppSnapshot().Data.Settings.Automation.LastChanged[id].IsZero() {
		t.Fatal("沒有由已確認後像重建冷卻時間")
	}
}

func TestAutomaticHistoryIgnoresOtherCredentialsAndUnknownItems(t *testing.T) {
	s := domain.DefaultSettings()
	s.ProfileID = "profile"
	s.CredentialRevision = "credential"
	now := time.Now().UTC()
	jobs := map[string]domain.Job{
		"verified": {Kind: "apply", Trigger: "automatic", ProfileID: s.ProfileID, CredentialRevision: s.CredentialRevision, StartedAt: now.Add(-time.Minute), FinishedAt: now, Items: []domain.JournalItem{{State: "verified", Preview: domain.PreviewItem{DomainID: "domain"}}, {State: "unknown", Preview: domain.PreviewItem{DomainID: "unknown"}}}},
		"other":    {Kind: "apply", Trigger: "automatic", ProfileID: s.ProfileID, CredentialRevision: "other", StartedAt: now.Add(time.Minute)},
	}
	a := automaticHistory(s, jobs)
	if !a.LastChanged["domain"].Equal(now) || !a.LastChanged["unknown"].IsZero() || a.LastRun.After(now) {
		t.Fatal("歷史重建混入未知結果或其他帳號")
	}
}

func TestStaleAutomaticCompletionCannotClearNewFault(t *testing.T) {
	c, _ := newTestCore(t, successfulProbe)
	generation := c.automationGeneration
	c.blockAutomatic("新的安全故障")
	if c.clearAutomaticCheckpoint(generation, false) == nil || c.GetAppSnapshot().Data.AutomationError == "" {
		t.Fatal("過時完成事件解除新的暫停")
	}
	var checkpoint automaticCheckpoint
	if err := c.data.Read("automation-checkpoint.json", &checkpoint); err != nil || !checkpoint.Blocked {
		t.Fatal("持久暫停標記被舊作業清除")
	}
}
