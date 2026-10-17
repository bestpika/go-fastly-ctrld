package rulesync

import (
	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"strings"
	"testing"
	"time"
)

func TestOldStrategyCannotBeApplied(t *testing.T) {
	s := domain.DefaultSettings()
	n := domain.NetworkSnapshot{Fingerprint: "network"}
	entry := domain.DomainEntry{ID: "domain", Hostname: "www.twitch.tv", Enabled: true}
	now := time.Now()
	ranges := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/16"}, FetchedAt: now, ExpiresAt: now.Add(time.Hour)}
	ranges.Digest = domain.Digest(ranges.CIDRs)
	r := domain.TestResult{ID: "result", DomainID: entry.ID, Hostname: entry.Hostname, SettingsRevision: s.Revision, NetworkFingerprint: n.Fingerprint, ExpiresAt: now.Add(time.Minute), BestIPv4: "151.101.0.214"}
	preview := Build(s, n, []domain.DomainEntry{entry}, map[string]domain.TestResult{entry.ID: r}, ranges, nil, nil, nil)
	if preview.Items[0].Applicable {
		t.Fatal("舊策略結果可套用")
	}
	r.StrategyVersion = domain.ProbeStrategyVersion
	r.Complete = true
	preview = Build(s, n, []domain.DomainEntry{entry}, map[string]domain.TestResult{entry.ID: r}, ranges, nil, nil, nil)
	if !preview.Items[0].Applicable {
		t.Fatal("新策略有效結果被拒絕")
	}
	s.DestinationFolderID = 7
	old := domain.Rule{Hostname: entry.Hostname, Do: 2, Status: 1, Via: "151.101.1.214", Group: 3, Safe: true}
	preview = Build(s, n, []domain.DomainEntry{entry}, map[string]domain.TestResult{entry.ID: r}, ranges, map[string]domain.Rule{entry.Hostname: old}, []domain.Group{{ID: 7, Name: "目的"}}, nil)
	if !preview.Items[0].Applicable || !preview.Items[0].Move || preview.Items[0].After.Group != 7 || preview.Items[0].Before.Group != 3 {
		t.Fatal("目的資料夾或移動未入不可變預覽")
	}
	preview = Build(s, n, []domain.DomainEntry{entry}, map[string]domain.TestResult{entry.ID: r}, ranges, nil, []domain.Group{{ID: 7, Action: true}}, nil)
	if preview.Items[0].Applicable {
		t.Fatal("動作資料夾仍可套用")
	}
}

func TestForcePreviewUsesOriginalDNSAndRewritesSameValue(t *testing.T) {
	now := time.Now().UTC()
	s := domain.DefaultSettings()
	n := domain.NetworkSnapshot{Fingerprint: "network"}
	entry := domain.DomainEntry{ID: "domain", Hostname: "cdn.example.com", Enabled: true}
	ranges := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/16"}, FetchedAt: now, ExpiresAt: now.Add(time.Hour)}
	ranges.Digest = domain.Digest(ranges.CIDRs)
	r := domain.TestResult{ID: "result", DomainID: entry.ID, Hostname: entry.Hostname, Complete: true, StrategyVersion: domain.ProbeStrategyVersion, SettingsRevision: s.Revision, NetworkFingerprint: n.Fingerprint, ExpiresAt: now.Add(time.Minute)}
	d := domain.DetectionResult{DomainID: entry.ID, Hostname: entry.Hostname, Classification: "confirmed", ObservedAt: now, ExpiresAt: now.Add(30 * time.Second)}
	for _, source := range []string{"Google", "Cloudflare"} {
		d.Sources = append(d.Sources, domain.DNSObservation{Source: source, ObservedAt: now, Records: []domain.DNSRecord{{Name: entry.Hostname, Type: "CNAME", Value: "edge.fastly.net", TTL: 30}, {Name: "edge.fastly.net", Type: "A", Value: "151.101.0.214", TTL: 30}}})
	}
	old := domain.Rule{Hostname: entry.Hostname, Do: 2, Status: 1, Via: "edge.fastly.net", ViaV6: "2a04:4e42::214", Comment: "保留註解", Safe: true}
	rules := map[string]domain.Rule{entry.Hostname: old}
	detections := map[string]domain.DetectionResult{entry.ID: d}
	build := func() domain.SyncPreview {
		return Build(s, n, []domain.DomainEntry{entry}, map[string]domain.TestResult{entry.ID: r}, ranges, rules, nil, detections)
	}
	p := build()
	if p.Items[0].Applicable || !strings.Contains(p.Items[0].Reason, "沒有符合改善") {
		t.Fatal("預設行為改變")
	}
	s.ForceWrite = true
	p = build()
	i := p.Items[0]
	if !i.Applicable || !i.ForceWrite || i.Operation != "rewrite" || i.OriginalDNS == nil || i.After.SemanticDigest() != old.SemanticDigest() || !p.ExpiresAt.Equal(d.ExpiresAt) || strings.Contains(i.Reason, "通過多輪改善") {
		t.Fatalf("原始 DNS 同值重寫不符：%+v", i)
	}
	r.BestIPv4 = "151.101.1.214"
	p = build()
	if !p.Items[0].Applicable || p.Items[0].OriginalDNS != nil || p.Items[0].After.Via != r.BestIPv4 {
		t.Fatal("有改善時未使用正常最佳 IP")
	}
	r.BestIPv4 = ""
	delete(detections, entry.ID)
	if build().Items[0].Applicable {
		t.Fatal("缺少原始 DNS 時拿既有覆寫替代")
	}
	detections[entry.ID] = d
	r.Complete = false
	r.Reason = "Google／Cloudflare DNS 重新查詢失敗"
	blocked := build().Items[0]
	if blocked.Applicable || blocked.Reason != "測試未完成，無法套用" {
		t.Fatal("未完成結果被強制寫入")
	}
	r.Complete = true
	r.BaselineService = true
	blocked = build().Items[0]
	if blocked.Applicable || !strings.Contains(blocked.Reason, "基準結果不可套用") || strings.Contains(blocked.Reason, "測試未完成") {
		t.Fatal("基準展示與管理網域未完成原因混淆")
	}
	r.BaselineService = false
	rules["fastly.net"] = domain.Rule{Hostname: "fastly.net", Status: 1, Safe: true}
	if build().Items[0].Applicable {
		t.Fatal("CNAME 落入其他規則仍回填")
	}
}
