package app

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/probe"
	"github.com/bestpika/go-fastly-ctrld/internal/settings"
)

func localVerifier(t *testing.T, checked func(*http.Request), dialed ...func(string)) *probe.Engine {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"www.twitch.tv", "example.com"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead || r.Host != r.TLS.ServerName {
			t.Error("未以目標自身 Host/SNI 重驗")
		}
		if checked != nil {
			checked(r)
		}
		w.WriteHeader(405)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	root, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	engine := probe.New()
	engine.HTTPS = probe.NewHTTPS(func(ctx context.Context, _, address string) (net.Conn, error) {
		for _, observe := range dialed {
			observe(address)
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}, roots)
	return engine
}

func TestEveryResultModeRevalidatesExactTargetsBeforeWriting(t *testing.T) {
	for _, mode := range []string{"download", "connection"} {
		for _, failure := range []string{"正常", "TLS 失敗", "驗完後網路改變", "驗完後結果過期"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				c, api, entry := forceTestCore(t, false)
				seedNoImprovement(c, entry)
				network := &validationNetwork{}
				c.net = network
				c.gate.Lock()
				result := c.results[entry.ID]
				result.BestIPv4, result.Mode = "151.101.0.214", mode
				c.results[entry.ID] = result
				c.gate.Unlock()
				p := c.BuildSyncPreview([]string{entry.ID})
				if !p.OK || !p.Data.Items[0].Applicable || p.Data.Items[0].OriginalDNS != nil {
					t.Fatalf("測試預覽不符：%+v", p)
				}
				var calls atomic.Int32
				c.probeEngine = localVerifier(t, func(*http.Request) {
					calls.Add(1)
					if failure == "驗完後網路改變" {
						network.changed.Store(true)
					}
				})
				if failure == "TLS 失敗" {
					c.probeEngine.HTTPS = probe.NewHTTPS(func(context.Context, string, string) (net.Conn, error) { return nil, io.ErrUnexpectedEOF }, nil)
				}
				if failure == "驗完後結果過期" {
					// 在回應 HEAD 後讓已捕捉結果的有效期限確實到達。
					deadline := time.Now().Add(2 * time.Second)
					c.probeEngine = localVerifier(t, func(*http.Request) { calls.Add(1); time.Sleep(max(0, time.Until(deadline)) + 10*time.Millisecond) })
					c.gate.Lock()
					result.ExpiresAt = deadline
					c.results[entry.ID] = result
					c.gate.Unlock()
				}
				apply := c.ApplySyncPreview(p.Data.ID)
				if !apply.OK {
					t.Fatal(apply.Error)
				}
				job := waitJob(t, c, apply.Data)
				api.mu.Lock()
				writes := api.writes
				api.mu.Unlock()
				if failure == "正常" || failure == "驗完後結果過期" {
					if writes != 1 || calls.Load() != 1 || job.State != "completed" {
						t.Fatalf("未重驗或未寫入：%+v calls=%d", job, calls.Load())
					}
				} else if writes != 0 || job.State == "completed" {
					t.Fatal("最後驗證失敗仍寫入")
				}
				if failure != "TLS 失敗" && calls.Load() != 1 {
					t.Fatal("未真正執行最後 TLS 驗證")
				}
			})
		}
	}
}

func TestFinalValidationChecksBothExactAddressFamilies(t *testing.T) {
	c, api, entry := forceTestCore(t, false)
	s, _ := c.manager.Snapshot()
	s.Probe.EnableIPv6 = true
	if err := c.manager.Update(s.Revision, settings.Patch{Theme: s.Theme, Probe: s.Probe}); err != nil {
		t.Fatal(err)
	}
	c.net = &validationNetwork{ipv6: true}
	seedNoImprovement(c, entry)
	c.gate.Lock()
	result := c.results[entry.ID]
	result.BestIPv4, result.BestIPv6, result.Mode = "151.101.0.214", "2a04:4e42::49", "connection"
	result.Candidates = append(result.Candidates, domain.CandidateResult{IP: result.BestIPv6, Family: 6, Successes: 1, Samples: []domain.Sample{{Success: true}}})
	c.results[entry.ID] = result
	c.gate.Unlock()
	p := c.BuildSyncPreview([]string{entry.ID})
	if !p.OK || !p.Data.Items[0].Applicable || p.Data.Items[0].After.ViaV6 != result.BestIPv6 {
		t.Fatalf("雙協定位址預覽失敗：%+v", p)
	}
	addresses := make(chan string, 2)
	c.probeEngine = localVerifier(t, nil, func(address string) { addresses <- address })
	apply := c.ApplySyncPreview(p.Data.ID)
	if !apply.OK {
		t.Fatal(apply.Error)
	}
	job := waitJob(t, c, apply.Data)
	if job.State != "completed" || len(addresses) != 2 || <-addresses != "151.101.0.214:443" || <-addresses != "[2a04:4e42::49]:443" {
		t.Fatalf("未逐一驗證精確 IPv4／IPv6：%+v", job)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.writes != 1 || api.rules[entry.Hostname].ViaV6 != result.BestIPv6 {
		t.Fatal("雙協定驗證後未保留選定 IPv6")
	}
}

type validationNetwork struct {
	changed atomic.Bool
	ipv6    bool
}

func (n *validationNetwork) Snapshot(context.Context) domain.NetworkSnapshot {
	if n.changed.Load() {
		return domain.NetworkSnapshot{Fingerprint: "changed"}
	}
	snapshot := testNetwork{}.Snapshot(context.Background())
	snapshot.IPv6 = n.ipv6
	return snapshot
}
