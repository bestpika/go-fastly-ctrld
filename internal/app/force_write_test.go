package app

import (
	"bytes"
	"context"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/controld"
	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/probe"
	"github.com/bestpika/go-fastly-ctrld/internal/settings"
	"golang.org/x/net/dns/dnsmessage"
)

func forceTestCore(t *testing.T, cname bool) (*Core, *testAPI, domain.DomainEntry) {
	t.Helper()
	c, api := newTestCore(t, successfulProbe)
	p := c.PreviewDomainImport("example.com", "paste", "")
	if !p.OK || !c.ConfirmDomainImport(p.Data.ID).OK || !c.SetControlDToken("local-test-token").OK {
		t.Fatal("測試設定失敗")
	}
	s, _ := c.manager.Snapshot()
	if err := c.manager.SelectProfile(s.Revision, "profile"); err != nil {
		t.Fatal(err)
	}
	s, _ = c.manager.Snapshot()
	on := true
	s.Probe.Services = []string{}
	if err := c.manager.Update(s.Revision, settings.Patch{Theme: s.Theme, Probe: s.Probe, ForceWrite: &on}); err != nil {
		t.Fatal(err)
	}
	_, entries := c.manager.Snapshot()
	entry := entries[0]
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != entry.Hostname || r.TLS.ServerName != entry.Hostname || r.Method != http.MethodHead || r.URL.Path != "/" {
			t.Error("沒有使用原網域 Host/SNI 的資格驗證")
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	c.probeEngine = probe.New()
	c.probeEngine.HTTPS = probe.NewHTTPS(func(ctx context.Context, _, address string) (net.Conn, error) {
		if address != "151.101.0.214:443" {
			t.Error("原始 DNS 位址遭替換或做服務尾碼轉換")
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}, roots)
	if cname {
		c.dns.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(r.Body)
			var query dnsmessage.Message
			if err := query.Unpack(body); err != nil {
				return nil, err
			}
			q := query.Questions[0]
			reply := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true}, Questions: query.Questions}
			target := dnsmessage.MustNewName("edge.fastly.net.")
			if q.Name.String() == entry.Hostname+"." {
				reply.Answers = append(reply.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.CNAMEResource{CNAME: target}})
			}
			if q.Type == dnsmessage.TypeA {
				reply.Answers = append(reply.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: target, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{151, 101, 0, 214}}})
			}
			packed, err := reply.Pack()
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/dns-message"}}, Body: io.NopCloser(bytes.NewReader(packed))}, err
		})}
	}
	return c, api, entry
}

func TestProxyWithoutIPv6CanBeBackedUpAppliedAndRestored(t *testing.T) {
	c, api, entry := forceTestCore(t, false)
	old, err := controld.ParseRule([]byte(`{"PK":"example.com","group":0,"comment":"原始代理","action":{"do":3,"status":1,"via":"HND"}}`))
	if err != nil || !old.Safe {
		t.Fatal("省略不適用 IPv6 欄位的代理規則仍被拒絕")
	}
	api.rules[entry.Hostname] = old
	seedNoImprovement(c, entry)
	p := c.BuildSyncPreview([]string{entry.ID})
	if !p.OK || !p.Data.Items[0].Applicable || p.Data.Items[0].Before.Do != 3 {
		t.Fatalf("代理預覽失敗：%+v", p)
	}
	start := c.ApplySyncPreview(p.Data.ID)
	if !start.OK {
		t.Fatal(start.Error)
	}
	job := waitJob(t, c, start.Data)
	if job.State != "completed" || api.rules[entry.Hostname].Do != 2 {
		t.Fatal("套用未完成")
	}
	undo := c.BuildRestorePreview(job.ID)
	if !undo.OK || !undo.Data.Items[0].Applicable {
		t.Fatalf("代理復原預覽失敗：%+v", undo)
	}
	start = c.ApplyRestorePreview(undo.Data.ID)
	if !start.OK || waitJob(t, c, start.Data).State != "completed" {
		t.Fatal("代理復原失敗")
	}
	actual := api.rules[entry.Hostname]
	if actual.Do != 3 || actual.Via != "HND" || actual.Comment != old.Comment || actual.SemanticDigest() != old.SemanticDigest() || api.writes != 2 {
		t.Fatal("代理動作／位置／註解未完整還原")
	}
}

