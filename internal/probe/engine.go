package probe

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/fastly"
)

type Engine struct {
	root          *Engine
	limits        map[string]chan struct{}
	precheckMu    sync.Mutex
	prechecks     map[string]*tcpCheck
	checkRoute    func(context.Context, domain.DomainEntry, profile, string, domain.RangeSnapshot, domain.ProbeSettings) domain.Sample
	HTTPS         HTTPS
	Lookup        func(context.Context, string) ([]net.IPAddr, error)
	TCP           func(context.Context, string) (float64, error)
	DiscoveryHTTP *http.Client
	RefreshDNS    func(context.Context, domain.DomainEntry, domain.RangeSnapshot) domain.DetectionResult
	Progress      func(int, int, string)
}

func New() *Engine { return &Engine{Lookup: net.DefaultResolver.LookupIPAddr} }

// Fork 共用同作業的預掃與全域上限，但不共用各網域的進度回呼。
func (e *Engine) Fork(progress func(int, int, string)) *Engine {
	root := e
	if e.root != nil {
		root = e.root
	}
	return &Engine{root: root, HTTPS: e.HTTPS, Lookup: e.Lookup, TCP: e.TCP, DiscoveryHTTP: e.DiscoveryHTTP, RefreshDNS: e.RefreshDNS, checkRoute: e.checkRoute, Progress: progress}
}

func (e *Engine) slots(stage string, capacity int) chan struct{} {
	root := e
	if e.root != nil {
		root = e.root
	}
	root.precheckMu.Lock()
	defer root.precheckMu.Unlock()
	if root.limits == nil {
		return make(chan struct{}, capacity)
	}
	if root.limits[stage] == nil {
		root.limits[stage] = make(chan struct{}, capacity)
	}
	return root.limits[stage]
}

// Prepared 只含該網域自己的資格結果；下載留待作業依公平順序串行執行。
type Prepared struct {
	result         domain.TestResult
	checks         []domain.Sample
	profile        profile
	entry          domain.DomainEntry
	settings       domain.Settings
	detection      domain.DetectionResult
	ranges         domain.RangeSnapshot
	network        domain.NetworkSnapshot
	reference      *domain.SharedReference
	targets        []string
	wantsReference bool
	reused         bool
}

func (e *Engine) check(ctx context.Context, entry domain.DomainEntry, p profile, ip string, r domain.RangeSnapshot, s domain.ProbeSettings) domain.Sample {
	if e.checkRoute != nil {
		return e.checkRoute(ctx, entry, p, ip, r, s)
	}
	// 服務資格探測對齊原版三秒 socket 期限，下載使用一般 I/O 設定。
	s.ConnectionSeconds, s.SampleSeconds = 3, 3
	sample := e.HTTPS.measure(ctx, p.probeEntry(), ip, s, nil, false, p.anyStatus)
	if !p.anyStatus && sample.HTTPStatus >= 300 && sample.HTTPStatus < 400 {
		sample.Success = true
		sample.Error = ""
		sample.Stage = "complete"
	}
	if sample.Success && p.host != entry.Hostname {
		// 同服務資源是路線測試，不取代原目標 Host/SNI 的個別驗證。
		actual := e.HTTPS.measure(ctx, domain.DomainEntry{Hostname: entry.Hostname}, ip, s, nil, false, true)
		if !actual.Success {
			return actual
		}
	}
	return sample
}

func (e *Engine) Run(ctx context.Context, entry domain.DomainEntry, s domain.Settings, d domain.DetectionResult, r domain.RangeSnapshot, n domain.NetworkSnapshot, current *domain.Rule, budget *Budget) domain.TestResult {
	return e.run(ctx, entry, s, d, r, n, current, budget, false)
}

func (e *Engine) run(ctx context.Context, entry domain.DomainEntry, s domain.Settings, d domain.DetectionResult, r domain.RangeSnapshot, n domain.NetworkSnapshot, current *domain.Rule, budget *Budget, connectionOnly bool) domain.TestResult {
	ctx, cancel := JobContext(ctx, s.Probe.JobSeconds)
	defer cancel()
	prepared := e.Prepare(ctx, entry, s, d, r, n, current)
	if connectionOnly {
		prepared.profile.speed = ""
	}
	return e.Finish(ctx, prepared, budget)
}

