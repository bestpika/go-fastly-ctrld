package settings

import (
	"errors"
	"testing"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
)

type fakeVault struct {
	token string
	sets  int
}

func TestOriginalPresetMigrationAndExplicitExtensionsPersist(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	old := domain.DefaultSettings()
	old.PresetVersion = 1
	old.Probe.NoResourceMode, old.Probe.EnableIPv6 = "shared", true
	old.Probe.JobSeconds = 600
	old.Probe.ScanMode = "full"
	old.Automation.BudgetBytes = 100_000_000
	old.Automation.AllowSharedReference = true
	old.Automation.Grants = []domain.Grant{{DomainID: "old"}}
	if err := store.Write("state.json", state{Settings: old, Domains: []domain.DomainEntry{}}); err != nil {
		t.Fatal(err)
	}
	m, err := New(store, &fakeVault{})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := m.Snapshot()
	if s.PresetVersion != domain.PresetVersion || s.Revision == old.Revision || s.Probe.NoResourceMode != "latency" || s.Probe.EnableIPv6 || s.Automation.AllowSharedReference || len(s.Automation.Grants) != 0 || s.Probe.JobSeconds != 600 || s.Probe.ScanMode != "full" || s.Automation.BudgetBytes != 100_000_000 {
		t.Fatal("遷移繼承隱含擴充，或放寬既有上限")
	}
	p := s.Probe
	p.Services, p.EnableIPv6, p.NoResourceMode = []string{}, true, "shared"
	if err := m.Update(s.Revision, Patch{Theme: s.Theme, Probe: p}); err != nil {
		t.Fatal(err)
	}
	m, err = New(store, &fakeVault{})
	if err != nil {
		t.Fatal(err)
	}
	s, _ = m.Snapshot()
	if s.Probe.Services == nil || len(s.Probe.Services) != 0 || !s.Probe.EnableIPv6 || s.Probe.NoResourceMode != "shared" {
		t.Fatal("明確選用與全關服務未持久保存")
	}
}

func (v *fakeVault) Set(s string) error   { v.token = s; v.sets++; return nil }
func (v *fakeVault) Get() (string, error) { return v.token, nil }
func (v *fakeVault) Delete() error        { v.token = ""; return nil }

func TestForceWriteDefaultsPersistsAndInvalidatesAuthorization(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	m, _ := New(store, &fakeVault{})
	s, _ := m.Snapshot()
	if s.ForceWrite {
		t.Fatal("強制寫入不得預設開啟")
	}
	// 模擬既有授權，驗證開關不能沿用原本只寫改善候選的同意。
	m.state.Settings.Automation.Enabled = true
	m.state.Settings.Automation.Grants = []domain.Grant{{DomainID: "old", Overwrite: true}}
	on := true
	if err := m.Update(s.Revision, Patch{Theme: s.Theme, Probe: s.Probe, ForceWrite: &on}); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Snapshot()
	if !next.ForceWrite || next.Revision == s.Revision || next.Automation.Enabled || len(next.Automation.Grants) != 0 {
		t.Fatal("開關未失效舊結果與授權")
	}
	if err := m.Update(next.Revision, Patch{Theme: "dark", Probe: next.Probe}); err != nil {
		t.Fatal(err)
	}
	m, err := New(store, &fakeVault{})
	if err != nil {
		t.Fatal(err)
	}
	next, _ = m.Snapshot()
	if !next.ForceWrite || next.Theme != "dark" {
		t.Fatal("省略開關或重新啟動遺失已保存值")
	}
	store.BeforeReplace = func() error { return errors.New("故障") }
	off := false
	if m.Update(next.Revision, Patch{Theme: next.Theme, Probe: next.Probe, ForceWrite: &off}) == nil {
		t.Fatal("保存故障未回報")
	}
	actual, _ := m.Snapshot()
	if !actual.ForceWrite || actual.Revision != next.Revision {
		t.Fatal("保存失敗仍變更開關")
	}
}

func TestRevisionAndCredentialSafety(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	vault := &fakeVault{}
	m, err := New(store, vault)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := m.Snapshot()
	if err = m.SetToken("secret"); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Snapshot()
	if next.CredentialRevision == s.CredentialRevision || next.Revision == s.Revision {
		t.Fatal("權杖更換未失效舊修訂")
	}
	if m.Update(s.Revision, Patch{Theme: "dark", Probe: s.Probe}) == nil {
		t.Fatal("舊修訂未拒絕")
	}
	store.BeforeReplace = func() error { return errors.New("故障") }
	if m.SetToken("new-secret") == nil || vault.sets != 1 {
		t.Fatal("保存失敗後仍變更金鑰")
	}
}

func TestImportAndMutationsDoNotContainToken(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	m, _ := New(store, &fakeVault{})
	s, _ := m.Snapshot()
	p, _ := domain.ParseImport("www.twitch.tv\nwww.twitch.tv", "paste", s.Revision, nil)
	if err := m.Import(p); err != nil {
		t.Fatal(err)
	}
	s, ds := m.Snapshot()
	if len(ds) != 1 {
		t.Fatal("匯入去重錯誤")
	}
	if err := m.RemoveDomain(s.Revision, ds[0].ID); err != nil {
		t.Fatal(err)
	}
	_, ds = m.Snapshot()
	if len(ds) != 0 {
		t.Fatal("移除失敗")
	}
}

func TestLegacySettingsKeepExistingLimits(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	original := domain.DefaultSettings()
	original.Probe.ScanMode = ""
	original.Probe.JobSeconds = 600
	original.Automation.BudgetBytes = 100_000_000
	if err := store.Write("state.json", state{Settings: original, Domains: []domain.DomainEntry{}}); err != nil {
		t.Fatal(err)
	}
	m, err := New(store, &fakeVault{})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := m.Snapshot()
	if s.Probe.ScanMode != "near" || s.Probe.JobSeconds != 600 || s.Automation.BudgetBytes != 100_000_000 || s.Revision != original.Revision {
		t.Fatal("讀取舊設定擅自放寬已存上限")
	}
}

func TestPresetMigrationAndUpdatesCannotKeepHiddenTuning(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	old := domain.DefaultSettings()
	old.PresetVersion = 0
	old.Probe.Candidates = 30
	old.Probe.Rounds = 9
	old.Probe.NoResourceMode = "latency"
	old.Probe.SpeedImprovement = .5
	old.Probe.JobSeconds = 600
	old.Automation.BudgetBytes = 100_000_000
	old.Automation.TestOnly = true
	old.Automation.Enabled = true
	old.Automation.StabilitySeconds = 55
	if err := store.Write("state.json", state{Settings: old, Domains: []domain.DomainEntry{}}); err != nil {
		t.Fatal(err)
	}
	m, err := New(store, &fakeVault{})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := m.Snapshot()
	if s.Revision == old.Revision || s.Automation.Enabled || len(s.Automation.Grants) > 0 || s.Probe.Candidates != 10 || s.Probe.Rounds != 3 || s.Probe.SpeedImprovement != 0 || s.Probe.NoResourceMode != "latency" || s.Probe.JobSeconds != 600 || s.Automation.BudgetBytes != 100_000_000 {
		t.Fatal("遷移未固定策略或擅自解除上限／授權")
	}
	p := s.Probe
	p.Candidates = 30
	p.SampleSeconds = 20
	if err := m.Update(s.Revision, Patch{Theme: s.Theme, Probe: p}); err != nil {
		t.Fatal(err)
	}
	s, _ = m.Snapshot()
	if s.Probe.Candidates != 10 || s.Probe.SampleSeconds != 10 {
		t.Fatal("隱藏數值仍可影響策略")
	}
}