func TestIncompleteForcePreviewDoesNotSuggestManualBypass(t *testing.T) {
	for _, mode := range []string{"connection", "shared-reference"} {
		t.Run(mode, func(t *testing.T) {
			c, api, entry := forceTestCore(t, false)
			seedNoImprovement(c, entry)
			c.gate.Lock()
			r := c.results[entry.ID]
			r.Complete, r.AutomaticEligible, r.Mode = false, false, mode
			r.Reason = "DNS 重新查詢失敗"
			c.results[entry.ID] = r
			c.gate.Unlock()
			p := c.BuildSyncPreview([]string{entry.ID})
			if !p.OK || p.Data.Items[0].Applicable || !strings.Contains(p.Data.Items[0].Reason, "測試未完成") || strings.Contains(p.Data.Items[0].Reason, "僅可手動") || c.results[entry.ID].Reason != r.Reason || api.writes != 0 {
				t.Fatalf("未完成原因被覆蓋或誤稱可手動繞過：%+v", p)
			}
		})
	}
}

func seedNoImprovement(c *Core, entry domain.DomainEntry) domain.Settings {
	s, _ := c.manager.Snapshot()
	r := successfulProbe(context.Background(), entry, s, domain.DetectionResult{Classification: "confirmed"}, domain.RangeSnapshot{}, domain.NetworkSnapshot{Fingerprint: "network"}, nil, nil, nil)
	r.BestIPv4 = ""
	c.gate.Lock()
	c.results[entry.ID] = r
	c.gate.Unlock()
	return s
}

func TestForceWriteManualOriginalDNSAndSameValueSubmission(t *testing.T) {
	for _, cname := range []bool{false, true} {
		t.Run(map[bool]string{false: "原始 IPv4", true: "原始 CNAME"}[cname], func(t *testing.T) {
			c, api, entry := forceTestCore(t, cname)
			seedNoImprovement(c, entry)
			for round := 0; round < 2; round++ {
				p := c.BuildSyncPreview([]string{entry.ID})
				if !p.OK || !p.Data.Items[0].Applicable || p.Data.Items[0].OriginalDNS == nil {
					t.Fatalf("原始 DNS 預覽失敗：%+v", p)
				}
				if round == 1 && p.Data.Items[0].Operation != "rewrite" {
					t.Fatal("相同值未標明重寫")
				}
				apply := c.ApplySyncPreview(p.Data.ID)
				if !apply.OK {
					t.Fatal(apply.Error)
				}
				job := waitJob(t, c, apply.Data)
				if job.State != "completed" || !job.Items[0].WriteAccepted {
					t.Fatalf("強制寫入未確認：%+v", job)
				}
			}
			api.mu.Lock()
			writes, actual := api.writes, api.rules[entry.Hostname]
			api.mu.Unlock()
			want := "151.101.0.214"
			if cname {
				want = "edge.fastly.net"
			}
			if writes != 2 || actual.Via != want || c.GetAppSnapshot().Data.Settings.Automation.Enabled {
				t.Fatal("無改善或同值未實際寫入，或手動偷偷開啟自動")
			}
		})
	}
}