func (e *Engine) Prepare(ctx context.Context, entry domain.DomainEntry, s domain.Settings, d domain.DetectionResult, r domain.RangeSnapshot, n domain.NetworkSnapshot, current *domain.Rule) *Prepared {
	p := service(entry)
	result := domain.TestResult{StrategyVersion: domain.ProbeStrategyVersion, Service: p.name, Mode: "connection", ProbeHost: p.host, ID: domain.ID(), DomainID: entry.ID, Hostname: entry.Hostname, NetworkFingerprint: n.Fingerprint, SettingsRevision: s.Revision, ObservedAt: time.Now().UTC(), Candidates: []domain.CandidateResult{}, AutomaticEligible: d.Classification == "confirmed" && n.Error == ""}
	result.DNSClassification = d.Classification
	prepared := &Prepared{profile: p, entry: entry, settings: s, detection: d, ranges: r, network: n}
	defer func() { prepared.result = result }()
	if !fastly.Valid(r, time.Now()) {
		result.Reason = "官方網段快取失效"
		return prepared
	}
	ipv6 := n.IPv6 && s.Probe.EnableIPv6
	scanDetection := d
	if p.suffix >= 0 {
		scanDetection.Candidates = slices.Clone(d.Candidates)
		for i, ip := range scanDetection.Candidates {
			scanDetection.Candidates[i] = p.routeIP(ip, r)
		}
	}
	if p.suffix >= 0 && !slices.ContainsFunc(d.Candidates, func(ip string) bool { a, err := netip.ParseAddr(ip); return err == nil && a.Is4() }) {
		// 內建服務不因系統 DNS 暫時失敗而失去官方路線探索。
		for _, cidr := range r.CIDRs {
			if prefix, err := netip.ParsePrefix(cidr); err == nil && prefix.Addr().Is4() {
				scanDetection.Candidates = append(slices.Clone(d.Candidates), p.routeIP(prefix.Addr().String(), r))
				break
			}
		}
	}
	addresses := Candidates(scanDetection, r, current, s.Probe, ipv6)
	baseline := map[string]bool{}
	addBaseline := func(ip string) {
		a, err := netip.ParseAddr(ip)
		if err == nil && domain.PublicIP(a) && (ipv6 || a.Is4()) && len(baseline) < 16 {
			baseline[a.String()] = true
			if !slices.Contains(addresses, a.String()) {
				addresses = append(addresses, a.String())
			}
		}
	}
	lookup := e.Lookup
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	lctx, lcancel := context.WithTimeout(ctx, 5*time.Second)
	system, err := lookup(lctx, entry.Hostname)
	lcancel()
	if err == nil {
		for _, ip := range system {
			addBaseline(ip.IP.String())
		}
	}
	if current != nil {
		addBaseline(current.Via)
		addBaseline(current.ViaV6)
	}
	near, count := e.scan(ctx, addresses, baseline, entry, r, s.Probe)
	result.ScanCount = count
	qualified := make([]domain.CandidateResult, len(near))
	preflight := make([]domain.Sample, len(near))
	workers, tries := 16, 3
	if s.Probe.ScanMode == "full" {
		workers, tries = 48, 1
	}
	slots := e.slots("qualification", workers)
	var wg sync.WaitGroup
	for i, v := range near {
		wg.Go(func() {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			a, _ := netip.ParseAddr(v.ip)
			family := 4
			if a.Is6() {
				family = 6
			}
			c := domain.CandidateResult{IP: v.ip, Family: family, Baseline: baseline[v.ip], Samples: []domain.Sample{}}
			latencies := []float64{}
			for attempt := 0; attempt < tries; attempt++ {
				if value, err := e.tcpWithin(ctx, v.ip, 3*time.Second); err == nil {
					latencies = append(latencies, value)
				}
			}
			c.MedianLatency = median(latencies)
			qualified[i] = c
			if len(latencies) > 0 {
				preflight[i] = e.check(ctx, entry, p, v.ip, r, s.Probe)
			} else {
				preflight[i] = domain.Sample{Stage: "tcp", ActualIP: v.ip, Hostname: entry.Hostname, Error: "服務 TCP 驗證失敗"}
			}
		})
	}
	wg.Wait()
	if s.Probe.ScanMode == "full" {
		retestSlots := e.slots("retest", 16)
		for i := range qualified {
			if !preflight[i].Success {
				continue
			}
			wg.Go(func() {
				select {
				case retestSlots <- struct{}{}:
					defer func() { <-retestSlots }()
				case <-ctx.Done():
					return
				}
				select {
				case slots <- struct{}{}:
					defer func() { <-slots }()
				case <-ctx.Done():
					return
				}
				values := []float64{}
				for attempt := 0; attempt < 3; attempt++ {
					if value, err := e.tcpWithin(ctx, qualified[i].IP, 3*time.Second); err == nil {
						values = append(values, value)
					}
				}
				if len(values) > 0 {
					qualified[i].MedianLatency = median(values)
				}
			})
		}
		wg.Wait()
	}
	indices := make([]int, 0, len(near))
	for i, c := range qualified {
		if c.IP != "" {
			indices = append(indices, i)
		}
	}
	slices.SortFunc(indices, func(a, b int) int {
		if preflight[a].Success != preflight[b].Success {
			if preflight[a].Success {
				return -1
			}
			return 1
		}
		if qualified[a].MedianLatency < qualified[b].MedianLatency {
			return -1
		}
		if qualified[a].MedianLatency > qualified[b].MedianLatency {
			return 1
		}
		return strings.Compare(qualified[a].IP, qualified[b].IP)
	})
	checks := []domain.Sample{}
	counts := map[int]int{}
	minLatency := map[int]float64{}
	for _, i := range indices {
		c := qualified[i]
		if !preflight[i].Success {
			c.Samples = []domain.Sample{preflight[i]}
			result.Rejected = append(result.Rejected, c)
			continue
		}
		c.POP = popOf(preflight[i].POP)
		if fastly.Match(c.IP, r) != "" {
			if _, ok := minLatency[c.Family]; !ok {
				minLatency[c.Family] = c.MedianLatency
			}
		}
		inTop := fastly.Match(c.IP, r) != "" && counts[c.Family] < s.Probe.Candidates && (s.Probe.ScanMode == "full" || c.MedianLatency <= minLatency[c.Family]+60)
		if inTop || c.Baseline {
			result.Candidates = append(result.Candidates, c)
			checks = append(checks, preflight[i])
			if inTop {
				counts[c.Family]++
			}
		}
	}
	prepared.checks = checks
	return prepared
}

