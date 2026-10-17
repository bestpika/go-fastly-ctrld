package probe

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

func TestAllOfficialIPv4BlocksAndIPv6OptIn(t *testing.T) {
	p := domain.DefaultSettings().Probe
	r := domain.RangeSnapshot{CIDRs: []string{"151.96.0.0/11", "151.101.0.0/16", "2a04:4e42::/32"}}
	d := domain.DetectionResult{Candidates: []string{"151.101.0.49", "2a04:4e42::49"}}
	ips := Candidates(d, r, nil, p, true)
	if len(ips) != 8192 || !slices.Contains(ips, "151.96.1.49") || !slices.Contains(ips, "151.127.255.49") || slices.Contains(ips, "2a04:4e42::49") {
		t.Fatal("官方 /24 仍遭截斷／間隔取樣，或未選用 IPv6 就探測")
	}
	p.EnableIPv6 = true
	if got := Candidates(d, r, nil, p, true); len(got) != 8193 || !slices.Contains(got, "2a04:4e42::49") {
		t.Fatal("明確選用後仍未保留實際 IPv6")
	}
	if len(Candidates(d, r, nil, p, false)) != 8192 {
		t.Fatal("IPv6 未驗證可用仍探測")
	}
}

func TestSelectedBaselinesAndOriginalServiceDependencies(t *testing.T) {
	for _, tc := range []struct {
		name     string
		services []string
		mode     string
		entry    domain.DomainEntry
		want     []string
	}{
		{"預設三類十二域", nil, "latency", domain.DomainEntry{}, []string{"baseline-github", "baseline-twitch", "baseline-x", "baseline-github-raw", "baseline-twitch-www", "baseline-twitch-gql", "baseline-twitch-k", "baseline-twitch-static", "baseline-twitch-jtvnw", "baseline-x-abs", "baseline-x-video", "baseline-x-video-s"}},
		{"只測 Twitch 六域", []string{"twitch"}, "latency", domain.DomainEntry{}, []string{"baseline-twitch", "baseline-twitch-www", "baseline-twitch-gql", "baseline-twitch-k", "baseline-twitch-static", "baseline-twitch-jtvnw"}},
		{"X 四域不依賴 GitHub", []string{"x"}, "latency", domain.DomainEntry{}, []string{"baseline-x", "baseline-x-abs", "baseline-x-video", "baseline-x-video-s"}},
		{"全關", []string{}, "latency", domain.DomainEntry{}, []string{}},
		{"管理 X 不補依賴", []string{}, "latency", domain.DomainEntry{Hostname: "pbs.twimg.com"}, []string{}},
		{"明確選用共享", []string{}, "shared", domain.DomainEntry{ID: "target", Hostname: "example.com"}, []string{"baseline-github", "baseline-twitch"}},
		{"內建下載不借共享", []string{}, "shared", domain.DomainEntry{ID: "target", Hostname: "assets.twitch.tv"}, []string{}},
		{"內建僅連線也可明確共享", []string{}, "shared", domain.DomainEntry{ID: "target", Hostname: "www.twitch.tv"}, []string{"baseline-github", "baseline-twitch"}},
		{"自訂檔案優先", []string{}, "shared", domain.DomainEntry{ID: "target", Hostname: "example.com", TestURL: "https://example.com/file"}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.DefaultSettings().Probe
			p.Services, p.NoResourceMode = tc.services, tc.mode
			entries := []domain.DomainEntry{}
			if tc.entry.Hostname != "" {
				entries = append(entries, tc.entry)
			}
			got := []string{}
			for _, entry := range SelectedBaselines(p, entries) {
				got = append(got, entry.ID)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("依賴不符：%v", got)
			}
		})
	}
}