func TestForceOriginalDNSChangeAndTLSFailurePreventWrite(t *testing.T) {
	for _, cause := range []string{"TLS 失敗", "DNS 改變", "CNAME 後續鏈改變", "CNAME 被其他規則涵蓋", "DNS 證據過期"} {
		t.Run(cause, func(t *testing.T) {
			c, api, entry := forceTestCore(t, cause == "CNAME 被其他規則涵蓋" || cause == "CNAME 後續鏈改變")
			seedNoImprovement(c, entry)
			p := c.BuildSyncPreview([]string{entry.ID})
			if !p.OK || !p.Data.Items[0].Applicable {
				t.Fatal("預覽失敗")
			}
			switch cause {
			case "TLS 失敗":
				// 僅注入失敗連線，不碰真實網路。
				c.probeEngine.HTTPS = probe.NewHTTPS(func(context.Context, string, string) (net.Conn, error) { return nil, io.ErrUnexpectedEOF }, nil)
			case "DNS 改變", "CNAME 後續鏈改變":
				previous := c.dns.HTTP.Transport
				c.dns.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
					response, err := previous.RoundTrip(r)
					if err != nil {
						return response, err
					}
					body, _ := io.ReadAll(response.Body)
					response.Body.Close()
					var message dnsmessage.Message
					if err := message.Unpack(body); err != nil {
						return nil, err
					}
					for index := range message.Answers {
						answer := &message.Answers[index]
						if a, ok := answer.Body.(*dnsmessage.AResource); ok {
							if cause == "DNS 改變" {
								a.A = [4]byte{151, 101, 1, 214}
							} else {
								answer.Header.Name = dnsmessage.MustNewName("other.fastly.net.")
							}
						}
					}
					if cause == "CNAME 後續鏈改變" {
						message.Answers = append(message.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("edge.fastly.net."), Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName("other.fastly.net.")}})
					}
					packed, err := message.Pack()
					response.Body = io.NopCloser(bytes.NewReader(packed))
					return response, err
				})}
			case "CNAME 被其他規則涵蓋":
				api.mu.Lock()
				api.rules["fastly.net"] = domain.Rule{Hostname: "fastly.net", Status: 1, Safe: true}
				api.mu.Unlock()
			case "DNS 證據過期":
				c.gate.Lock()
				stored := c.previews[p.Data.ID]
				stored.Items[0].OriginalDNS.ExpiresAt = time.Now().Add(-time.Second)
				c.previews[p.Data.ID] = stored
				c.gate.Unlock()
			}
			apply := c.ApplySyncPreview(p.Data.ID)
			if !apply.OK {
				t.Fatal(apply.Error)
			}
			job := waitJob(t, c, apply.Data)
			if cause == "DNS 證據過期" {
				api.mu.Lock()
				defer api.mu.Unlock()
				if job.State != "completed" || api.writes != 1 {
					t.Fatal("原始 DNS 不應僅因時間而阻擋寫入")
				}
				return
			}
			if job.State != "failed" {
				t.Fatal("套用前驗證失敗仍寫入")
			}
			if cause == "CNAME 後續鏈改變" && !strings.Contains(job.Items[0].Message, "原始 DNS 目標已變更") {
				t.Fatalf("未偵測相同第一跳之後的鏈結變動：%+v", job)
			}
			api.mu.Lock()
			defer api.mu.Unlock()
			if api.writes != 0 {
				t.Fatal("驗證失敗仍送出")
			}
		})
	}
}

func TestForceAutomaticApplySkipsCooldownButTestOnlyNeverWrites(t *testing.T) {
	for _, testOnly := range []bool{true, false} {
		t.Run(map[bool]string{true: "只測試", false: "自動套用"}[testOnly], func(t *testing.T) {
			c, api, entry := forceTestCore(t, false)
			s, _ := c.manager.Snapshot()
			a := s.Automation
			a.Enabled, a.TestOnly, a.ProfileID, a.CredentialRevision = true, testOnly, s.ProfileID, s.CredentialRevision
			a.Grants = []domain.Grant{{DomainID: entry.ID, Overwrite: true, Move: true}}
			a.LastChanged[entry.ID] = time.Now()
			if err := c.manager.Configure(s.Revision, a); err != nil {
				t.Fatal(err)
			}
			s = seedNoImprovement(c, entry)
			c.autoApply([]domain.DomainEntry{entry}, s)
			if testOnly {
				if api.writes != 0 {
					t.Fatal("只測試仍寫入")
				}
				return
			}
			c.gate.Lock()
			id := c.writerID
			for jobID, j := range c.jobs {
				if j.Kind == "apply" {
					id = jobID
				}
			}
			c.gate.Unlock()
			if id == "" || waitJob(t, c, id).State != "completed" {
				t.Fatal("強制自動套用未略過冷卻")
			}
			api.mu.Lock()
			defer api.mu.Unlock()
			if api.writes != 1 {
				t.Fatal("自動套用沒有實際寫入")
			}
		})
	}
}
