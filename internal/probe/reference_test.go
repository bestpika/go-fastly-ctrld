package probe

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

func sampleReference() (domain.SharedReference, domain.Settings, domain.RangeSnapshot, domain.NetworkSnapshot) {
	s := domain.DefaultSettings()
	now := time.Now().UTC()
	r := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/16", "2a04:4e42::/32"}, FetchedAt: now, ExpiresAt: now.Add(time.Hour)}
	r.Digest = domain.Digest(r.CIDRs)
	n := domain.NetworkSnapshot{Fingerprint: "test", IPv6: true}
	ref := domain.SharedReference{ID: "reference", StrategyVersion: domain.ProbeStrategyVersion, NetworkFingerprint: n.Fingerprint, RangesDigest: r.Digest, ProbeDigest: domain.Digest(s.Probe), ObservedAt: now, ExpiresAt: now.Add(15 * time.Minute)}
	for _, source := range []struct{ name, host, tail string }{{"Twitch", "assets.twitch.tv", "214"}, {"GitHub", "release-assets.githubusercontent.com", "133"}} {
		part := domain.ReferenceSource{Name: source.name, SpeedHost: source.host, Candidates: []domain.CandidateResult{}}
		for index := 0; index < 2; index++ {
			ip := "151.101.0." + source.tail
			if index == 1 {
				ip = "151.101.1." + source.tail
			}
			scores := []float64{4, 4, 100}
			if index == 1 {
				scores = []float64{100, 100, 100}
			}
			c := domain.CandidateResult{IP: ip, Family: 4}
			for _, mbps := range scores {
				c.Samples = append(c.Samples, domain.Sample{Success: true, Mbps: mbps, ActualIP: ip, Hostname: source.host})
			}
			Summarize(&c)
			part.Candidates = append(part.Candidates, c)
		}
		RankSmooth(part.Candidates)
		ref.Sources = append(ref.Sources, part)
	}
	ref.Digest = ReferenceDigest(ref)
	return ref, s, r, n
}

func TestReferenceProofMappingWithoutAgeQualification(t *testing.T) {
	ref, s, r, n := sampleReference()
	if !ValidReference(ref, s, r, n, time.Now()) {
		t.Fatal("合法證據被拒")
	}
	d := domain.DetectionResult{Candidates: []string{"151.101.0.49", "151.101.0.99", "2a04:4e42::49"}, Sources: []domain.DNSObservation{{Records: []domain.DNSRecord{{Type: "A", Value: "151.101.0.49"}, {Type: "AAAA", Value: "2a04:4e42::49"}}}}}
	ips := ReferenceTargets(ref, d, r, true)
	for _, ip := range ips {
		a, _ := netip.ParseAddr(ip)
		if a.Is4() && a.As4()[3] != 49 {
			t.Fatal("照抄來源尾碼")
		}
		if a.Is6() {
			t.Fatal("造出 IPv6")
		}
	}
	if len(ips) != 2 {
		t.Fatal("路線未去重")
	}
	if len(ReferenceTargets(ref, domain.DetectionResult{Candidates: d.Candidates}, r, true)) != 0 {
		t.Fatal("歷史候選被當成實際 DNS 尾碼")
	}
	other := n
	other.Fingerprint = "changed"
	if ValidReference(ref, s, r, other, time.Now()) {
		t.Fatal("網路變更未失效")
	}
	if !ValidReference(ref, s, r, n, ref.ExpiresAt.Add(365*24*time.Hour)) {
		t.Fatal("時間經過不應阻擋內容有效的共享參考")
	}
	ref.Sources[0].Candidates[0].Samples[0].Mbps++
	if ValidReference(ref, s, r, n, time.Now()) {
		t.Fatal("證據摘要未核對")
	}
}

func TestSharedConsensusDoesNotBorrowTargetMbpsOrEmergency(t *testing.T) {
	ref, s, _, _ := sampleReference()
	own := func(ip string, baseline bool) domain.CandidateResult {
		c := domain.CandidateResult{IP: ip, Family: 4, Baseline: baseline, MedianLatency: 10, ReferenceScores: Scores(ref, ip), Samples: []domain.Sample{{Success: true}, {Success: true}, {Success: true}}}
		Summarize(&c)
		return c
	}
	base, candidate := own("151.101.0.49", true), own("151.101.1.49", false)
	if ip, ok := PickShared([]domain.CandidateResult{base, candidate}, 4, s.Probe); !ok || ip != candidate.IP {
		t.Fatal("共同改善未入選")
	}
	if candidate.MedianMbps != 0 || candidate.Successes != 3 {
		t.Fatal("來源樣本被塞入目標")
	}
	candidate.ReferenceScores[1].SmoothRounds = 0
	if _, ok := PickShared([]domain.CandidateResult{base, candidate}, 4, s.Probe); ok {
		t.Fatal("來源相反仍建議")
	}
	candidate.ReferenceScores = Scores(ref, candidate.IP)
	base.Samples = []domain.Sample{{Error: "目標失敗"}, {Error: "目標失敗"}, {Error: "目標失敗"}}
	Summarize(&base)
	if _, ok := PickShared([]domain.CandidateResult{base, candidate}, 4, s.Probe); ok {
		t.Fatal("來源成績假冒目標緊急切換")
	}
	if _, ok := PickShared([]domain.CandidateResult{candidate}, 4, s.Probe); ok {
		t.Fatal("沒有基準仍使用共享")
	}
}

func TestReferenceInsufficientBudgetDoesNotStartSourceDownloads(t *testing.T) {
	_, s, r, n := sampleReference()
	d := domain.DetectionResult{Classification: "confirmed", ExpiresAt: time.Now().Add(time.Minute)}
	b := &Budget{Limit: 1_000_000}
	_, err := New().BuildReference(context.Background(), s, r, n, []domain.DetectionResult{d, d}, nil, b)
	if err == nil || b.Used() != 0 {
		t.Fatal("不足預算仍啟動參考下載")
	}
}
