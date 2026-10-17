package probe

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

func TestUnlimitedBudgetAndOptionalJobDeadline(t *testing.T) {
	b := &Budget{}
	for range 4 {
		n, release := b.Reserve(8 << 20)
		if n != 8<<20 {
			t.Fatal("不限預算截斷樣本")
		}
		release(n)
	}
	if b.Used() != 32<<20 || !b.CanSample(8<<20) {
		t.Fatal("不限預算未計量")
	}
	ctx, cancel := JobContext(context.Background(), 0)
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("不限作業仍有總期限")
	}
	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("不限作業不能取消")
	}
	ctx, cancel = JobContext(context.Background(), 30)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("自訂總期限未生效")
	}
}

func TestForksShareGlobalQualificationLimits(t *testing.T) {
	for _, mode := range []string{"near", "full"} {
		t.Run(mode, func(t *testing.T) {
			s := domain.DefaultSettings()
			s.Probe.ScanMode = mode
			s.Probe.Candidates = 1
			now := time.Now()
			r := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/18"}, FetchedAt: now, ExpiresAt: now.Add(time.Hour)}
			r.Digest = domain.Digest(r.CIDRs)
			e := New()
			e.BeginScanSession()
			defer e.EndScanSession()
			e.Lookup = func(context.Context, string) ([]net.IPAddr, error) { return nil, nil }
			var active, peak atomic.Int32
			e.TCP = func(ctx context.Context, _ string) (float64, error) {
				deadline, _ := ctx.Deadline()
				if time.Until(deadline) > 2*time.Second {
					n := active.Add(1)
					for old := peak.Load(); n > old; old = peak.Load() {
						if peak.CompareAndSwap(old, n) {
							break
						}
					}
					time.Sleep(time.Millisecond)
					active.Add(-1)
				}
				return 10, nil
			}
			e.checkRoute = func(context.Context, domain.DomainEntry, profile, string, domain.RangeSnapshot, domain.ProbeSettings) domain.Sample {
				return domain.Sample{Success: true}
			}
			var wg sync.WaitGroup
			for _, host := range []string{"one.example.net", "two.example.net", "three.example.net", "four.example.net"} {
				wg.Go(func() {
					e.Fork(nil).Prepare(context.Background(), domain.DomainEntry{ID: host, Hostname: host}, s, domain.DetectionResult{ExpiresAt: now.Add(time.Minute), Candidates: []string{"151.101.0.49"}}, r, domain.NetworkSnapshot{}, nil)
				})
			}
			wg.Wait()
			limit := int32(16)
			if mode == "full" {
				limit = 48
			}
			if peak.Load() > limit || peak.Load() < 2 {
				t.Fatalf("跨網域超出全域上限：%d/%d", peak.Load(), limit)
			}
		})
	}
}

func TestForkDownloadsRemainSerial(t *testing.T) {
	var active, peak, requests atomic.Int32
	e := originalFlowEngine(t, nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		_, _ = io.WriteString(w, "實際下載處理不重疊")
	}))
	e.BeginScanSession()
	defer e.EndScanSession()
	s, d, r := originalFlowInput()
	n := domain.NetworkSnapshot{Fingerprint: "test"}
	var wg sync.WaitGroup
	for _, id := range []string{"one", "two"} {
		wg.Go(func() {
			result := e.Fork(nil).Run(context.Background(), domain.DomainEntry{ID: id, Hostname: "example.com", TestURL: "https://example.com/test.bin"}, s, d, r, n, nil, &Budget{})
			if !result.Complete {
				t.Error("本機下載未完成")
			}
		})
	}
	wg.Wait()
	if peak.Load() != 1 || active.Load() != 0 || requests.Load() != 12 {
		t.Fatalf("主要下載未串行或未完成十二個樣本：peak=%d active=%d requests=%d", peak.Load(), active.Load(), requests.Load())
	}
}

