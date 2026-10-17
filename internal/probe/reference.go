package probe

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/fastly"
)

const referenceSampleBytes = 8 << 20

func WantsReference(e domain.DomainEntry, p domain.ProbeSettings) bool {
	return e.TestURL == "" && !HasDownload(e) && !strings.HasPrefix(e.ID, "baseline-") && p.NoResourceMode == "shared"
}

// RouteKey 只在 IPv4 共用 /24；IPv6 必須完全相同，不猜位址。
func RouteKey(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil || !domain.PublicIP(a) {
		return ""
	}
	if a.Is4() {
		return netip.PrefixFrom(a, 24).Masked().String()
	}
	return a.String()
}

func ReferenceDigest(r domain.SharedReference) string { r.Digest = ""; return domain.Digest(r) }
func ReferenceKey(s domain.Settings, r domain.RangeSnapshot, n domain.NetworkSnapshot) string {
	return domain.Digest([]any{domain.ProbeStrategyVersion, s.Probe, r.Digest, n.Fingerprint})
}
func ValidReference(ref domain.SharedReference, s domain.Settings, r domain.RangeSnapshot, n domain.NetworkSnapshot, now time.Time) bool {
	if !fastly.Valid(r, now) || ref.ID == "" || ref.StrategyVersion != domain.ProbeStrategyVersion || ref.Digest != ReferenceDigest(ref) || ref.NetworkFingerprint != n.Fingerprint || n.Error != "" || ref.RangesDigest != r.Digest || ref.ProbeDigest != domain.Digest(s.Probe) || len(ref.Sources) != 2 {
		return false
	}
	for i, name := range []string{"Twitch", "GitHub"} {
		source := ref.Sources[i]
		if source.Name != name || len(source.Candidates) == 0 {
			return false
		}
		if name == "Twitch" && source.SpeedHost != "assets.twitch.tv" || name == "GitHub" && !strings.HasSuffix(source.SpeedHost, ".githubusercontent.com") {
			return false
		}
		for _, c := range source.Candidates {
			if fastly.Match(c.IP, r) == "" || len(c.Samples) > s.Probe.Rounds {
				return false
			}
			for _, sample := range c.Samples {
				if sample.ActualIP != c.IP || sample.Hostname != source.SpeedHost || sample.Bytes < 0 {
					return false
				}
			}
		}
	}
	return true
}

func (e *Engine) Baselines(ctx context.Context, entry domain.DomainEntry, current *domain.Rule) []string {
	result := []string{}
	add := func(ip string) {
		if RouteKey(ip) != "" && !slices.Contains(result, ip) && len(result) < 16 {
			result = append(result, ip)
		}
	}
	if current != nil {
		add(current.Via)
		add(current.ViaV6)
	}
	lookup := e.Lookup
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if ips, err := lookup(lctx, entry.Hostname); err == nil {
		for _, ip := range ips {
			add(ip.IP.String())
		}
	}
	return result
}

