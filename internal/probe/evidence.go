package probe

import (
	"context"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/fastly"
)

// Detection 回傳最後實際取得的 DNS 證據，供作業保存，不以修改時間偽造新 TTL。
func (p *Prepared) Detection() domain.DetectionResult { return p.detection }

func contextReason(ctx context.Context) string {
	if ctx.Err() == context.DeadlineExceeded {
		return "已達測試總時間上限"
	}
	if ctx.Err() != nil {
		return "測試已取消"
	}
	return ""
}

func unfinished(result domain.TestResult, reason string) domain.TestResult {
	result.Complete, result.AutomaticEligible = false, false
	result.BestIPv4, result.BestIPv6 = "", ""
	result.Reason = "測試未完成：" + reason
	return result
}

func (e *Engine) refreshDetection(ctx context.Context, p *Prepared, force bool) string {
	if reason := contextReason(ctx); reason != "" {
		return reason
	}
	if !fastly.Valid(p.ranges, time.Now()) {
		return "Fastly 網段資料無效"
	}
	if e.RefreshDNS != nil && (force || !p.detection.ExpiresAt.After(time.Now())) {
		previous := p.detection.Classification
		d := e.RefreshDNS(ctx, p.entry, p.ranges)
		p.detection = d
		if reason := contextReason(ctx); reason != "" {
			return reason
		}
		if d.DomainID != p.entry.ID || d.Hostname != p.entry.Hostname {
			return "DNS 證據與網域不符"
		}
		if len(d.Sources) != 2 || d.Sources[0].Source != "Google" || d.Sources[1].Source != "Cloudflare" || d.Sources[0].Error != "" || d.Sources[1].Error != "" {
			return "DNS 雙來源查詢失敗"
		}
		if d.Classification != previous {
			return "DNS 歸屬已變更，請重新測試"
		}
		if p.wantsReference && p.reference != nil {
			p.targets = ReferenceTargets(*p.reference, d, p.ranges, p.network.IPv6 && p.settings.Probe.EnableIPv6)
		}
	}
	if !fastly.Valid(p.ranges, time.Now()) {
		return "Fastly 網段資料無效"
	}
	return ""
}
