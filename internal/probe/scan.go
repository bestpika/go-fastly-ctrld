package probe

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/fastly"
)

func Candidates(d domain.DetectionResult, r domain.RangeSnapshot, current *domain.Rule, p domain.ProbeSettings, v6 bool) []string {
	result := []string{}
	seen := map[string]bool{}
	v6count := 0
	add := func(ip string) {
		a, err := netip.ParseAddr(ip)
		if err != nil || seen[ip] || fastly.Match(ip, r) == "" {
			return
		}
		if a.Is6() {
			if !v6 || !p.EnableIPv6 || v6count >= p.Candidates {
				return
			}
			v6count++
		}
		seen[ip] = true
		result = append(result, ip)
	}
	if current != nil {
		add(current.Via)
		add(current.ViaV6)
	}
	for _, ip := range d.Candidates {
		add(ip)
	}
	tail := -1
	for _, ip := range d.Candidates {
		a, err := netip.ParseAddr(ip)
		if err == nil && a.Is4() {
			v := a.As4()
			tail = int(v[3])
			break
		}
	}
	if tail < 0 {
		return result
	}
	// 對齊原版，遍歷官方 IPv4 的每個 /24，不另截斷或均勻取樣。
	for _, cidr := range r.CIDRs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || !prefix.Addr().Is4() {
			continue
		}
		v := prefix.Masked().Addr().As4()
		base := uint64(v[0])<<24 | uint64(v[1])<<16 | uint64(v[2])<<8 | uint64(v[3])
		size := uint64(1) << uint(32-prefix.Bits())
		for offset := uint64(0); offset < size; offset += 256 {
			value := base + offset
			ip := netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(tail)})
			if prefix.Contains(ip) {
				add(ip.String())
			}
		}
	}
	return result
}

func (e *Engine) tcp(ctx context.Context, ip string) (float64, error) {
	return e.tcpWithin(ctx, ip, time.Second)
}

func (e *Engine) tcpWithin(ctx context.Context, ip string, timeout time.Duration) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if e.TCP != nil {
		return e.TCP(ctx, ip)
	}
	a, err := netip.ParseAddr(ip)
	if err != nil || !domain.PublicIP(a) {
		return 0, domain.Fail("UNSAFE_TARGET", "不安全的 TCP 候選")
	}
	network := "tcp4"
	if a.Is6() {
		network = "tcp6"
	}
	start := time.Now()
	conn, err := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ip, "443"))
	if err != nil {
		return 0, err
	}
	conn.Close()
	return float64(time.Since(start)) / float64(time.Millisecond), nil
}

type scanned struct {
	ip      string
	latency float64
}

type tcpCheck struct {
	done    chan struct{}
	latency float64
	err     error
}

// 同一作業只對每個 /24 的 .133 預掃一次，所有網域共用並行上限。
func (e *Engine) BeginScanSession() {
	e.precheckMu.Lock()
	e.prechecks = map[string]*tcpCheck{}
	e.limits = map[string]chan struct{}{}
	e.precheckMu.Unlock()
}
func (e *Engine) EndScanSession() {
	e.precheckMu.Lock()
	e.prechecks = nil
	e.limits = nil
	e.precheckMu.Unlock()
}
func (e *Engine) precheck(ctx context.Context, ip string) (float64, error) {
	if e.root != nil {
		return e.root.precheck(ctx, ip)
	}
	e.precheckMu.Lock()
	if e.prechecks == nil {
		e.precheckMu.Unlock()
		return e.tcp(ctx, ip)
	}
	if previous, ok := e.prechecks[ip]; ok {
		e.precheckMu.Unlock()
		select {
		case <-previous.done:
			return previous.latency, previous.err
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	value := &tcpCheck{done: make(chan struct{})}
	e.prechecks[ip] = value
	e.precheckMu.Unlock()
	value.latency, value.err = e.tcp(ctx, ip)
	close(value.done)
	return value.latency, value.err
}

func (e *Engine) scan(ctx context.Context, addresses []string, baseline map[string]bool, entry domain.DomainEntry, ranges domain.RangeSnapshot, p domain.ProbeSettings) ([]scanned, int) {
	if p.ScanMode == "full" {
		all := make([]scanned, len(addresses))
		for i, ip := range addresses {
			all[i] = scanned{ip: ip}
		}
		return all, len(all)
	}
	values := make([]scanned, len(addresses))
	slots := e.slots("precheck", 64)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	for i, ip := range addresses {
		wg.Go(func() {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			precheckIP := ip
			if !baseline[ip] || fastly.Match(ip, ranges) != "" {
				if a, err := netip.ParseAddr(ip); err == nil && a.Is4() {
					v := a.As4()
					v[3] = 133
					precheckIP = netip.AddrFrom4(v).String()
				}
			}
			latency, err := e.precheck(ctx, precheckIP)
			if err == nil {
				values[i] = scanned{ip, latency}
			}
			mu.Lock()
			done++
			if e.Progress != nil {
				e.Progress(done, len(addresses), entry.Hostname+" · TCP 預掃")
			}
			mu.Unlock()
		})
	}
	wg.Wait()
	pool := []scanned{}
	for _, v := range values {
		if v.ip != "" {
			pool = append(pool, v)
		}
	}
	slices.SortFunc(pool, func(a, b scanned) int {
		if a.latency < b.latency {
			return -1
		}
		if a.latency > b.latency {
			return 1
		}
		return slices.Compare([]byte(a.ip), []byte(b.ip))
	})
	near := []scanned{}
	minimum := map[bool]float64{}
	for _, v := range pool {
		if fastly.Match(v.ip, ranges) != "" || !baseline[v.ip] {
			a, _ := netip.ParseAddr(v.ip)
			if _, ok := minimum[a.Is6()]; !ok {
				minimum[a.Is6()] = v.latency
			}
		}
	}
	for _, v := range pool {
		a, _ := netip.ParseAddr(v.ip)
		family := a.Is6()
		if baseline[v.ip] || v.latency <= minimum[family]+100 {
			near = append(near, v)
		}
	}
	for ip := range baseline {
		if !slices.ContainsFunc(near, func(v scanned) bool { return v.ip == ip }) {
			near = append(near, scanned{ip: ip})
		}
	}
	return near, done
}
