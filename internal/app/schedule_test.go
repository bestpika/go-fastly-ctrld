package app

import (
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

func TestScheduleStabilitySleepAndInterval(t *testing.T) {
	a := domain.DefaultSettings().Automation
	a.Enabled = true
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	s := scheduleState{}
	s.observe(now, "first")
	for i := 1; i <= 5; i++ {
		s.observe(now.Add(time.Duration(i)*5*time.Second), "first")
	}
	if s.ready(now.Add(25*time.Second), a, "") {
		t.Fatal("尚未穩定即啟動")
	}
	s.observe(now.Add(30*time.Second), "first")
	if !s.ready(now.Add(30*time.Second), a, "") {
		t.Fatal("初次穩定後未啟動")
	}
	s.pending = false
	a.LastRun = now.Add(30 * time.Second)
	if s.ready(now.Add(35*time.Second), a, "") {
		t.Fatal("重複補跑")
	}
	s.observe(now.Add(2*time.Minute), "first")
	if s.ready(now.Add(2*time.Minute), a, "") {
		t.Fatal("喚醒後沒有重驗穩定")
	}
	for i := 1; i <= 6; i++ {
		s.observe(now.Add(2*time.Minute+time.Duration(i)*5*time.Second), "first")
	}
	if !s.ready(now.Add(150*time.Second), a, "") {
		t.Fatal("喚醒合併事件遺失")
	}
	s.pending = false
	if !s.ready(a.LastRun.Add(24*time.Hour), a, "") {
		t.Fatal("定期觸發失效")
	}
	if s.ready(a.LastRun.Add(24*time.Hour), a, "路由讀取失敗") {
		t.Fatal("無效網路仍觸發")
	}
	a.Enabled = false
	if s.ready(a.LastRun.Add(24*time.Hour), a, "") {
		t.Fatal("自動停用仍觸發")
	}
}

func TestSharedAutomaticRequiresNewExplicitPermission(t *testing.T) {
	s := domain.DefaultSettings()
	s.Automation.Grants = []domain.Grant{{DomainID: "target", Overwrite: true, Move: true}}
	makePreview := func() domain.SyncPreview {
		return domain.SyncPreview{Items: []domain.PreviewItem{{DomainID: "target", Applicable: true}}}
	}
	results := map[string]domain.TestResult{"target": {Mode: "shared-reference", AutomaticEligible: true}}
	p := makePreview()
	limitAutomatic(&p, s.Automation, results, time.Now())
	if p.Items[0].Applicable {
		t.Fatal("舊授權被默默擴大")
	}
	s.Automation.AllowSharedReference = true
	p = makePreview()
	limitAutomatic(&p, s.Automation, results, time.Now())
	if !p.Items[0].Applicable {
		t.Fatal("完整明確授權仍被拒")
	}
}

func TestAutomaticOverwriteMoveCooldownAndEvidence(t *testing.T) {
	now := time.Now()
	before := domain.Rule{Hostname: "www.twitch.tv", Safe: true}
	for _, test := range []struct {
		name                                 string
		overwrite, move, eligible, emergency bool
		last                                 time.Time
		want                                 bool
	}{
		{"完整授權", true, true, true, false, time.Time{}, true},
		{"沒有覆寫授權", false, true, true, false, time.Time{}, false},
		{"沒有移動授權", true, false, true, false, time.Time{}, false},
		{"雙來源不一致", true, true, false, false, time.Time{}, false},
		{"冷卻期間", true, true, true, false, now, false},
		{"緊急例外", true, true, true, true, now, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := domain.SyncPreview{Items: []domain.PreviewItem{{DomainID: "domain", Before: &before, Move: true, Applicable: true}}}
			a := domain.DefaultSettings().Automation
			a.Grants = []domain.Grant{{DomainID: "domain", Overwrite: test.overwrite, Move: test.move}}
			a.LastChanged = map[string]time.Time{"domain": test.last}
			limitAutomatic(&p, a, map[string]domain.TestResult{"domain": {AutomaticEligible: test.eligible, Emergency: test.emergency}}, now)
			if p.Items[0].Applicable != test.want {
				t.Fatal("自動套用安全條件不符")
			}
		})
	}
}

func TestForceWriteSkipsOnlyCooldownNotPermissions(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name                                     string
		overwrite, move, eligible, shared, grant bool
		want                                     bool
	}{
		{"只略過冷卻", true, true, true, true, true, true},
		{"仍需覆寫", false, true, true, true, true, false},
		{"仍需移動", true, false, true, true, true, false},
		{"仍需自動資格", true, true, false, true, true, false},
		{"仍需共享授權", true, true, true, false, true, false},
		{"仍需網域授權", true, true, true, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := domain.DefaultSettings().Automation
			a.AllowSharedReference = tc.shared
			a.LastChanged["domain"] = now
			if tc.grant {
				a.Grants = []domain.Grant{{DomainID: "domain", Overwrite: tc.overwrite, Move: tc.move}}
			}
			p := domain.SyncPreview{Items: []domain.PreviewItem{{DomainID: "domain", Before: &domain.Rule{}, Move: true, Applicable: true, ForceWrite: true}}}
			limitAutomatic(&p, a, map[string]domain.TestResult{"domain": {Mode: "shared-reference", AutomaticEligible: tc.eligible}}, now)
			if p.Items[0].Applicable != tc.want {
				t.Fatal("強制寫入放寬非冷卻安全條件")
			}
		})
	}
}
