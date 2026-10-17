package probe

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
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

func TestTwitchOwnHost405AndActualAsset(t *testing.T) {
	var homepageHeads, downloads atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Host {
		case "www.twitch.tv":
			if r.Method == http.MethodHead {
				homepageHeads.Add(1)
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			_, _ = w.Write([]byte(`<script src="https://assets.twitch.tv/assets/nomodulePolyfills-test.js"></script>`))
		case "static-cdn.jtvnw.net":
			if r.URL.Path != "/jtv_user_pictures/xarth/404_user_70x70.png" {
				t.Error("未使用上游專用探測資源")
			}
			w.WriteHeader(http.StatusOK)
		case "assets.twitch.tv":
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if r.URL.Path != "/assets/nomodulePolyfills-test.js" {
				t.Error("沒有下載動態發現的資源")
			}
			downloads.Add(1)
			_, _ = w.Write([]byte(strings.Repeat("x", 100000)))
		default:
			t.Error("不允許的服務網域")
			w.WriteHeader(400)
		}
	}))
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"www.twitch.tv", "static-cdn.jtvnw.net", "assets.twitch.tv"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	defer server.Close()
	root, _ := x509.ParseCertificate(der)
	roots := x509.NewCertPool()
	roots.AddCert(root)
	e := Engine{HTTPS: HTTPS{roots: roots, dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}, TCP: func(_ context.Context, ip string) (float64, error) {
		if ip == "151.101.0.214" {
			return 1, nil
		}
		return 20, nil
	}, Lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("151.101.1.214")}}, nil
	}}
	s := domain.DefaultSettings()
	s.Probe.Candidates = 1
	now := time.Now().UTC()
	r := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/23"}, FetchedAt: now, ExpiresAt: now.Add(time.Hour)}
	r.Digest = domain.Digest(r.CIDRs)
	d := domain.DetectionResult{Classification: "confirmed", ExpiresAt: now.Add(time.Minute), Candidates: []string{"151.101.0.214"}}
	b := &Budget{Limit: 100000000}
	result := e.Run(context.Background(), domain.DomainEntry{ID: "twitch", Hostname: "assets.twitch.tv"}, s, d, r, domain.NetworkSnapshot{Fingerprint: "test"}, nil, b)
	if result.Mode != "download" || result.ProbeHost != "assets.twitch.tv" || result.SpeedHost != "assets.twitch.tv" || downloads.Load() != 6 {
		t.Fatalf("405 仍淘汰路線或未量服務資源: %+v", result)
	}
	for _, c := range result.Candidates {
		if c.Successes != 3 {
			t.Fatalf("有效候選未完成三輪: %+v", c)
		}
	}
	if b.Used() != result.Bytes || b.Used() <= 600000 || b.Used() > b.Limit {
		t.Fatal("探索流量未共用預算")
	}
	result = e.Run(context.Background(), domain.DomainEntry{ID: "www", Hostname: "www.twitch.tv"}, s, d, r, domain.NetworkSnapshot{Fingerprint: "test"}, nil, b)
	if !result.Complete || result.Mode != "connection" || result.ProbeHost != "www.twitch.tv" || result.SpeedHost != "" || homepageHeads.Load() == 0 || downloads.Load() != 6 || result.Bytes != 0 {
		t.Fatalf("首頁 405 未獨立驗證，或誤借下載成績：%+v", result)
	}
}
