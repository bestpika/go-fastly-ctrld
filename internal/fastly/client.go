package fastly

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"sort"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
)

const Endpoint = "https://api.fastly.com/public-ip-list"

type Client struct {
	HTTP  *http.Client
	Cache *storage.Store
	URL   string
	Now   func() time.Time
}

func New(cache *storage.Store) *Client {
	return &Client{HTTP: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Cache: cache, URL: Endpoint, Now: time.Now}
}

func ParseCIDRs(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 4096 {
		return nil, domain.Fail("NO_VALID_RANGES", "Fastly 網段清單數量不正確")
	}
	set := map[string]bool{}
	for _, v := range values {
		p, err := netip.ParsePrefix(v)
		if err != nil || p != p.Masked() || p.Bits() == 0 || !domain.PublicIP(p.Addr()) {
			return nil, domain.Fail("NO_VALID_RANGES", "Fastly 網段清單包含不安全或無效資料")
		}
		set[p.String()] = true
	}
	out := []string{}
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out, nil
}

func (c *Client) Valid(s domain.RangeSnapshot) bool {
	return Valid(s, c.Now())
}

// Valid 只驗證官方清單內容；時間僅用於 Get 的快取刷新。
func Valid(s domain.RangeSnapshot, _ time.Time) bool {
	parsed, err := ParseCIDRs(s.CIDRs)
	return err == nil && s.Digest == domain.Digest(parsed)
}

func (c *Client) Get(ctx context.Context) (domain.RangeSnapshot, error) {
	var cached domain.RangeSnapshot
	usable := c.Cache != nil && c.Cache.Read("fastly.json", &cached) == nil && c.Valid(cached)
	if usable && cached.ExpiresAt.After(c.Now()) {
		return cached, nil
	}
	fresh, err := c.fetch(ctx)
	if err != nil && usable && ctx.Err() == nil && domain.Failure[bool](err).Error.Code != "STORAGE_ERROR" {
		return cached, nil
	}
	return fresh, err
}

func (c *Client) fetch(ctx context.Context) (domain.RangeSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return domain.RangeSnapshot{}, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return domain.RangeSnapshot{}, domain.Fail("NO_VALID_RANGES", "無法取得 Fastly 網段清單")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return domain.RangeSnapshot{}, domain.Fail("NO_VALID_RANGES", "Fastly 網段服務未成功回應")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(b) > 1<<20 {
		return domain.RangeSnapshot{}, domain.Fail("NO_VALID_RANGES", "Fastly 網段回應超出上限或讀取失敗")
	}
	var data struct {
		Addresses []string `json:"addresses"`
		IPv6      []string `json:"ipv6_addresses"`
	}
	if json.Unmarshal(b, &data) != nil {
		return domain.RangeSnapshot{}, domain.Fail("NO_VALID_RANGES", "Fastly 網段回應格式不正確")
	}
	values, err := ParseCIDRs(append(data.Addresses, data.IPv6...))
	if err != nil {
		return domain.RangeSnapshot{}, err
	}
	s := domain.RangeSnapshot{CIDRs: values, FetchedAt: c.Now().UTC(), ExpiresAt: c.Now().UTC().Add(24 * time.Hour), Digest: domain.Digest(values)}
	if c.Cache != nil {
		if err = c.Cache.Write("fastly.json", s); err != nil {
			return domain.RangeSnapshot{}, domain.Fail("STORAGE_ERROR", "Fastly 網段快取無法安全保存")
		}
	}
	return s, nil
}

func Match(ip string, s domain.RangeSnapshot) string {
	a, err := netip.ParseAddr(ip)
	if err != nil || !domain.PublicIP(a) {
		return ""
	}
	for _, v := range s.CIDRs {
		p, err := netip.ParsePrefix(v)
		if err == nil && p.Contains(a) {
			return v
		}
	}
	return ""
}
