package probe

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

var twitchScripts = regexp.MustCompile(`https://assets\.twitch\.tv/assets/[^"'<>\s]+\.js(?:\?[^"'<>\s]*)?`)

func twitchAsset(body []byte) string {
	values := twitchScripts.FindAllString(string(body), -1)
	slices.Sort(values)
	values = slices.Compact(values)
	for _, needle := range []string{"nomodulePolyfills", "core-", ""} {
		for _, value := range values {
			if strings.Contains(value, needle) {
				value = html.UnescapeString(value)
				if domain.ValidateURL("assets.twitch.tv", value) == nil {
					return value
				}
			}
		}
	}
	return ""
}

func (h *HTTPS) readMetadata(ctx context.Context, rawURL, ip string, p domain.ProbeSettings, budget *Budget, limit int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || domain.ValidateURL(u.Hostname(), rawURL) != nil {
		return nil, errors.New("不安全的服務資源")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	n, release := budget.Reserve(limit)
	var used int64
	defer func() { release(used) }()
	if n < 1024 {
		return nil, errors.New("資源探索下載預算不足")
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true}
	defer transport.CloseIdleConnections()
	requestContext := ctx
	transport.DialTLSContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		a, err := netip.ParseAddr(ip)
		if err != nil || !domain.PublicIP(a) {
			return nil, errors.New("不安全的資源位址")
		}
		network := "tcp4"
		if a.Is6() {
			network = "tcp6"
		}
		dial := h.dial
		if dial == nil {
			dial = (&net.Dialer{Timeout: time.Duration(p.ConnectionSeconds) * time.Second}).DialContext
		}
		conn, err := dial(ctx, network, net.JoinHostPort(ip, "443"))
		if err != nil {
			return nil, err
		}
		conn = guardIO(requestContext, conn, p.SampleSeconds)
		secure := tls.Client(conn, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12, RootCAs: h.roots})
		if err = secure.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, err
		}
		return secure, nil
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	request.Header.Set("User-Agent", "Mozilla/5.0 go-fastly-ctrld/0.1")
	request.Header.Set("Accept-Encoding", "identity")
	response, err := (&http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
	if err != nil {
		return nil, errors.New("服務資源 TLS 或 HTTPS 失敗")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("服務資源 HTTP %d", response.StatusCode)
	}
	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return nil, errors.New("服務資源不是原始資料")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, n))
	used = int64(len(body))
	if err != nil {
		return nil, errors.New("服務資源已取消或讀取失敗")
	}
	if used == n {
		return nil, errors.New("服務資源超出探索上限")
	}
	return body, nil
}

func allowedReleaseURL(raw string, initial bool) bool {
	u, err := url.Parse(raw)
	if err != nil || domain.ValidateURL(u.Hostname(), raw) != nil {
		return false
	}
	if u.Hostname() == "github.com" {
		return initial && (strings.HasPrefix(u.Path, "/ollama/ollama/releases/download/") || strings.HasPrefix(u.Path, "/cli/cli/releases/download/"))
	}
	return u.Hostname() == "release-assets.githubusercontent.com" || u.Hostname() == "objects.githubusercontent.com" || strings.HasSuffix(u.Hostname(), ".githubusercontent.com")
}

// 僅探索固定 GitHub API 與受限下載重新導向；DNS 結果先驗公開位址，無環境代理。
func (e *Engine) metadataClient(p domain.ProbeSettings) *http.Client {
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true}
	transport.TLSHandshakeTimeout = time.Duration(p.SampleSeconds) * time.Second
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" {
			return nil, errors.New("不安全的探索目標")
		}
		lookup := e.Lookup
		if lookup == nil {
			lookup = net.DefaultResolver.LookupIPAddr
		}
		values, err := lookup(ctx, host)
		if err != nil {
			return nil, err
		}
		slices.SortStableFunc(values, func(a, b net.IPAddr) int {
			if (a.IP.To4() != nil) == (b.IP.To4() != nil) {
				return 0
			}
			if a.IP.To4() != nil {
				return -1
			}
			return 1
		})
		for _, v := range values {
			a, err := netip.ParseAddr(v.IP.String())
			if err != nil || !domain.PublicIP(a) {
				continue
			}
			conn, err := (&net.Dialer{Timeout: time.Duration(p.ConnectionSeconds) * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(a.String(), "443"))
			if err == nil {
				return guardIO(ctx, conn, p.SampleSeconds), nil
			}
		}
		return nil, errors.New("探索 DNS 沒有可連線公開位址")
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if e.DiscoveryHTTP != nil {
		copy := *e.DiscoveryHTTP
		copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		return &copy
	}
	return client
}

