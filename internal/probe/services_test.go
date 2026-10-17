package probe

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

func TestTwelveDomainsProbeOwnHostAndOnlyThreeDownload(t *testing.T) {
	entries := BaselineEntries()
	hosts := []string{}
	for _, entry := range entries {
		hosts = append(hosts, entry.Hostname)
	}
	var mu sync.Mutex
	heads, gets := map[string]int{}, map[string]int{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS.ServerName != r.Host {
			t.Error("Host 與 TLS SNI 不一致")
		}
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodHead {
			heads[r.Host]++
			if r.Host == "k.twitchcdn.net" {
				w.Header().Set("X-Served-By", "cache-test-HKG")
			} else if r.Host == "pbs.twimg.com" {
				w.Header().Set("X-Served-By", "t4_p")
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		gets[r.Host]++
		switch r.Host {
		case "www.twitch.tv":
			_, _ = io.WriteString(w, `<script src="https://assets.twitch.tv/assets/nomodulePolyfills-test.js"></script>`)
		case "release-assets.githubusercontent.com", "assets.twitch.tv", "pbs.twimg.com":
			if r.Host == "pbs.twimg.com" && r.URL.RequestURI() != xImagePath {
				t.Error("X 圖片路徑或查詢參數不符")
			}
			_, _ = io.WriteString(w, strings.Repeat("x", 698569))
		default:
			t.Errorf("非下載網域發出 GET：%s", r.Host)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: hosts, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	defer server.Close()
	root, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	e := New()
	e.HTTPS = HTTPS{roots: roots, dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	e.TCP = func(context.Context, string) (float64, error) { return 10, nil }
	e.Lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("151.101.0.49")}}, nil
	}
	e.DiscoveryHTTP = &http.Client{Transport: resourceTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "api.github.com" {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"assets":[{"size":100000,"browser_download_url":"https://github.com/cli/cli/releases/download/v1/file.bin"}]}`))}, nil
		}
		if req.URL.Hostname() == "release-assets.githubusercontent.com" {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://release-assets.githubusercontent.com/file.bin"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	s, d, ranges := originalFlowInput()
	budget := &Budget{}
	downloads := map[string]bool{"release-assets.githubusercontent.com": true, "assets.twitch.tv": true, "pbs.twimg.com": true}
	results := []domain.TestResult{}
	var total int64
	for _, entry := range entries {
		result := e.Run(context.Background(), entry, s, d, ranges, domain.NetworkSnapshot{Fingerprint: "test"}, nil, budget)
		results = append(results, result)
		total += result.Bytes
		wantMode := "connection"
		if downloads[entry.Hostname] {
			wantMode = "download"
		}
		if !result.Complete || result.ProbeHost != entry.Hostname || result.Mode != wantMode || heads[entry.Hostname] == 0 {
			t.Fatalf("未獨立探測：%+v", result)
		}
		baseline := false
		for _, candidate := range result.Candidates {
			if candidate.Baseline && candidate.IP == "151.101.0.49" {
				baseline = true
			}
			wantPOP := "?"
			if entry.Hostname == "k.twitchcdn.net" {
				wantPOP = "HKG"
			}
			if candidate.POP != wantPOP || len(candidate.ReferenceScores) > 0 {
				t.Fatal("借用其他網域機房或下載分數")
			}
			for _, sample := range candidate.Samples {
				if sample.ActualIP != candidate.IP || sample.Hostname != entry.Hostname || !sample.Success {
					t.Fatalf("現用 IP、Host 或短檔成績失真：%+v", sample)
				}
			}
			if downloads[entry.Hostname] {
				if len(candidate.Samples) != 3 || candidate.Samples[0].Bytes != 698569 || candidate.MedianMbps <= 0 {
					t.Fatal("下載沒有保留三輪真實短檔成績")
				}
			} else if candidate.MedianMbps != 0 || result.SpeedHost != "" || result.Bytes != 0 {
				t.Fatal("連線探測冒充下載成績")
			}
		}
		if !baseline {
			t.Fatal("現用路線遭改為服務尾碼")
		}
	}
	if total != budget.Used() || len(heads) != 12 || len(gets) != 4 {
		t.Fatal("網域探測遺漏或流量未完整記帳")
	}
	ref, err := ReferenceFromBaselines(append([]domain.TestResult{results[3], results[4]}, results...), s, ranges, domain.NetworkSnapshot{Fingerprint: "test"})
	if err != nil || len(ref.Sources) != 2 || ref.Sources[0].ProbeHost != "assets.twitch.tv" || ref.Sources[1].ProbeHost != "release-assets.githubusercontent.com" {
		t.Fatalf("連線結果干擾共享下載來源：%+v %v", ref, err)
	}
	// X 不得偷偷探索 GitHub，且額度不足仍不能當成完整三輪。
	e.DiscoveryHTTP = &http.Client{Transport: resourceTransport(func(*http.Request) (*http.Response, error) {
		t.Error("X 仍依賴 GitHub")
		return nil, io.ErrUnexpectedEOF
	})}
	result := e.Run(context.Background(), entries[2], s, d, ranges, domain.NetworkSnapshot{Fingerprint: "test"}, nil, &Budget{Limit: 1})
	if result.Complete || result.BestIPv4 != "" || result.AutomaticEligible {
		t.Fatal("X 預算不足仍建議套用")
	}
}

func TestCustomURLOverridesEveryBuiltinProfile(t *testing.T) {
	for _, entry := range BaselineEntries() {
		entry.TestURL = "https://" + entry.Hostname + "/custom?source=user"
		p := service(entry)
		resource, err := (&Engine{}).resource(context.Background(), p, entry, nil, domain.RangeSnapshot{}, domain.ProbeSettings{}, &Budget{})
		if p.host != entry.Hostname || p.speed != "url" || p.suffix != -1 || err != nil || resource != entry.TestURL {
			t.Fatalf("未優先同域自訂 URL：%s", entry.Hostname)
		}
	}
}
