package probe

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

func TestReplayRankAndDecisionFromUnmodifiedUpstreamFunctions(t *testing.T) {
	// 固定 v0.8.3／336e105 的原始 rank、evaluate 透過 AST 離線重播所得。
	var cases []struct {
		Name                          string
		Download, HasBaseline, Apply  bool
		BaseSpeeds, CandidateSpeeds   []float64
		BaseLatency, CandidateLatency float64
		CandidateFast                 int
		CandidateMedian               float64
	}
	raw, err := os.ReadFile("testdata/upstream-v0.8.3.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			makeCandidate := func(ip string, baseline bool, values []float64, latency float64) domain.CandidateResult {
				c := domain.CandidateResult{IP: ip, Family: 4, Baseline: baseline, MedianLatency: latency}
				if !fixture.Download {
					values = []float64{1, 1, 1}
				}
				for _, value := range values {
					mbps := value
					if !fixture.Download {
						mbps = 0
					}
					c.Samples = append(c.Samples, domain.Sample{Success: value > 0, Mbps: mbps})
				}
				Summarize(&c)
				return c
			}
			values := []domain.CandidateResult{makeCandidate("151.101.1.1", false, fixture.CandidateSpeeds, fixture.CandidateLatency)}
			if fixture.HasBaseline {
				values = append(values, makeCandidate("151.101.0.1", true, fixture.BaseSpeeds, fixture.BaseLatency))
			}
			RankSmooth(values)
			if values[0].SmoothRounds != fixture.CandidateFast || values[0].MedianMbps != fixture.CandidateMedian {
				t.Fatal("原版排名分數不一致")
			}
			best, _ := Select(values, 4, domain.DefaultSettings().Probe, fixture.Download)
			if (best != "") != fixture.Apply {
				t.Fatalf("原版 apply=%v，本案 best=%s", fixture.Apply, best)
			}
		})
	}
}

func TestQualificationPrecedesTopTenAndDoesNotStopAtThirty(t *testing.T) {
	s := domain.DefaultSettings()
	now := time.Now()
	r := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/18"}, FetchedAt: now, ExpiresAt: now.Add(time.Hour)}
	r.Digest = domain.Digest(r.CIDRs)
	e := New()
	e.Lookup = func(context.Context, string) ([]net.IPAddr, error) { return nil, errors.New("沒有基準") }
	e.TCP = func(_ context.Context, ip string) (float64, error) {
		a := netip.MustParseAddr(ip).As4()
		return 20 + float64(a[2])*.5, nil
	}
	e.checkRoute = func(_ context.Context, _ domain.DomainEntry, _ profile, ip string, _ domain.RangeSnapshot, _ domain.ProbeSettings) domain.Sample {
		a := netip.MustParseAddr(ip).As4()
		return domain.Sample{Success: a[2] >= 30 && a[2] < 40, Stage: "tls", Error: "探索診斷", ActualIP: ip}
	}
	d := domain.DetectionResult{Classification: "confirmed", ExpiresAt: now.Add(time.Minute), Candidates: []string{"151.101.0.49"}}
	result := e.Run(context.Background(), domain.DomainEntry{ID: "target", Hostname: "one.example.net"}, s, d, r, domain.NetworkSnapshot{Fingerprint: "test"}, nil, &Budget{Limit: 100_000_000})
	if !result.Complete || len(result.Candidates) != 10 || len(result.Rejected) != 54 {
		t.Fatalf("資格後名額錯誤：complete=%v qualified=%d rejected=%d", result.Complete, len(result.Candidates), len(result.Rejected))
	}
	for _, c := range result.Candidates {
		if netip.MustParseAddr(c.IP).As4()[2] < 30 || c.Successes != 1 {
			t.Fatal("淘汰路線占據正式候選")
		}
	}
}