func (e *Engine) githubAsset(ctx context.Context, p domain.ProbeSettings, budget *Budget) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client := e.metadataClient(p)
	defer client.CloseIdleConnections()
	for _, repo := range []string{"ollama/ollama", "cli/cli"} {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
		request.Header.Set("User-Agent", "go-fastly-ctrld/0.1")
		request.Header.Set("Accept-Encoding", "identity")
		response, err := client.Do(request)
		if err != nil {
			continue
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			continue
		}
		n, release := budget.Reserve(1024 * 1024)
		if n < 1024 {
			release(0)
			response.Body.Close()
			return "", errors.New("下載資源探索預算不足")
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, n))
		release(int64(len(body)))
		response.Body.Close()
		if err != nil || int64(len(body)) == n {
			continue
		}
		var releaseInfo struct {
			Assets []struct {
				Size int64  `json:"size"`
				URL  string `json:"browser_download_url"`
			} `json:"assets"`
		}
		if json.Unmarshal(body, &releaseInfo) != nil {
			continue
		}
		chosen := ""
		size := int64(0)
		for _, asset := range releaseInfo.Assets {
			if asset.Size > size && allowedReleaseURL(asset.URL, true) {
				size = asset.Size
				chosen = asset.URL
			}
		}
		if chosen == "" {
			continue
		}
		for hop := 0; hop < 5; hop++ {
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, chosen, nil)
			request.Header.Set("User-Agent", "go-fastly-ctrld/0.1")
			response, err := client.Do(request)
			if err != nil {
				break
			}
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 && allowedReleaseURL(chosen, false) {
				return chosen, nil
			}
			if response.StatusCode < 300 || response.StatusCode >= 400 {
				break
			}
			location, err := response.Location()
			if err != nil || !allowedReleaseURL(location.String(), true) {
				break
			}
			chosen = location.String()
		}
	}
	return "", errors.New("無法取得受限 GitHub Release 下載資源")
}

const xImagePath = "/media/HU2En5YbAAAByoT?format=png&name=900x900"

func (e *Engine) resource(ctx context.Context, p profile, entry domain.DomainEntry, candidates []domain.CandidateResult, r domain.RangeSnapshot, s domain.ProbeSettings, budget *Budget) (string, error) {
	switch p.speed {
	case "url":
		return entry.TestURL, nil
	case "github":
		return e.githubAsset(ctx, s, budget)
	case "x":
		return "https://pbs.twimg.com" + xImagePath, nil
	case "twitch":
		for i, c := range candidates {
			if i >= 5 {
				break
			}
			body, err := e.HTTPS.readMetadata(ctx, "https://www.twitch.tv/", p.routeIP(c.IP, r), s, budget, 512*1024)
			if err == nil {
				if asset := twitchAsset(body); asset != "" {
					return asset, nil
				}
			}
			if ctx.Err() != nil {
				break
			}
		}
		lookup := e.Lookup
		if lookup == nil {
			lookup = net.DefaultResolver.LookupIPAddr
		}
		lctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		if ips, err := lookup(lctx, "www.twitch.tv"); err == nil {
			for _, ip := range ips {
				if a, err := netip.ParseAddr(ip.IP.String()); err == nil && domain.PublicIP(a) {
					body, err := e.HTTPS.readMetadata(lctx, "https://www.twitch.tv/", a.String(), s, budget, 512*1024)
					if err == nil {
						if asset := twitchAsset(body); asset != "" {
							return asset, nil
						}
					}
				}
			}
		}
		return "", errors.New("Twitch 首頁與有界系統路線備援未提供 JavaScript 資源")
	}
	return "", nil
}
