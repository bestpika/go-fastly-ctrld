package network

import (
	"context"
	"crypto/tls"
	"net"
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

type Inspector struct {
	mu        sync.Mutex
	base      string
	last      time.Time
	ipv6      bool
	reason    string
	CheckIPv6 func(context.Context) bool
}

func New() *Inspector { return &Inspector{CheckIPv6: verifyIPv6} }

func verifyIPv6(ctx context.Context) bool {
	for _, target := range []struct{ ip, hostname string }{{"2606:4700:4700::1111", "cloudflare-dns.com"}, {"2001:4860:4860::8888", "dns.google"}} {
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 3 * time.Second}, Config: &tls.Config{ServerName: target.hostname, MinVersion: tls.VersionTLS12}}
		conn, err := dialer.DialContext(probe, "tcp6", net.JoinHostPort(target.ip, "443"))
		cancel()
		if err == nil {
			conn.Close()
			return true
		}
		if ctx.Err() != nil {
			return false
		}
	}
	return false
}

func (i *Inspector) Snapshot(ctx context.Context) domain.NetworkSnapshot {
	i.mu.Lock()
	defer i.mu.Unlock()
	s := domain.NetworkSnapshot{ObservedAt: time.Now().UTC(), Interfaces: []string{}, Routes: []string{}, Resolvers: []string{}}
	interfaces, err := net.Interfaces()
	hasV6 := false
	if err != nil {
		s.Error = "無法讀取網路介面"
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			s.Error = "無法讀取網路位址"
			continue
		}
		for _, address := range addresses {
			s.Interfaces = append(s.Interfaces, iface.Name+"|"+address.String())
			p, err := netip.ParsePrefix(address.String())
			if err == nil && p.Addr().Is6() && p.Addr().IsGlobalUnicast() && !p.Addr().Is4In6() {
				hasV6 = true
			}
		}
	}
	s.Routes, s.Resolvers, err = platformDetails()
	if err != nil {
		s.Error = "無法讀取路由或 DNS 設定"
	}
	sort.Strings(s.Interfaces)
	sort.Strings(s.Routes)
	sort.Strings(s.Resolvers)
	base := domain.Digest([]any{s.Interfaces, s.Routes, s.Resolvers, s.Error})
	if !hasV6 {
		i.ipv6 = false
		i.reason = "未偵測到可用的非 link-local IPv6 位址"
		i.last = time.Now()
	} else if base != i.base || time.Since(i.last) >= 5*time.Minute {
		i.ipv6 = i.CheckIPv6(ctx)
		i.last = time.Now()
		if i.ipv6 {
			i.reason = "已驗證對外 IPv6 TLS 連線"
		} else {
			i.reason = "IPv6 對外 TLS 連線不可用；僅測試 IPv4"
		}
	}
	i.base = base
	s.IPv6 = i.ipv6
	s.IPv6Reason = i.reason
	s.Fingerprint = domain.Digest([]any{base, s.IPv6})
	return s
}
