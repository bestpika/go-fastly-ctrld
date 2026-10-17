package dns

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/fastly"
	"golang.org/x/net/dns/dnsmessage"
)

const Google = "https://dns.google/dns-query"
const Cloudflare = "https://cloudflare-dns.com/dns-query"

type endpoint struct{ name, url string }
type Client struct {
	HTTP      *http.Client
	Now       func() time.Time
	endpoints []endpoint
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Now: time.Now, endpoints: []endpoint{{"Google", Google}, {"Cloudflare", Cloudflare}}}
}

func (c *Client) query(ctx context.Context, e endpoint, hostname string, kind dnsmessage.Type) ([]domain.DNSRecord, error) {
	name := hostname
	records := []domain.DNSRecord{}
	visited := map[string]bool{}
	links := 0
	for hop := 0; hop <= 8; hop++ {
		if visited[name] {
			return records, fmt.Errorf("CNAME 循環")
		}
		visited[name] = true
		qname, err := dnsmessage.NewName(name + ".")
		if err != nil {
			return records, err
		}
		var nonce [2]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return records, err
		}
		id := binary.BigEndian.Uint16(nonce[:])
		q := dnsmessage.Question{Name: qname, Type: kind, Class: dnsmessage.ClassINET}
		wire, err := (&dnsmessage.Message{Header: dnsmessage.Header{ID: id, RecursionDesired: true}, Questions: []dnsmessage.Question{q}}).Pack()
		if err != nil {
			return records, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, bytes.NewReader(wire))
		if err != nil {
			return records, err
		}
		req.Header.Set("Content-Type", "application/dns-message")
		req.Header.Set("Accept", "application/dns-message")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return records, fmt.Errorf("DoH 連線失敗")
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 65536))
		resp.Body.Close()
		contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if readErr != nil || resp.StatusCode != 200 || contentType != "application/dns-message" || len(body) > 65535 {
			return records, fmt.Errorf("DoH 回應無效")
		}
		var message dnsmessage.Message
		if message.Unpack(body) != nil || !message.Response || message.Truncated || message.ID != id || message.RCode != dnsmessage.RCodeSuccess || len(message.Questions) != 1 || message.Questions[0].Type != kind || message.Questions[0].Class != dnsmessage.ClassINET || !strings.EqualFold(message.Questions[0].Name.String(), qname.String()) {
			return records, fmt.Errorf("DNS 回應無效或無法解析")
		}
		cnames := map[string]dnsmessage.Resource{}
		for _, r := range message.Answers {
			if _, ok := r.Body.(*dnsmessage.CNAMEResource); ok && r.Header.Class == dnsmessage.ClassINET {
				cnames[strings.ToLower(strings.TrimSuffix(r.Header.Name.String(), "."))] = r
			}
		}
		current := name
		followed := false
		for n := 0; n < 8; n++ {
			r, exists := cnames[current]
			if !exists {
				break
			}
			links++
			if links > 8 {
				return records, fmt.Errorf("CNAME 鏈結超過上限")
			}
			target, err := domain.Normalize(r.Body.(*dnsmessage.CNAMEResource).CNAME.String())
			if err != nil {
				return records, fmt.Errorf("CNAME 目標不安全")
			}
			records = append(records, domain.DNSRecord{Name: current, Type: "CNAME", Value: target, TTL: r.Header.TTL})
			if visited[target] {
				return records, fmt.Errorf("CNAME 循環")
			}
			visited[target] = true
			current = target
			followed = true
		}
		found := false
		for _, r := range message.Answers {
			if r.Header.Class != dnsmessage.ClassINET || !strings.EqualFold(strings.TrimSuffix(r.Header.Name.String(), "."), current) {
				continue
			}
			switch v := r.Body.(type) {
			case *dnsmessage.AResource:
				if kind == dnsmessage.TypeA {
					records = append(records, domain.DNSRecord{Name: current, Type: "A", Value: netip.AddrFrom4(v.A).String(), TTL: r.Header.TTL})
					found = true
				}
			case *dnsmessage.AAAAResource:
				if kind == dnsmessage.TypeAAAA {
					records = append(records, domain.DNSRecord{Name: current, Type: "AAAA", Value: netip.AddrFrom16(v.AAAA).String(), TTL: r.Header.TTL})
					found = true
				}
			}
		}
		if found || !followed {
			return records, nil
		}
		// 本次封包已驗證此鏈結；下一個 HTTP 查詢仍要保留循環防護。
		delete(visited, current)
		name = current
	}
	return records, fmt.Errorf("CNAME 鏈結超過上限")
}

