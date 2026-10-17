package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/probe"
	"golang.org/x/net/dns/dnsmessage"
)

func TestSharedApplyRequiresCurrentPrivateReference(t *testing.T) {
	c, _ := newTestCore(t, successfulProbe)
	s, _ := c.manager.Snapshot()
	r, err := c.fastly.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n := c.net.Snapshot(context.Background())
	now := time.Now().UTC()
	ref := domain.SharedReference{ID: domain.ID(), StrategyVersion: domain.ProbeStrategyVersion, NetworkFingerprint: n.Fingerprint, RangesDigest: r.Digest, ProbeDigest: domain.Digest(s.Probe), ObservedAt: now, ExpiresAt: now.Add(time.Minute), Sources: []domain.ReferenceSource{}}
	for _, name := range []string{"Twitch", "GitHub"} {
		host, ip := "assets.twitch.tv", "151.101.0.214"
		if name == "GitHub" {
			host = "release-assets.githubusercontent.com"
			ip = "151.101.0.133"
		}
		ref.Sources = append(ref.Sources, domain.ReferenceSource{Name: name, SpeedHost: host, Candidates: []domain.CandidateResult{{IP: ip, Family: 4, Samples: []domain.Sample{{Success: true, ActualIP: ip, Hostname: host}}}}})
	}
	ref.Digest = probe.ReferenceDigest(ref)
	if err = c.cache.Write("references/"+probe.ReferenceKey(s, r, n)+".json", ref); err != nil {
		t.Fatal(err)
	}
	summary := ref
	summary.Sources = nil
	result := domain.TestResult{Mode: "shared-reference", Reference: &summary}
	if err = c.validateReference(result, s, r, n); err != nil {
		t.Fatal(err)
	}
	summary.Digest = "changed"
	if c.validateReference(result, s, r, n) == nil {
		t.Fatal("共享摘要變更仍可套用")
	}
	summary.Digest = ref.Digest
	changed := n
	changed.Fingerprint = "different"
	if c.validateReference(result, s, r, changed) == nil {
		t.Fatal("不同網路仍可沿用參考")
	}
	ref.ExpiresAt = now.Add(-time.Second)
	ref.Digest = probe.ReferenceDigest(ref)
	if err = c.cache.Write("references/"+probe.ReferenceKey(s, r, n)+".json", ref); err != nil {
		t.Fatal(err)
	}
	summary.Digest = ref.Digest
	if err := c.validateReference(result, s, r, n); err != nil {
		t.Fatal("共享參考不應因時間被拒絕", err)
	}
	result.Mode = "connection"
	if err = c.validateReference(result, s, r, n); err != nil {
		t.Fatal("純自身延遲不應被說明用參考阻擋")
	}
}

func TestAutomaticReferencePriorityRotatesEvenAfterWholeBatch(t *testing.T) {
	for _, item := range []struct{ cursor, completed, count, want int }{{0, 10, 10, 1}, {2, 10, 10, 3}, {0, 3, 10, 3}, {0, 0, 10, 1}, {0, 1, 1, 0}} {
		if got := nextAutomaticCursor(item.cursor, item.completed, item.count); got != item.want {
			t.Fatalf("參考基準優先順序未輪替: %d", got)
		}
	}
}

func TestMissingSharedGrantIsNotOriginalMixedDNSConsent(t *testing.T) {
	c, api := newTestCore(t, successfulProbe)
	id := importTestDomain(t, c)
	if !c.SetControlDToken("test-secret").OK {
		t.Fatal("假權杖設定失敗")
	}
	s, _ := c.manager.Snapshot()
	if !c.SelectProfile(s.Revision, "profile").OK {
		t.Fatal("假 Profile 選擇失敗")
	}
	job := c.StartTest([]string{id}, 1_000_000)
	if !job.OK {
		t.Fatal(job.Error)
	}
	waitJob(t, c, job.Data)
	c.gate.Lock()
	result := c.results[id]
	result.DNSClassification = "confirmed"
	result.AutomaticEligible = false
	c.results[id] = result
	c.gate.Unlock()
	p := c.BuildSyncPreview([]string{id})
	if !p.OK || !p.Data.Items[0].Applicable {
		t.Fatal("預覽建立失敗")
	}
	c.dns.HTTP = &http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		var query dnsmessage.Message
		if err := query.Unpack(body); err != nil {
			return nil, err
		}
		reply := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true}, Questions: query.Questions}
		q := query.Questions[0]
		if q.Type == dnsmessage.TypeA {
			ip := [4]byte{151, 101, 0, 214}
			if request.URL.Hostname() == "cloudflare-dns.com" {
				ip = [4]byte{1, 1, 1, 1}
			}
			reply.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: q.Name, Type: q.Type, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: ip}}}
		}
		packed, _ := reply.Pack()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/dns-message"}}, Body: io.NopCloser(bytes.NewReader(packed))}, nil
	})}
	if c.validateItem(context.Background(), p.Data, p.Data.Items[0]) == nil {
		t.Fatal("未授權共享被誤當成原本同意混合 DNS")
	}
	c.gate.Lock()
	result.DNSClassification = "mixed"
	c.results[id] = result
	c.gate.Unlock()
	if err := c.validateItem(context.Background(), p.Data, p.Data.Items[0]); err != nil {
		t.Fatal("原本混合 DNS 的手動例外不應失效", err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.writes != 0 {
		t.Fatal("驗證不應寫入帳號")
	}
}
