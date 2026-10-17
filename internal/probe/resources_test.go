package probe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

type resourceTransport func(*http.Request) (*http.Response, error)

func (f resourceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestServiceProfilesAndTwitchAssetPriority(t *testing.T) {
	p := service(domain.DomainEntry{Hostname: "www.twitch.tv"})
	if p.host != "www.twitch.tv" || p.speed != "" || !p.anyStatus {
		t.Fatal("首頁沒有獨立連線驗證")
	}
	if got := twitchAsset([]byte(`<script src="https://assets.twitch.tv/assets/core-old.js"></script><script src="https://assets.twitch.tv/assets/nomodulePolyfills-new.js"></script>`)); !strings.Contains(got, "nomodulePolyfills") {
		t.Fatal("未優先選擇上游 JavaScript 資源")
	}
	if service(domain.DomainEntry{Hostname: "pbs.twimg.com"}).speed != "x" {
		t.Fatal("X 沒有使用自身圖片下載")
	}
	if service(domain.DomainEntry{Hostname: "www.twitch.tv", TestURL: "https://www.twitch.tv/custom"}).speed != "url" {
		t.Fatal("忽略自訂同域 URL")
	}
	if got := twitchAsset([]byte(`<script src="https://assets.twitch.tv/assets/nomodulePolyfills-z.js"></script><script src="https://assets.twitch.tv/assets/nomodulePolyfills-a.js"></script>`)); got != "https://assets.twitch.tv/assets/nomodulePolyfills-a.js" {
		t.Fatal("未依原版 sorted 去重選檔")
	}
}

func TestGitHubSignsEachRoundAndRejectsUnsafeRedirect(t *testing.T) {
	count := 0
	unsafe := false
	e := Engine{DiscoveryHTTP: &http.Client{Transport: resourceTransport(func(r *http.Request) (*http.Response, error) {
		status := 200
		body := ""
		headers := http.Header{}
		switch r.URL.Hostname() {
		case "api.github.com":
			count++
			body = `{"assets":[{"size":1000000,"browser_download_url":"https://github.com/ollama/ollama/releases/download/v1/a.zip"}]}`
		case "github.com":
			status = 302
			location := fmt.Sprintf("https://release-assets.githubusercontent.com/github/a.zip?signature=%d", count)
			if unsafe {
				location = "https://127.0.0.1/private"
			}
			headers.Set("Location", location)
		case "release-assets.githubusercontent.com":
		default:
			t.Fatal("請求不安全的重新導向")
		}
		return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}}
	b := &Budget{Limit: 1000000}
	p := domain.DefaultSettings().Probe
	a, err := e.githubAsset(context.Background(), p, b)
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.githubAsset(context.Background(), p, b)
	if err != nil || a == second || count != 2 || b.Used() == 0 {
		t.Fatal("未重取簽章或未計入探索預算")
	}
	unsafe = true
	if _, err = e.githubAsset(context.Background(), p, b); err == nil {
		t.Fatal("接受私有重新導向")
	}
}