func (c *Client) observe(ctx context.Context, e endpoint, hostname string, ranges domain.RangeSnapshot) domain.DNSObservation {
	o := domain.DNSObservation{Source: e.name, ObservedAt: c.Now().UTC(), Records: []domain.DNSRecord{}}
	type result struct {
		records []domain.DNSRecord
		err     error
	}
	results := make(chan result, 3)
	for _, kind := range []dnsmessage.Type{dnsmessage.TypeCNAME, dnsmessage.TypeA, dnsmessage.TypeAAAA} {
		go func() { r, err := c.query(ctx, e, hostname, kind); results <- result{r, err} }()
	}
	seen := map[string]int{}
	for range 3 {
		r := <-results
		if r.err != nil {
			o.Error = "部分 DNS 查詢失敗，證據不足"
		}
		for _, record := range r.records {
			if _, err := netip.ParseAddr(record.Value); err == nil {
				record.CIDR = fastly.Match(record.Value, ranges)
			}
			key := record.Name + "|" + record.Type + "|" + record.Value
			if index, ok := seen[key]; ok {
				if record.TTL < o.Records[index].TTL {
					o.Records[index].TTL = record.TTL
				}
				continue
			}
			seen[key] = len(o.Records)
			o.Records = append(o.Records, record)
		}
	}
	sort.Slice(o.Records, func(i, j int) bool {
		return o.Records[i].Name+o.Records[i].Type+o.Records[i].Value < o.Records[j].Name+o.Records[j].Type+o.Records[j].Value
	})
	return o
}

func classify(o domain.DNSObservation) string {
	if o.Error != "" {
		return "indeterminate"
	}
	fast, other, unsafe, suspected := 0, 0, 0, false
	for _, r := range o.Records {
		if r.Type == "CNAME" && (r.Value == "fastly.net" || strings.HasSuffix(r.Value, ".fastly.net") || strings.HasSuffix(r.Value, ".fastlylb.net")) {
			suspected = true
		}
		if r.Type != "A" && r.Type != "AAAA" {
			continue
		}
		ip, err := netip.ParseAddr(r.Value)
		if err != nil || !domain.PublicIP(ip) {
			unsafe++
			continue
		}
		if r.CIDR != "" {
			fast++
		} else {
			other++
		}
	}
	if unsafe > 0 {
		return "indeterminate"
	}
	if fast > 0 && other > 0 {
		return "mixed"
	}
	if fast > 0 {
		return "confirmed"
	}
	if suspected {
		return "suspected"
	}
	if other > 0 {
		return "not_detected"
	}
	return "indeterminate"
}

func (c *Client) Detect(ctx context.Context, d domain.DomainEntry, ranges domain.RangeSnapshot) domain.DetectionResult {
	now := c.Now().UTC()
	result := domain.DetectionResult{DomainID: d.ID, Hostname: d.Hostname, ObservedAt: now, ExpiresAt: now.Add(15 * time.Minute), Sources: make([]domain.DNSObservation, 2), Candidates: []string{}, Warnings: []string{}}
	if !fastly.Valid(ranges, now) {
		result.Classification = "indeterminate"
		result.Warnings = append(result.Warnings, "Fastly 網段資料無效")
		return result
	}
	if ranges.ExpiresAt.Before(result.ExpiresAt) {
		result.ExpiresAt = ranges.ExpiresAt
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i, e := range c.endpoints {
		wg.Go(func() { result.Sources[i] = c.observe(ctx, e, d.Hostname, ranges) })
	}
	wg.Wait()
	a, b := classify(result.Sources[0]), classify(result.Sources[1])
	switch {
	case a == "indeterminate" || b == "indeterminate":
		result.Classification = "indeterminate"
	case a == b:
		result.Classification = a
	default:
		result.Classification = "mixed"
	}
	if a != b {
		result.Warnings = append(result.Warnings, "Google 與 Cloudflare 的 CDN 歸屬證據不同")
	}
	seen := map[string]bool{}
	for _, s := range result.Sources {
		for _, r := range s.Records {
			if expires := s.ObservedAt.Add(time.Duration(r.TTL) * time.Second); expires.Before(result.ExpiresAt) {
				result.ExpiresAt = expires
			}
			if r.CIDR != "" && !seen[r.Value] {
				seen[r.Value] = true
				result.Candidates = append(result.Candidates, r.Value)
			}
		}
	}
	sort.Strings(result.Candidates)
	return result
}