func (e *Engine) Finish(ctx context.Context, prepared *Prepared, budget *Budget) domain.TestResult {
	result, checks := prepared.result, prepared.checks
	p, entry, s, d, r, n := prepared.profile, prepared.entry, prepared.settings, prepared.detection, prepared.ranges, prepared.network
	before := budget.Used()
	ipv6 := n.IPv6 && s.Probe.EnableIPv6
	if p.speed != "" {
		slots := e.slots("download", 1)
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		case <-ctx.Done():
			return unfinished(result, contextReason(ctx))
		}
	}
	// 同批資格掃描及等待下載可能超過短 TTL；取得實際下載槽後才重查，不能硬延長舊證據。
	if reason := e.refreshDetection(ctx, prepared, true); reason != "" {
		return unfinished(result, reason)
	}
	d = prepared.detection
	result.DNSClassification = d.Classification
	result.AutomaticEligible = result.AutomaticEligible && d.Classification == "confirmed"
	if len(result.Candidates) == 0 {
		reason := result.Reason
		if reason == "" {
			reason = "沒有通過自身 TCP／TLS／HTTPS 驗證的候選路線"
		}
		return unfinished(result, reason)
	}
	var err error
	resource := ""
	warning := ""
	budgetInsufficient := p.speed != "" && !budget.CanSample(8*1024*1024)
	if p.speed != "" && len(result.Candidates) > 0 {
		resource, err = e.resource(ctx, p, entry, result.Candidates, r, s.Probe, budget)
		if err != nil {
			warning = "測速資源取得失敗，改以服務 TCP 延遲比較"
			budgetInsufficient = budgetInsufficient || !budget.CanSample(8*1024*1024)
		} else {
			result.Mode = "download"
			u, _ := url.Parse(resource)
			result.SpeedHost = u.Hostname()
		}
	}
	done := 0
	total := len(result.Candidates) * s.Probe.Rounds