// BuildReference 用原版完整服務測試建立本輪參考，不在 TLS 資格前截下載名額。
func (e *Engine) BuildReference(ctx context.Context, s domain.Settings, r domain.RangeSnapshot, n domain.NetworkSnapshot, sources []domain.DetectionResult, baselines []string, budget *Budget) (domain.SharedReference, error) {
	if len(sources) != 2 || !budget.CanSample(referenceSampleBytes) {
		return domain.SharedReference{}, domain.Fail("BUDGET_EXHAUSTED", "不足原版完整樣本預算")
	}
	results := []domain.TestResult{}
	for index, entry := range BaselineEntries()[:2] {
		d := sources[1-index]
		for _, ip := range baselines {
			if fastly.Match(ip, r) != "" {
				d.Candidates = append(d.Candidates, service(entry).routeIP(ip, r))
			}
		}
		result := e.Run(ctx, entry, s, d, r, n, nil, budget)
		result.BaselineService = true
		results = append(results, result)
	}
	return ReferenceFromBaselines(results, s, r, n)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func Scores(ref domain.SharedReference, ip string) []domain.ReferenceScore {
	key := RouteKey(ip)
	result := []domain.ReferenceScore{}
	for _, source := range ref.Sources {
		for _, c := range source.Candidates {
			if RouteKey(c.IP) == key {
				result = append(result, domain.ReferenceScore{Source: source.Name, Route: key, ActualIP: c.IP, SpeedHost: source.SpeedHost, SmoothRounds: c.SmoothRounds, ThresholdMbps: c.SmoothThresholdMbps, MedianMbps: c.MedianMbps, Successes: c.Successes, Rounds: len(c.Samples)})
				break
			}
		}
	}
	return result
}

// PickShared 不合併不同來源 Mbps；來源相反、不完整或基準缺漏時回退目標延遲。
func PickShared(candidates []domain.CandidateResult, family int, p domain.ProbeSettings) (string, bool) {
	valid := func(c domain.CandidateResult) bool {
		return c.Successes > 0 && len(c.Samples) > 0 && len(c.ReferenceScores) == 2 && slices.ContainsFunc(c.ReferenceScores, func(v domain.ReferenceScore) bool { return v.Source == "Twitch" }) && slices.ContainsFunc(c.ReferenceScores, func(v domain.ReferenceScore) bool { return v.Source == "GitHub" }) && !slices.ContainsFunc(c.ReferenceScores, func(v domain.ReferenceScore) bool { return v.Rounds != p.Rounds })
	}
	base := []domain.CandidateResult{}
	good := []domain.CandidateResult{}
	for _, c := range candidates {
		if c.Family == family {
			if c.Baseline {
				base = append(base, c)
			} else if valid(c) {
				good = append(good, c)
			}
		}
	}
	if len(base) == 0 || !slices.ContainsFunc(base, valid) {
		return "", false
	}
	// 與所有可比較現用路線比，不能挑最差基準製造改善。
	for _, b := range base {
		if !valid(b) {
			return "", false
		}
	}
	accepted := []domain.CandidateResult{}
	for _, candidate := range good {
		all := true
		for _, baseline := range base {
			improved := false
			for _, score := range candidate.ReferenceScores {
				var old domain.ReferenceScore
				for _, v := range baseline.ReferenceScores {
					if v.Source == score.Source {
						old = v
					}
				}
				if score.SmoothRounds < old.SmoothRounds {
					all = false
				}
				if score.SmoothRounds > old.SmoothRounds {
					improved = true
				}
			}
			if !improved {
				all = false
			}
		}
		if all {
			accepted = append(accepted, candidate)
		}
	}
	if len(accepted) == 0 {
		return "", false
	}
	slices.SortFunc(accepted, func(a, b domain.CandidateResult) int {
		minimum := func(c domain.CandidateResult) int {
			value := p.Rounds
			for _, v := range c.ReferenceScores {
				value = min(value, v.SmoothRounds)
			}
			return value
		}
		if delta := minimum(b) - minimum(a); delta != 0 {
			return delta
		}
		relative := func(c domain.CandidateResult) float64 {
			value := 0.0
			for _, v := range c.ReferenceScores {
				worst := 0.0
				for _, old := range base {
					for _, score := range old.ReferenceScores {
						if score.Source == v.Source {
							worst = max(worst, score.MedianMbps)
						}
					}
				}
				if worst > 0 {
					value += v.MedianMbps/worst - 1
				}
			}
			return value
		}
		if relative(a) > relative(b) {
			return -1
		}
		if relative(a) < relative(b) {
			return 1
		}
		if a.MedianLatency < b.MedianLatency {
			return -1
		}
		if a.MedianLatency > b.MedianLatency {
			return 1
		}
		return strings.Compare(a.IP, b.IP)
	})
	return accepted[0].IP, true
}

func ReferenceTargets(ref domain.SharedReference, d domain.DetectionResult, r domain.RangeSnapshot, v6 bool) []string {
	tails := []byte{}
	out := []string{}
	actualDNS := []string{}
	for _, source := range d.Sources {
		for _, record := range source.Records {
			if (record.Type == "A" || record.Type == "AAAA") && fastly.Match(record.Value, r) != "" && !slices.Contains(actualDNS, record.Value) {
				actualDNS = append(actualDNS, record.Value)
			}
		}
	}
	for _, ip := range actualDNS {
		a, err := netip.ParseAddr(ip)
		if err == nil && a.Is4() {
			v := a.As4()
			if !slices.Contains(tails, v[3]) {
				tails = append(tails, v[3])
			}
		}
	}
	for _, source := range ref.Sources {
		for _, candidate := range source.Candidates {
			key := RouteKey(candidate.IP)
			if prefix, err := netip.ParsePrefix(key); err == nil {
				for _, tail := range tails {
					v := prefix.Addr().As4()
					v[3] = tail
					ip := netip.AddrFrom4(v).String()
					if fastly.Match(ip, r) != "" && !slices.Contains(out, ip) {
						out = append(out, ip)
					}
				}
			} else if v6 && slices.Contains(actualDNS, candidate.IP) && !slices.Contains(out, candidate.IP) {
				out = append(out, candidate.IP)
			}
		}
	}
	return out
}

func (e *Engine) RunWithReference(ctx context.Context, entry domain.DomainEntry, s domain.Settings, d domain.DetectionResult, r domain.RangeSnapshot, n domain.NetworkSnapshot, current *domain.Rule, budget *Budget, ref *domain.SharedReference, reused bool) domain.TestResult {
	ctx, cancel := JobContext(ctx, s.Probe.JobSeconds)
	defer cancel()
	return e.Finish(ctx, e.PrepareWithReference(ctx, entry, s, d, r, n, current, ref, reused), budget)
}

func (e *Engine) PrepareWithReference(ctx context.Context, entry domain.DomainEntry, s domain.Settings, d domain.DetectionResult, r domain.RangeSnapshot, n domain.NetworkSnapshot, current *domain.Rule, ref *domain.SharedReference, reused bool) *Prepared {
	if !WantsReference(entry, s.Probe) {
		return e.Prepare(ctx, entry, s, d, r, n, current)
	}
	targets := []string{}
	if ref == nil || !ValidReference(*ref, s, r, n, time.Now()) {
		ref = nil
	} else {
		targets = ReferenceTargets(*ref, d, r, n.IPv6 && s.Probe.EnableIPv6)
		d.Candidates = append(targets, d.Candidates...)
	}
	// 優先驗證參考路線，但不將來源的 samples 或 Mbps 填入目標自身成績。
	prepared := e.Prepare(ctx, entry, s, d, r, n, current)
	prepared.profile.speed = ""
	prepared.reference, prepared.targets, prepared.wantsReference, prepared.reused = ref, targets, true, reused
	return prepared
}

func applyReference(ctx context.Context, result domain.TestResult, prepared *Prepared, r domain.RangeSnapshot, n domain.NetworkSnapshot) domain.TestResult {
	ref, s, targets, reused := prepared.reference, prepared.settings, prepared.targets, prepared.reused
	if ref == nil || !ValidReference(*ref, s, r, n, time.Now()) {
		result.Reason += "；共享參考不足，改用自身延遲"
		return result
	}
	if ctx.Err() != nil {
		return result
	}
	for i := range result.Candidates {
		candidate := &result.Candidates[i]
		if candidate.Baseline || slices.Contains(targets, candidate.IP) {
			candidate.ReferenceScores = Scores(*ref, candidate.IP)
		}
	}
	copy := *ref
	copy.Sources = make([]domain.ReferenceSource, len(ref.Sources))
	for index, source := range ref.Sources {
		copy.Sources[index] = domain.ReferenceSource{Name: source.Name, ProbeHost: source.ProbeHost, SpeedHost: source.SpeedHost}
	}
	// 完整來源樣本只保存一份在 references cache，目標保留摘要與各路線分數。
	result.Reference = &copy
	result.ReferenceReused = reused
	if !result.Complete {
		return result
	}
	used := false
	for _, family := range []int{4, 6} {
		if family == 6 && (!n.IPv6 || !s.Probe.EnableIPv6) {
			continue
		}
		best, available := PickShared(result.Candidates, family, s.Probe)
		if available {
			used = true
			if family == 4 {
				result.BestIPv4 = best
			} else {
				result.BestIPv6 = best
			}
		}
	}
	if used {
		result.Mode = "shared-reference"
		result.ExpiresAt = minTime(result.ExpiresAt, ref.ExpiresAt)
		result.Emergency = false
		result.AutomaticEligible = result.AutomaticEligible && s.Automation.AllowSharedReference
		result.Reason = "共享路線參考，非自身下載成績"
		if !s.Automation.AllowSharedReference {
			result.Reason += "；未授權共享參考自動套用"
		}
	} else {
		result.Reason += "；共享路線未改善，改用自身延遲"
	}
	return result
}

func (e *Engine) VerifyTarget(ctx context.Context, entry domain.DomainEntry, ip string, s domain.Settings, r domain.RangeSnapshot) error {
	if fastly.Match(ip, r) == "" || !e.check(ctx, entry, service(entry), ip, r, s.Probe).Success {
		return domain.Fail("UNSAFE_TARGET", "目標自己的 TLS／HTTPS 重新驗證失敗")
	}
	return nil
}

// VerifyOriginalTarget 不做服務尾碼轉換，直接驗證原網域的 Host/SNI。
func (e *Engine) VerifyOriginalTarget(ctx context.Context, entry domain.DomainEntry, ip string, s domain.Settings, r domain.RangeSnapshot) error {
	if fastly.Match(ip, r) == "" || !e.HTTPS.measure(ctx, domain.DomainEntry{Hostname: entry.Hostname}, ip, s.Probe, nil, false, true).Success {
		return domain.Fail("UNSAFE_TARGET", "原始 DNS 目標的 TLS／HTTPS 驗證失敗")
	}
	return nil
}
