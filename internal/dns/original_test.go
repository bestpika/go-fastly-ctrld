package dns

import (
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

func originalEvidence() (domain.DetectionResult, domain.DomainEntry, domain.RangeSnapshot, time.Time) {
	now := time.Now().UTC()
	entry := domain.DomainEntry{ID: "domain", Hostname: "cdn.example.com", Enabled: true}
	ranges := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/16", "2a04:4e42::/32"}, FetchedAt: now, ExpiresAt: now.Add(time.Hour)}
	ranges.Digest = domain.Digest(ranges.CIDRs)
	d := domain.DetectionResult{DomainID: entry.ID, Hostname: entry.Hostname, Classification: "confirmed", ObservedAt: now, ExpiresAt: now.Add(time.Minute)}
	for _, source := range []string{"Google", "Cloudflare"} {
		d.Sources = append(d.Sources, domain.DNSObservation{Source: source, ObservedAt: now, Records: []domain.DNSRecord{
			{Name: entry.Hostname, Type: "CNAME", Value: "edge.fastly.net", TTL: 60},
			{Name: "edge.fastly.net", Type: "A", Value: "151.101.0.214", TTL: 60},
			{Name: "edge.fastly.net", Type: "AAAA", Value: "2a04:4e42::214", TTL: 60},
		}})
	}
	return d, entry, ranges, now
}

func TestOriginalTargetPrefersFirstCNAMEAndExplicitIPv6(t *testing.T) {
	d, entry, ranges, now := originalEvidence()
	via, v6, ips, err := OriginalTarget(d, entry, ranges, false, now)
	if err != nil || via != "edge.fastly.net" || v6 != "" || len(ips) != 1 || ips[0] != "151.101.0.214" {
		t.Fatalf("沒有優先第一跳 CNAME：%s %s %v %v", via, v6, ips, err)
	}
	for index := range d.Sources {
		d.Sources[index].Records = d.Sources[index].Records[1:]
		for ri := range d.Sources[index].Records {
			d.Sources[index].Records[ri].Name = entry.Hostname
		}
	}
	via, v6, ips, err = OriginalTarget(d, entry, ranges, false, now)
	if err != nil || via != "151.101.0.214" || v6 != "" || len(ips) != 1 {
		t.Fatal("沒有 CNAME 時沒有使用原始 IPv4，或默默選用 IPv6")
	}
	_, v6, ips, err = OriginalTarget(d, entry, ranges, true, now)
	if err != nil || v6 != "2a04:4e42::214" || len(ips) != 2 {
		t.Fatal("明確選用 IPv6 後未保留原始 AAAA")
	}
}

func TestOriginalTargetRejectsAmbiguousAndUnsafeEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*domain.DetectionResult)
	}{
		{"來源不足", func(d *domain.DetectionResult) { d.Sources = d.Sources[:1] }},
		{"來源重複", func(d *domain.DetectionResult) { d.Sources[1].Source = "Google" }},
		{"來源錯誤", func(d *domain.DetectionResult) { d.Sources[0].Error = "失敗" }},
		{"歸屬不一致", func(d *domain.DetectionResult) { d.Classification = "mixed" }},
		{"網域不符", func(d *domain.DetectionResult) { d.DomainID = "other" }},
		{"CNAME 衝突", func(d *domain.DetectionResult) { d.Sources[1].Records[0].Value = "other.fastly.net" }},
		{"CNAME 自指", func(d *domain.DetectionResult) { d.Sources[0].Records[0].Value = d.Hostname }},
		{"CNAME 子域循環", func(d *domain.DetectionResult) { d.Sources[0].Records[0].Value = "child." + d.Hostname }},
		{"CNAME 鏈循環", func(d *domain.DetectionResult) {
			d.Sources[0].Records = append(d.Sources[0].Records, domain.DNSRecord{Name: "edge.fastly.net", Type: "CNAME", Value: "edge.fastly.net", TTL: 60})
		}},
		{"不相干 CNAME", func(d *domain.DetectionResult) {
			d.Sources[0].Records = append(d.Sources[0].Records, domain.DNSRecord{Name: "other.example.com", Type: "CNAME", Value: "edge.fastly.net", TTL: 60})
		}},
		{"私有 IP", func(d *domain.DetectionResult) { d.Sources[0].Records[1].Value = "10.0.0.1" }},
		{"非官方 IP", func(d *domain.DetectionResult) { d.Sources[0].Records[1].Value = "1.1.1.1" }},
		{"非鏈尾 IP", func(d *domain.DetectionResult) { d.Sources[0].Records[1].Name = "other.example.com" }},
		{"缺少 IPv4", func(d *domain.DetectionResult) { d.Sources[0].Records = d.Sources[0].Records[:1] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, entry, ranges, now := originalEvidence()
			tc.mutate(&d)
			if _, _, _, err := OriginalTarget(d, entry, ranges, true, now); err == nil {
				t.Fatal("不安全的原始 DNS 證據未拒絕")
			}
		})
	}
}

func TestOriginalTargetDoesNotUseElapsedTimeOrZeroTTLAsQualification(t *testing.T) {
	d, entry, ranges, now := originalEvidence()
	d.ExpiresAt, ranges.ExpiresAt = now.Add(-24*time.Hour), now.Add(-24*time.Hour)
	for si := range d.Sources {
		for ri := range d.Sources[si].Records {
			d.Sources[si].Records[ri].TTL = 0
		}
	}
	via, _, ips, err := OriginalTarget(d, entry, ranges, false, now.Add(365*24*time.Hour))
	if err != nil || via != "edge.fastly.net" || len(ips) != 1 {
		t.Fatal("時間或零 TTL 阻擋內容有效的原始 DNS", err)
	}
}

func TestOriginalCNAMECannotEnterAnotherProfileRule(t *testing.T) {
	d, entry, _, _ := originalEvidence()
	for _, host := range []string{"edge.fastly.net", "fastly.net", "*.fastly.net"} {
		if err := ValidateOriginalRules(entry.Hostname, d, map[string]domain.Rule{host: {Status: 1, Safe: true}}); err == nil {
			t.Fatal("CNAME 進入其他啟用規則仍允許")
		}
	}
	if err := ValidateOriginalRules(entry.Hostname, d, map[string]domain.Rule{"fastly.net": {Status: 0, Safe: true}}); err != nil {
		t.Fatal("已確認停用的規則不應阻擋")
	}
	if err := ValidateOriginalRules(entry.Hostname, d, map[string]domain.Rule{"fastly.net": {Status: 0, Safe: false}}); err == nil {
		t.Fatal("無法解析的規則被當成停用")
	}
}
