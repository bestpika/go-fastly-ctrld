package probe

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

func freshEvidence(entry domain.DomainEntry, classification string) domain.DetectionResult {
	now := time.Now().UTC()
	d := domain.DetectionResult{DomainID: entry.ID, Hostname: entry.Hostname, Classification: classification, ObservedAt: now, ExpiresAt: now.Add(time.Minute), Candidates: []string{"151.101.0.133"}}
	for _, source := range []string{"Google", "Cloudflare"} {
		d.Sources = append(d.Sources, domain.DNSObservation{Source: source, ObservedAt: now, Records: []domain.DNSRecord{{Name: entry.Hostname, Type: "A", Value: "151.101.0.133", TTL: 60}}})
	}
	return d
}

func TestFinishRefreshesExpiredQualificationEvidenceWithoutExtendingTTL(t *testing.T) {
	for _, download := range []bool{false, true} {
		t.Run(map[bool]string{false: "連線", true: "下載"}[download], func(t *testing.T) {
			e := originalFlowEngine(t, nil)
			s, _, ranges := originalFlowInput()
			entry := domain.DomainEntry{ID: "target", Hostname: "example.com"}
			if download {
				entry.TestURL = "https://example.com/test.bin"
			}
			p := e.Prepare(context.Background(), entry, s, freshEvidence(entry, "confirmed"), ranges, domain.NetworkSnapshot{}, nil)
			p.detection.ExpiresAt = time.Now().Add(-time.Second)
			calls := 0
			fresh := freshEvidence(entry, "confirmed")
			// 來源可正常輪替 IP，不要求兩家 DoH 或新舊 IP 完全相同。
			fresh.Candidates = []string{"151.101.1.133"}
			e.RefreshDNS = func(_ context.Context, got domain.DomainEntry, _ domain.RangeSnapshot) domain.DetectionResult {
				calls++
				if got.ID != entry.ID || got.Hostname != entry.Hostname {
					t.Fatal("重查錯誤網域")
				}
				return fresh
			}
			result := e.Finish(context.Background(), p, &Budget{})
			if !result.Complete || calls != 1 || !result.ExpiresAt.Equal(fresh.ExpiresAt) || !p.Detection().ObservedAt.Equal(fresh.ObservedAt) || len(result.Candidates) == 0 {
				t.Fatalf("未實查或未綁最新 TTL：%+v calls=%d", result, calls)
			}
			for _, candidate := range result.Candidates {
				want := 1
				if download {
					want = 3
				}
				if len(candidate.Samples) != want {
					t.Fatal("更新 DNS 省略了必要量測")
				}
			}
		})
	}
}

func TestDownloadExpiryRechecksRealEvidenceAndKeepsUsedBytesOnFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "完成後更新", true: "更新失敗"}[fail], func(t *testing.T) {
			var downloads atomic.Int32
			e := originalFlowEngine(t, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					downloads.Add(1)
				}
				_, _ = io.WriteString(w, "真實短檔本文")
			}))
			s, _, ranges := originalFlowInput()
			entry := domain.DomainEntry{ID: "target", Hostname: "example.com", TestURL: "https://example.com/test.bin"}
			p := e.Prepare(context.Background(), entry, s, freshEvidence(entry, "confirmed"), ranges, domain.NetworkSnapshot{}, nil)
			calls := 0
			e.RefreshDNS = func(context.Context, domain.DomainEntry, domain.RangeSnapshot) domain.DetectionResult {
				calls++
				d := freshEvidence(entry, "confirmed")
				if fail && calls == 2 {
					d.Sources[1].Error = "模擬重新查詢失敗"
				}
				return d
			}
			e.Progress = func(done, _ int, _ string) {
				if done == 1 {
					p.detection.ExpiresAt = time.Now().Add(-time.Second)
				}
			}
			budget := &Budget{}
			result := e.Finish(context.Background(), p, budget)
			if calls != 2 || downloads.Load() != 6 || result.Bytes <= 0 || result.Bytes != budget.Used() || result.Complete == fail {
				t.Fatalf("三輪後沒有安全更新／記帳：%+v calls=%d downloads=%d", result, calls, downloads.Load())
			}
			if fail && (result.BestIPv4 != "" || result.AutomaticEligible || !strings.Contains(result.Reason, "DNS 雙來源查詢失敗")) {
				t.Fatal("重查失敗仍留下可套用建議")
			}
		})
	}
}

type downloadWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *downloadWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestDNSRefreshHappensAfterWaitingForDownloadSlot(t *testing.T) {
	e := originalFlowEngine(t, nil)
	s, _, ranges := originalFlowInput()
	entry := domain.DomainEntry{ID: "target", Hostname: "example.com", TestURL: "https://example.com/test.bin"}
	p := e.Prepare(context.Background(), entry, s, freshEvidence(entry, "confirmed"), ranges, domain.NetworkSnapshot{}, nil)
	e.BeginScanSession()
	defer e.EndScanSession()
	slots := e.slots("download", 1)
	slots <- struct{}{}
	refreshed := make(chan struct{}, 1)
	e.RefreshDNS = func(context.Context, domain.DomainEntry, domain.RangeSnapshot) domain.DetectionResult {
		refreshed <- struct{}{}
		return freshEvidence(entry, "confirmed")
	}
	parent, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx := &downloadWaitContext{Context: parent, waiting: make(chan struct{})}
	finished := make(chan domain.TestResult, 1)
	go func() { finished <- e.Finish(ctx, p, &Budget{}) }()
	select {
	case <-ctx.waiting:
	case <-parent.Done():
		t.Fatal("未進入下載槽等待")
	}
	select {
	case <-refreshed:
		t.Fatal("等待下載槽前就重查，無法涵蓋等待期間到期")
	default:
	}
	// Finish 已等待槽位；此時才使舊證據過期，模擬前一網域下載耗時。
	p.detection.ExpiresAt = time.Now().Add(-time.Second)
	<-slots
	result := <-finished
	if !result.Complete || len(refreshed) != 1 || !result.ExpiresAt.Equal(p.Detection().ExpiresAt) {
		t.Fatalf("取得下載槽後未更新有效證據：%+v", result)
	}
}

func TestEvidenceFailureReasonsAndMixedManualPolicy(t *testing.T) {
	for _, cause := range []string{"來源失敗", "分類改變", "零 TTL", "錯誤身分", "取消", "總期限", "官方過期", "無合格候選", "下載額度", "混合維持"} {
		t.Run(cause, func(t *testing.T) {
			e := originalFlowEngine(t, nil)
			s, _, ranges := originalFlowInput()
			entry := domain.DomainEntry{ID: "target", Hostname: "example.com"}
			classification := "confirmed"
			if cause == "混合維持" {
				classification = "mixed"
			}
			if cause == "下載額度" {
				entry.TestURL = "https://example.com/test.bin"
			}
			p := e.Prepare(context.Background(), entry, s, freshEvidence(entry, classification), ranges, domain.NetworkSnapshot{}, nil)
			ctx := context.Background()
			budget := &Budget{}
			want := ""
			e.RefreshDNS = func(context.Context, domain.DomainEntry, domain.RangeSnapshot) domain.DetectionResult {
				d := freshEvidence(entry, classification)
				switch cause {
				case "來源失敗":
					d.Sources[0].Error = "失敗"
				case "分類改變":
					d.Classification = "mixed"
				case "零 TTL":
					d.ExpiresAt = time.Now()
				case "錯誤身分":
					d.Hostname = "other.example.com"
				}
				return d
			}
			switch cause {
			case "來源失敗":
				want = "DNS 雙來源查詢失敗"
			case "分類改變":
				want = "歸屬已變更"
			case "錯誤身分":
				want = "網域不符"
			case "取消":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx, want = cancelled, "測試已取消"
			case "總期限":
				expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
				ctx, want = expired, "測試總時間上限"
			case "官方過期":
				p.ranges.ExpiresAt = time.Now().Add(-time.Second)
			case "無合格候選":
				p.result.Candidates, p.result.Reason = nil, ""
				want = "沒有通過自身"
			case "下載額度":
				budget.Limit = 1
				want = "下載流量額度不足"
			}
			result := e.Finish(ctx, p, budget)
			if cause == "混合維持" || cause == "零 TTL" || cause == "官方過期" {
				if !result.Complete || cause == "混合維持" && (result.AutomaticEligible || result.DNSClassification != "mixed") {
					t.Fatal("既有 mixed 診斷／手動政策被改變")
				}
			} else if result.Complete || result.BestIPv4 != "" || result.AutomaticEligible || !strings.Contains(result.Reason, want) {
				t.Fatalf("失敗原因錯誤或仍可套用：%+v want=%s", result, want)
			}
			if cause != "下載額度" && strings.Contains(result.Reason, "額度不足") {
				t.Fatal("將 DNS／連線／期限問題誤稱為流量不足")
			}
		})
	}
}
