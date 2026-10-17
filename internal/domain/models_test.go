package domain

import (
	"slices"
	"testing"
)

func TestDefaultSettingsAndLimits(t *testing.T) {
	s := DefaultSettings()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.Automation.Enabled || s.LaunchAtLogin {
		t.Fatal("自動功能不能預設啟用")
	}
	s.Automation.Enabled = true
	if s.Validate() == nil {
		t.Fatal("缺少授權不能啟用自動模式")
	}
	if !ValidBudget(0) || !ValidBudget(100_000_000) || ValidBudget(-1) {
		t.Fatal("預算驗證錯誤")
	}
	if s.Probe.ScanMode != "near" || s.Probe.JobSeconds != 0 || s.Automation.BudgetBytes != 0 || s.Probe.ConnectionSeconds != 10 {
		t.Fatal("參考預設不一致")
	}
}

func TestOriginalProbeOptionsAreExplicitAndIsolated(t *testing.T) {
	s := DefaultSettings()
	if s.Probe.NoResourceMode != "latency" || s.Probe.EnableIPv6 || !slices.Equal(s.Probe.Services, []string{"github", "twitch", "x"}) {
		t.Fatal("原版預設不可隱含啟用擴充")
	}
	input := ProbeSettings{Services: []string{"twitch"}, NoResourceMode: "shared", EnableIPv6: true}
	p := FixedProbe(input)
	input.Services[0] = "x"
	if !slices.Equal(p.Services, []string{"twitch"}) || p.NoResourceMode != "shared" || !p.EnableIPv6 {
		t.Fatal("明確選項需保留且不能借用呼叫端清單")
	}
	if empty := FixedProbe(ProbeSettings{Services: []string{}}); empty.Services == nil || len(empty.Services) != 0 {
		t.Fatal("空清單需保留全關語意，不能變回缺值預設")
	}
	for _, invalid := range [][]string{{"unknown"}, {"github", "github"}} {
		s.Probe.Services = invalid
		if s.Validate() == nil {
			t.Fatal("拒絕未知或重複的基準服務")
		}
	}
}
