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
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

func TestTwoTargetsReuseOneInterleavedReferenceWithoutTargetDownloads(t *testing.T) {
	var downloads atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Host {
		case "www.twitch.tv":
			if r.Method == http.MethodHead {
				w.WriteHeader(405)
				return
			}
			_, _ = io.WriteString(w, `<script src="https://assets.twitch.tv/assets/nomodulePolyfills-reference.js"></script>`)
		case "assets.twitch.tv", "release-assets.githubusercontent.com":
			if r.Method == http.MethodGet {
				downloads.Add(1)
				_, _ = io.WriteString(w, strings.Repeat("x", 100000))
			}
		case "static-cdn.jtvnw.net", "k.twitchcdn.net", "raw.githubusercontent.com", "one.example.net", "two.example.net":
			w.WriteHeader(200)
		default:
			t.Error("未允許的來源")
			w.WriteHeader(400)
		}
	}))
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"www.twitch.tv", "static-cdn.jtvnw.net", "k.twitchcdn.net", "assets.twitch.tv", "raw.githubusercontent.com", "release-assets.githubusercontent.com", "one.example.net", "two.example.net"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, _ := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	defer server.Close()
	root, _ := x509.ParseCertificate(der)
	roots := x509.NewCertPool()
	roots.AddCert(root)
	e := Engine{HTTPS: HTTPS{roots: roots, dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}, TCP: func(context.Context, string) (float64, error) { return 10, nil }, Lookup: func(_ context.Context, hostname string) ([]net.IPAddr, error) {
		tail := "49"
		if hostname == "two.example.net" {
			tail = "50"
		}
		return []net.IPAddr{{IP: net.ParseIP("151.101.0." + tail)}}, nil
	}, DiscoveryHTTP: &http.Client{Transport: resourceTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		status := 200
		headers := http.Header{}
		if r.URL.Host == "api.github.com" {
			body = `{"assets":[{"size":100000,"browser_download_url":"https://github.com/cli/cli/releases/download/v1/file.bin"}]}`
		} else if r.URL.Host == "github.com" {
			status = 302
			headers.Set("Location", "https://release-assets.githubusercontent.com/file.bin?fresh=1")
		}
		return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	s := domain.DefaultSettings()
	s.Probe.NoResourceMode = "shared"
	s.Probe.Candidates = 2
	s.Automation.AllowSharedReference = true
	now := time.Now().UTC()
	r := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/23"}, FetchedAt: now, ExpiresAt: now.Add(time.Hour)}
	r.Digest = domain.Digest(r.CIDRs)
	n := domain.NetworkSnapshot{Fingerprint: "test"}
	d := domain.DetectionResult{Classification: "confirmed", ExpiresAt: now.Add(10 * time.Minute), Candidates: []string{"151.101.0.214", "151.101.1.214"}}
	gh := d
	gh.Candidates = []string{"151.101.0.133", "151.101.1.133"}
	budget := &Budget{Limit: 60_000_000}
	ref, err := e.BuildReference(context.Background(), s, r, n, []domain.DetectionResult{d, gh}, []string{"151.101.0.49"}, budget)
	if err != nil || downloads.Load() != 12 || !ValidReference(ref, s, r, n, time.Now()) {
		t.Fatalf("來源沒有完整交錯三輪: %v downloads=%d", err, downloads.Load())
	}
	used := budget.Used()
	if ref.Bytes != used {
		t.Fatal("來源成本沒有只計一次")
	}
	for index, host := range []string{"one.example.net", "two.example.net"} {
		target := d
		tail := "49"
		if index == 1 {
			tail = "50"
		}
		target.Candidates = []string{"151.101.0." + tail}
		target.Sources = []domain.DNSObservation{{Records: []domain.DNSRecord{{Type: "A", Value: "151.101.0." + tail}}}}
		result := e.RunWithReference(context.Background(), domain.DomainEntry{ID: host, Hostname: host}, s, target, r, n, nil, budget, &ref, index == 1)
		if result.Reference == nil || result.Reference.ID != ref.ID || result.ReferenceReused != (index == 1) {
			t.Fatal("參考沒有批次重用")
		}
		for _, source := range result.Reference.Sources {
			if len(source.Candidates) != 0 {
				t.Fatal("每目標重複保存整份來源樣本")
			}
		}
		for _, c := range result.Candidates {
			if c.MedianMbps != 0 {
				t.Fatal("把來源下載當成目標速度")
			}
		}
	}
	if downloads.Load() != 12 || budget.Used() != used {
		t.Fatal("每網域重複下載或重算成本")
	}
	if e.VerifyTarget(context.Background(), domain.DomainEntry{Hostname: "not-in-certificate.example.net"}, "151.101.1.49", s, r) == nil {
		t.Fatal("未驗證目標 TLS")
	}
}