func originalFlowEngine(t *testing.T, dialed func(string), handlers ...http.Handler) *Engine {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"example.com", "release-assets.githubusercontent.com"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "小檔也保留原版樣本") })
	if len(handlers) > 0 {
		handler = handlers[0]
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	root, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	e := New()
	e.HTTPS = HTTPS{roots: roots, dial: func(ctx context.Context, _, address string) (net.Conn, error) {
		if dialed != nil {
			dialed(address)
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	e.Lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("151.101.0.133")}}, nil
	}
	e.TCP = func(_ context.Context, ip string) (float64, error) {
		if strings.HasPrefix(ip, "151.101.0.") {
			return 50, nil
		}
		return 10, nil
	}
	e.checkRoute = func(_ context.Context, _ domain.DomainEntry, _ profile, ip string, _ domain.RangeSnapshot, _ domain.ProbeSettings) domain.Sample {
		return domain.Sample{Success: true, ActualIP: ip}
	}
	return e
}

func originalFlowInput() (domain.Settings, domain.DetectionResult, domain.RangeSnapshot) {
	s := domain.DefaultSettings()
	s.Probe.ScanMode = "full"
	now := time.Now().UTC()
	d := domain.DetectionResult{Classification: "confirmed", ExpiresAt: now.Add(time.Minute), Candidates: []string{"151.101.0.133"}}
	r := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/23"}, FetchedAt: now, ExpiresAt: now.Add(time.Hour)}
	r.Digest = domain.Digest(r.CIDRs)
	return s, d, r
}

func TestEachDownloadRoundKeepsCandidateOrder(t *testing.T) {
	var order []string
	e := originalFlowEngine(t, func(address string) { order = append(order, address) })
	s, d, r := originalFlowInput()
	result := e.Run(context.Background(), domain.DomainEntry{ID: "target", Hostname: "example.com", TestURL: "https://example.com/file"}, s, d, r, domain.NetworkSnapshot{Fingerprint: "test"}, nil, &Budget{})
	want := []string{"151.101.1.133:443", "151.101.0.133:443", "151.101.1.133:443", "151.101.0.133:443", "151.101.1.133:443", "151.101.0.133:443"}
	if !result.Complete || result.Mode != "download" || !slices.Equal(order, want) {
		t.Fatalf("原版固定順序不符：%v %+v", order, result)
	}
}

func TestLaterResourceFailureDiscardsAllDownloadScores(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "退回自身延遲", true: "取消不可偽裝完成"}[cancelled], func(t *testing.T) {
			downloads := 0
			e := originalFlowEngine(t, func(string) { downloads++ })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			e.DiscoveryHTTP = &http.Client{Transport: resourceTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host == "api.github.com" {
					calls++
					if calls > 1 {
						if downloads != 2 {
							t.Errorf("更新失敗前沒有完成第一輪兩條路線下載：%d", downloads)
						}
						if cancelled {
							cancel()
						}
						return nil, errors.New("本機模擬資源更新失敗")
					}
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"assets":[{"size":100000,"browser_download_url":"https://github.com/cli/cli/releases/download/v1/file.bin"}]}`))}, nil
				}
				if req.URL.Hostname() == "release-assets.githubusercontent.com" {
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
				}
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://release-assets.githubusercontent.com/file.bin"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})}
			s, d, r := originalFlowInput()
			budget := &Budget{}
			result := e.Run(ctx, BaselineEntries()[0], s, d, r, domain.NetworkSnapshot{Fingerprint: "test"}, nil, budget)
			if downloads != 2 || calls < 2 {
				t.Fatalf("沒有真的走到第二輪資源更新失敗：downloads=%d calls=%d", downloads, calls)
			}
			if result.Mode != "connection" || result.SpeedHost != "" || result.Bytes == 0 || result.Bytes != budget.Used() {
				t.Fatalf("未捨棄下載評分或漏計流量：%+v", result)
			}
			for _, c := range result.Candidates {
				if c.MedianMbps != 0 || len(c.Samples) != 1 {
					t.Fatal("部分下載成績殘留")
				}
			}
			if cancelled {
				if result.Complete || result.BestIPv4 != "" || result.AutomaticEligible {
					t.Fatal("取消仍建議")
				}
			} else if !result.Complete || result.BestIPv4 != "151.101.1.133" {
				t.Fatalf("原版延遲比較失敗：%+v", result)
			}
		})
	}
}