rounds:
	for round := 0; resource != "" && round < s.Probe.Rounds; round++ {
		if round > 0 && resource != "" && p.speed == "github" {
			resource, err = e.resource(ctx, p, entry, result.Candidates, r, s.Probe, budget)
			if err != nil {
				resource = ""
				warning = "測速資源更新失敗，捨棄本次下載評分，改以服務 TCP 延遲比較"
				budgetInsufficient = budgetInsufficient || !budget.CanSample(8*1024*1024)
				break
			}
		}
		// 原版每輪維持相同候選順序，不輪替起點。
		for i := range result.Candidates {
			if ctx.Err() != nil {
				break rounds
			}
			c := &result.Candidates[i]
			if !budget.CanSample(8 * 1024 * 1024) {
				budgetInsufficient = true
				break rounds
			}
			u, _ := url.Parse(resource)
			sample := e.HTTPS.Measure(ctx, domain.DomainEntry{Hostname: u.Hostname(), TestURL: resource}, c.IP, s.Probe, budget, true)
			c.Samples = append(c.Samples, sample)
			done++
			if e.Progress != nil {
				e.Progress(done, total, entry.Hostname+" · 多輪服務測試")
			}
		}
	}
	required := s.Probe.Rounds
	if resource == "" {
		result.Mode, result.SpeedHost = "connection", ""
		required = 1
		for i := range result.Candidates {
			// 延遲使用資格階段的 TCP 中位數與一次 HTTPS 驗證，並非三輪下載。
			result.Candidates[i].Samples = []domain.Sample{checks[i]}
		}
	}
	result.Bytes = budget.Used() - before
	for i := range result.Candidates {
		Summarize(&result.Candidates[i])
	}
	RankSmooth(result.Candidates)
	// 三輪下載也可能超過 TTL；只在確實到期時再查一次，失敗不能保留可套用建議。
	evidenceReason := e.refreshDetection(ctx, prepared, false)
	d = prepared.detection
	result.DNSClassification = d.Classification
	result.Complete = len(result.Candidates) > 0 && !slices.ContainsFunc(result.Candidates, func(c domain.CandidateResult) bool { return len(c.Samples) != required }) && ctx.Err() == nil && !budgetInsufficient
	result.Complete = result.Complete && evidenceReason == ""
	result.BestIPv4, result.Emergency = Select(result.Candidates, 4, s.Probe, result.Mode == "download")
	if ipv6 {
		var emergency bool
		result.BestIPv6, emergency = Select(result.Candidates, 6, s.Probe, result.Mode == "download")
		result.Emergency = result.Emergency || emergency
	}
	result.ObservedAt = time.Now().UTC()
	result.ExpiresAt = result.ObservedAt.Add(15 * time.Minute)
	if r.ExpiresAt.Before(result.ExpiresAt) {
		result.ExpiresAt = r.ExpiresAt
	}
	if d.ExpiresAt.Before(result.ExpiresAt) {
		result.ExpiresAt = d.ExpiresAt
	}
	if ctx.Err() != nil || !result.Complete {
		reason := evidenceReason
		if reason == "" && budgetInsufficient {
			reason = "下載流量額度不足，無法完成必要樣本"
		}
		if reason == "" {
			reason = "下載樣本數不足，未完成必要輪數"
		}
		result = unfinished(result, reason)
	} else if result.BestIPv4 == "" && result.BestIPv6 == "" {
		result.Reason = "樣本不足或無改善，維持現用路線"
	} else {
		result.Reason = "服務路線通過 TLS 與 TCP 延遲比較"
		if result.Mode == "download" {
			result.Reason = "服務路線通過 TLS 與三輪下載評分"
		}
	}
	if warning != "" {
		result.Reason += "；" + warning
	}
	if d.Classification != "confirmed" {
		result.Reason += "；雙來源證據未達自動套用條件"
	}
	if prepared.wantsReference {
		return applyReference(ctx, result, prepared, r, n)
	}
	return result
}