func TestSocketIdleTimeoutResetsAndParentCancellation(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := guardIO(ctx, left, 1).(*idleConn)
	defer c.Close()
	c.timeout = 500 * time.Millisecond
	go func() {
		for range 6 {
			time.Sleep(120 * time.Millisecond)
			if _, err := right.Write([]byte{1}); err != nil {
				return
			}
		}
	}()
	start := time.Now()
	for range 6 {
		if _, err := c.Read(make([]byte, 1)); err != nil {
			t.Fatalf("每次有資料仍被總期限截斷：%v", err)
		}
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Fatal("未跨過單次 I/O 期限")
	}
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("閒置未逾時")
	}
	c.timeout = time.Hour
	cancel()
	start = time.Now()
	if _, err := c.Read(make([]byte, 1)); err == nil || time.Since(start) > time.Second {
		t.Fatal("父層取消未關閉 socket")
	}
}

func TestNearPrecheckReusedAcrossServices(t *testing.T) {
	e := New()
	var calls atomic.Int32
	e.TCP = func(ctx context.Context, ip string) (float64, error) {
		calls.Add(1)
		if ip != "151.101.0.133" {
			t.Error("未使用共用 .133 初掃")
		}
		if d, ok := ctx.Deadline(); !ok || time.Until(d) > time.Second {
			t.Error("初掃非一秒期限")
		}
		return 10, nil
	}
	e.BeginScanSession()
	defer e.EndScanSession()
	for _, tail := range []string{"133", "214", "159"} {
		pool, _ := e.scan(context.Background(), []string{"151.101.0." + tail}, nil, domain.DomainEntry{Hostname: "example.com"}, domain.RangeSnapshot{}, domain.DefaultSettings().Probe)
		if len(pool) != 1 {
			t.Fatal("共用初掃未回傳路線")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("三服務重複 TCP 初掃")
	}
}

func TestNearAndFullWorkerLimitsAndThreeSecondServiceTCP(t *testing.T) {
	for _, mode := range []string{"near", "full"} {
		t.Run(mode, func(t *testing.T) {
			s := domain.DefaultSettings()
			s.Probe.ScanMode = mode
			s.Probe.Candidates = 1
			now := time.Now()
			r := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/17"}, FetchedAt: now, ExpiresAt: now.Add(time.Hour)}
			r.Digest = domain.Digest(r.CIDRs)
			e := New()
			e.Lookup = func(context.Context, string) ([]net.IPAddr, error) { return nil, errors.New("無基準") }
			var active, peak, precheck, service, initialActive, initialPeak atomic.Int32
			e.TCP = func(ctx context.Context, _ string) (float64, error) {
				d, _ := ctx.Deadline()
				if time.Until(d) < 2*time.Second {
					precheck.Add(1)
					a := initialActive.Add(1)
					for old := initialPeak.Load(); a > old; old = initialPeak.Load() {
						if initialPeak.CompareAndSwap(old, a) {
							break
						}
					}
					time.Sleep(2 * time.Millisecond)
					initialActive.Add(-1)
				} else {
					service.Add(1)
					a := active.Add(1)
					for old := peak.Load(); a > old; old = peak.Load() {
						if peak.CompareAndSwap(old, a) {
							break
						}
					}
					time.Sleep(2 * time.Millisecond)
					active.Add(-1)
				}
				return 20, nil
			}
			e.checkRoute = func(context.Context, domain.DomainEntry, profile, string, domain.RangeSnapshot, domain.ProbeSettings) domain.Sample {
				return domain.Sample{Success: true}
			}
			result := e.Run(context.Background(), domain.DomainEntry{ID: "target", Hostname: "one.example.net"}, s, domain.DetectionResult{Classification: "confirmed", ExpiresAt: now.Add(time.Minute), Candidates: []string{"151.101.0.49"}}, r, domain.NetworkSnapshot{Fingerprint: "test"}, nil, &Budget{})
			if !result.Complete {
				t.Fatal("本機假路線未完成")
			}
			if mode == "near" {
				if precheck.Load() != 128 || service.Load() != 384 || peak.Load() > 16 || peak.Load() < 2 || initialPeak.Load() > 64 || initialPeak.Load() <= 16 {
					t.Fatalf("近端 worker/tries 錯誤：pre=%d service=%d peak=%d", precheck.Load(), service.Load(), peak.Load())
				}
			} else {
				if precheck.Load() != 0 || service.Load() != 512 || peak.Load() > 48 || peak.Load() <= 16 {
					t.Fatalf("完整初驗／複測錯誤：pre=%d service=%d peak=%d", precheck.Load(), service.Load(), peak.Load())
				}
			}
		})
	}
}
