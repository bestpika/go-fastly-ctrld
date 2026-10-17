package dns

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"golang.org/x/net/dns/dnsmessage"
)

func server(t *testing.T, address [4]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var q dnsmessage.Message
		if q.Unpack(body) != nil {
			t.Error("查詢格式錯誤")
		}
		m := dnsmessage.Message{Header: dnsmessage.Header{ID: q.ID, Response: true}, Questions: q.Questions}
		if q.Questions[0].Type == dnsmessage.TypeA {
			m.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: q.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: address}}}
		}
		wire, _ := m.Pack()
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(wire)
	}))
}

func TestParallelSourcesAllowDifferentFastlyAddresses(t *testing.T) {
	a, b := server(t, [4]byte{151, 101, 1, 214}), server(t, [4]byte{151, 101, 2, 214})
	defer a.Close()
	defer b.Close()
	startedA, startedB := make(chan struct{}), make(chan struct{})
	var onceA, onceB sync.Once
	handlerA, handlerB := a.Config.Handler, b.Config.Handler
	a.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		onceA.Do(func() { close(startedA) })
		select {
		case <-startedB:
			handlerA.ServeHTTP(w, r)
		case <-r.Context().Done():
		}
	})
	b.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		onceB.Do(func() { close(startedB) })
		select {
		case <-startedA:
			handlerB.ServeHTTP(w, r)
		case <-r.Context().Done():
		}
	})
	c := New()
	c.endpoints = []endpoint{{"Google", a.URL}, {"Cloudflare", b.URL}}
	now := time.Now().UTC()
	ranges := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/16"}, FetchedAt: now, ExpiresAt: now.Add(time.Hour)}
	ranges.Digest = domain.Digest(ranges.CIDRs)
	r := c.Detect(context.Background(), domain.DomainEntry{ID: "1", Hostname: "www.twitch.tv"}, ranges)
	if r.Classification != "confirmed" || len(r.Candidates) != 2 || len(r.Sources) != 2 {
		t.Fatalf("錯誤判定 %+v", r)
	}
	if r.ExpiresAt.After(now.Add(61 * time.Second)) {
		t.Fatal("未尊重 DNS TTL")
	}
}

func TestDisagreementAndFailureCannotConfirm(t *testing.T) {
	a, b := server(t, [4]byte{151, 101, 1, 214}), server(t, [4]byte{1, 1, 1, 1})
	defer a.Close()
	defer b.Close()
	c := New()
	c.endpoints = []endpoint{{"Google", a.URL}, {"Cloudflare", b.URL}}
	now := time.Now().UTC()
	ranges := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/16"}, FetchedAt: now, ExpiresAt: now.Add(time.Hour), Digest: domain.Digest([]string{"151.101.0.0/16"})}
	d := domain.DomainEntry{ID: "1", Hostname: "www.twitch.tv"}
	if r := c.Detect(context.Background(), d, ranges); r.Classification != "mixed" {
		t.Fatal(r.Classification)
	}
	b.Close()
	if r := c.Detect(context.Background(), d, ranges); r.Classification != "indeterminate" {
		t.Fatal(r.Classification)
	}
}