func JobContext(ctx context.Context, seconds int) (context.Context, context.CancelFunc) {
	if seconds <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sort.Float64s(v)
	if len(v)%2 == 1 {
		return v[len(v)/2]
	}
	return (v[len(v)/2-1] + v[len(v)/2]) / 2
}

func Summarize(c *domain.CandidateResult) {
	speeds, latencies := []float64{}, []float64{}
	c.Successes = 0
	for _, s := range c.Samples {
		speed := float64(0)
		if s.Success {
			c.Successes++
			speed = s.Mbps
		}
		speeds = append(speeds, speed)
		if s.TCPMilliseconds > 0 {
			latencies = append(latencies, s.TCPMilliseconds)
		}
	}
	c.MedianMbps = median(speeds)
	if c.MedianLatency == 0 {
		c.MedianLatency = median(latencies)
	}
}

func RankSmooth(candidates []domain.CandidateResult) {
	peak := map[int]float64{}
	for _, c := range candidates {
		for _, s := range c.Samples {
			if s.Success && s.Mbps > peak[c.Family] {
				peak[c.Family] = s.Mbps
			}
		}
	}
	for i := range candidates {
		c := &candidates[i]
		c.SmoothRounds = 0
		c.SmoothThresholdMbps = max(peak[c.Family]*.3, 3)
		for _, s := range c.Samples {
			if s.Success && s.Mbps >= c.SmoothThresholdMbps {
				c.SmoothRounds++
			}
		}
	}
}

func Select(candidates []domain.CandidateResult, family int, p domain.ProbeSettings, speed bool) (string, bool) {
	good, base := []domain.CandidateResult{}, []domain.CandidateResult{}
	for _, c := range candidates {
		if c.Family != family {
			continue
		}
		if c.Baseline {
			base = append(base, c)
		}
		if c.Successes > 0 && (!speed && len(c.Samples) > 0 || speed && len(c.Samples) == p.Rounds) {
			good = append(good, c)
		}
	}
	better := func(a, b domain.CandidateResult) int {
		if speed {
			if a.SmoothRounds != b.SmoothRounds {
				return b.SmoothRounds - a.SmoothRounds
			}
			if a.MedianMbps > b.MedianMbps {
				return -1
			}
			if a.MedianMbps < b.MedianMbps {
				return 1
			}
		}
		if a.MedianLatency < b.MedianLatency {
			return -1
		}
		if a.MedianLatency > b.MedianLatency {
			return 1
		}
		return strings.Compare(a.IP, b.IP)
	}
	slices.SortFunc(good, better)
	slices.SortFunc(base, better)
	if len(good) == 0 {
		return "", false
	}
	if len(base) == 0 {
		if speed {
			if good[0].SmoothRounds > 0 {
				return good[0].IP, false
			}
			return "", false
		}
		return good[0].IP, false
	}
	best, baseline := good[0], base[0]
	if best.Baseline || best.IP == baseline.IP {
		return "", false
	}
	if speed && len(baseline.Samples) != p.Rounds {
		return "", false
	}
	if speed && best.SmoothRounds > baseline.SmoothRounds {
		return best.IP, false
	}
	if !speed && baseline.MedianLatency > 0 && best.MedianLatency < baseline.MedianLatency-10 {
		return best.IP, false
	}
	return "", false
}

func popOf(value string) string {
	parts := strings.Split(value, ",")
	for index := len(parts) - 1; index >= 0; index-- {
		bits := strings.Split(strings.TrimSpace(parts[index]), "-")
		last := bits[len(bits)-1]
		if len(bits) >= 3 && bits[0] == "cache" && len(last) == 3 && strings.IndexFunc(last, func(r rune) bool { return r < 'A' || r > 'Z' && r < 'a' || r > 'z' }) < 0 {
			return strings.ToUpper(last)
		}
	}
	return "?"
}
