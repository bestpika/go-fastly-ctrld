package probe

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

func TestBudgetSharedAndRefunded(t *testing.T) {
	b := &Budget{Limit: 100}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { n, release := b.Reserve(10); release(n) })
	}
	wg.Wait()
	if b.Used() != 100 {
		t.Fatalf("預算=%d", b.Used())
	}
	n, release := b.Reserve(1)
	release(n)
	if n != 0 {
		t.Fatal("超支")
	}
}

func TestSelectionNeedsImprovementAndSuccess(t *testing.T) {
	p := domain.DefaultSettings().Probe
	values := []domain.CandidateResult{{IP: "151.101.0.1", Family: 4, Baseline: true, Samples: []domain.Sample{{Success: true, Mbps: 50}, {Success: true, Mbps: 50}, {Success: true, Mbps: 50}}}, {IP: "151.101.64.1", Family: 4, Samples: []domain.Sample{{Success: true, Mbps: 100}, {Success: true, Mbps: 100}, {Success: true, Mbps: 100}}}}
	for i := range values {
		Summarize(&values[i])
	}
	RankSmooth(values)
	if ip, _ := Select(values, 4, p, true); ip != "" {
		t.Fatal("順暢輪數相同仍因中位速度而切換")
	}
	values[0].Samples = []domain.Sample{{Success: true, Mbps: 4}, {Success: true, Mbps: 4}, {Success: true, Mbps: 100}}
	Summarize(&values[0])
	RankSmooth(values)
	if ip, _ := Select(values, 4, p, true); ip != values[1].IP {
		t.Fatal("未選出改善候選")
	}
	values[1].Samples[0] = domain.Sample{}
	Summarize(&values[1])
	RankSmooth(values)
	if ip, _ := Select(values, 4, p, true); ip != values[1].IP {
		t.Fatal("完整三輪含失敗零值，未依原版順暢次數選擇")
	}
}

func TestFullBlocksAndLatencyOrder(t *testing.T) {
	s := domain.DefaultSettings()
	r := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/16"}}
	d := domain.DetectionResult{Candidates: []string{"151.101.0.214"}}
	ips := Candidates(d, r, nil, s.Probe, false)
	if len(ips) != 256 || !slices.Contains(ips, "151.101.255.214") {
		t.Fatal("未展開每個 /24")
	}
	e := Engine{TCP: func(_ context.Context, ip string) (float64, error) {
		if ip == "151.101.1.133" {
			return 5, nil
		}
		return 80, nil
	}}
	near, count := e.scan(context.Background(), ips, map[string]bool{}, domain.DomainEntry{Hostname: "www.twitch.tv"}, domain.RangeSnapshot{}, s.Probe)
	if count != 256 || len(near) == 0 || near[0].ip != "151.101.1.214" {
		t.Fatal("仍依 CIDR 首筆而非延遲挑選")
	}
}

func TestFailedRoundsRemainZeroAndFamilyThresholds(t *testing.T) {
	values := []domain.CandidateResult{{Family: 4, Samples: []domain.Sample{{Success: true, Mbps: 10}, {}, {}}}, {Family: 6, Samples: []domain.Sample{{Success: true, Mbps: 1000}}}}
	for i := range values {
		Summarize(&values[i])
	}
	RankSmooth(values)
	if values[0].MedianMbps != 0 || values[0].SmoothThresholdMbps != 3 || values[1].SmoothThresholdMbps != 300 {
		t.Fatal("失敗未計零或位址族門檻互相污染")
	}
}
