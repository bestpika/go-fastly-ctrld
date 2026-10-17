package fastly

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
)

func TestOfficialListAndCacheExpiry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"addresses":["151.101.0.0/16"],"ipv6_addresses":["2a04:4e42::/32"]}`))
	}))
	defer srv.Close()
	cache, _ := storage.Open(t.TempDir())
	c := New(cache)
	c.URL = srv.URL
	s, err := c.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if Match("151.101.78.214", s) == "" || Match("10.0.0.1", s) != "" {
		t.Fatal("CIDR 判定不正確")
	}
	srv.Close()
	if _, err = c.Get(context.Background()); err != nil {
		t.Fatal("有效快取未使用")
	}
	c.Now = func() time.Time { return s.ExpiresAt.Add(time.Second) }
	if got, err := c.Get(context.Background()); err != nil || got.Digest != s.Digest {
		t.Fatal("刷新失敗不應僅因時間拒絕已驗證清單")
	}
	if _, err = ParseCIDRs([]string{"10.0.0.0/8"}); err == nil {
		t.Fatal("不安全網段未拒絕")
	}
	if c.Valid(domain.RangeSnapshot{}) {
		t.Fatal("空快取不能有效")
	}
}

func TestOldOfficialCacheRefreshesContentAndRejectsCorruption(t *testing.T) {
	cache, _ := storage.Open(t.TempDir())
	old := domain.RangeSnapshot{CIDRs: []string{"151.101.0.0/16"}, ExpiresAt: time.Now().Add(-time.Hour)}
	old.Digest = domain.Digest(old.CIDRs)
	_ = cache.Write("fastly.json", old)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"addresses":["199.232.0.0/16"],"ipv6_addresses":[]}`))
	}))
	defer srv.Close()
	c := New(cache)
	c.URL = srv.URL
	fresh, err := c.Get(context.Background())
	if err != nil || calls != 1 || Match("199.232.0.133", fresh) == "" || Match("151.101.0.133", fresh) != "" {
		t.Fatal("舊清單未刷新成實際官方內容")
	}
	fresh.Digest = "corrupt"
	_ = cache.Write("fastly.json", fresh)
	srv.Close()
	if _, err = c.Get(context.Background()); err == nil {
		t.Fatal("損毀清單被當成可用備援")
	}
	_ = cache.Write("fastly.json", old)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.Get(ctx); err == nil {
		t.Fatal("快取備援掩蓋作業取消")
	}
}
