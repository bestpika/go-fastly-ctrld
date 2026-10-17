package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"strings"
	"syscall"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

type HTTPS struct {
	dial  func(context.Context, string, string) (net.Conn, error)
	roots *x509.CertPool
}

// NewHTTPS 可注入連線與信任根，預設 nil 使用系統設定。
func NewHTTPS(dial func(context.Context, string, string) (net.Conn, error), roots *x509.CertPool) HTTPS {
	return HTTPS{dial: dial, roots: roots}
}

func (h *HTTPS) Measure(ctx context.Context, entry domain.DomainEntry, ip string, p domain.ProbeSettings, budget *Budget, download bool) domain.Sample {
	return h.measure(ctx, entry, ip, p, budget, download, false)
}

func (h *HTTPS) measure(ctx context.Context, entry domain.DomainEntry, ip string, p domain.ProbeSettings, budget *Budget, download, anyStatus bool) (s domain.Sample) {
	started := time.Now()
	s = domain.Sample{Stage: "validate", Hostname: entry.Hostname, ActualIP: ip}
	defer func() { s.ElapsedMilliseconds = float64(time.Since(started)) / float64(time.Millisecond) }()
	address, err := netip.ParseAddr(ip)
	if err != nil || !domain.PublicIP(address) || domain.ValidateURL(entry.Hostname, entry.TestURL) != nil {
		s.Error = "不安全的測試目標"
		return s
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, ForceAttemptHTTP2: false}
	defer transport.CloseIdleConnections()
	requestContext := ctx
	transport.DialTLSContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		s.Stage = "tcp"
		start := time.Now()
		network := "tcp4"
		if address.Is6() {
			network = "tcp6"
		}
		dial := h.dial
		if dial == nil {
			dial = (&net.Dialer{Timeout: time.Duration(p.ConnectionSeconds) * time.Second}).DialContext
		}
		conn, err := dial(ctx, network, net.JoinHostPort(ip, "443"))
		s.TCPMilliseconds = float64(time.Since(start)) / float64(time.Millisecond)
		if err != nil {
			return nil, err
		}
		conn = guardIO(requestContext, conn, p.SampleSeconds)
		start = time.Now()
		s.Stage = "tls"
		secure := tls.Client(conn, &tls.Config{ServerName: entry.Hostname, MinVersion: tls.VersionTLS12, RootCAs: h.roots})
		if err = secure.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, err
		}
		s.TLSMilliseconds = float64(time.Since(start)) / float64(time.Millisecond)
		s.Stage = "http"
		return secure, nil
	}
	var headers time.Time
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { headers = time.Now() }, GotFirstResponseByte: func() {
		if !headers.IsZero() {
			s.TTFBMilliseconds = float64(time.Since(headers)) / float64(time.Millisecond)
		}
	}}
	ctx = httptrace.WithClientTrace(ctx, trace)
	method, url := http.MethodHead, "https://"+entry.Hostname+"/"
	if entry.TestURL != "" {
		url = entry.TestURL
	}
	if download {
		method = http.MethodGet
	}
	request, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		s.Error = "測速 URL 無效"
		return s
	}
	request.Header.Set("User-Agent", "go-fastly-ctrld/0.1")
	request.Header.Set("Accept-Encoding", "identity")
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		s.Error = connectionError(err)
		return s
	}
	defer response.Body.Close()
	s.HTTPStatus = response.StatusCode
	valid := response.StatusCode >= 200 && response.StatusCode < 300
	if !download && anyStatus {
		valid = response.StatusCode >= 200 && response.StatusCode < 500
	}
	if !valid {
		s.Error = fmt.Sprintf("HTTPS HTTP %d；未跟隨重新導向", response.StatusCode)
		return s
	}
	s.POP = response.Header.Get("X-Served-By")
	if len(s.POP) > 200 {
		s.POP = s.POP[:200]
	}
	if !download {
		s.Success = true
		s.Stage = "complete"
		return s
	}
	if encoding := strings.TrimSpace(response.Header.Get("Content-Encoding")); encoding != "" && encoding != "identity" {
		s.Error = "伺服器未提供原始下載資料"
		return s
	}
	if budget == nil {
		s.Stage = "budget"
		s.Error = "未設定下載預算"
		return s
	}
	limit := int64(8 * 1024 * 1024)
	n, release := budget.Reserve(limit)
	defer func() { release(s.Bytes) }()
	if n == 0 {
		s.Stage = "budget"
		s.Error = "下載預算不足"
		return s
	}
	s.Stage = "download"
	start := time.Now()
	buffer := make([]byte, 256*1024)
	for s.Bytes < n {
		count, readErr := readBlock(response.Body, buffer[:min(int64(len(buffer)), n-s.Bytes)])
		s.Bytes += int64(count)
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				err = readErr
			}
			break
		}
		// 對齊參考：讀完一塊才檢查兩秒，外部期限與取消仍有效。
		if time.Since(start) >= 2*time.Second {
			break
		}
	}
	seconds := time.Since(start).Seconds()
	if err != nil && ctx.Err() == nil {
		s.Error = "下載中斷"
		return s
	}
	if ctx.Err() != nil {
		s.Error = "下載已取消或超過硬期限"
		return s
	}
	if s.Bytes == 0 {
		s.Error = "下載回應沒有本文，不能作為有效下載樣本"
		return s
	}
	s.Mbps = float64(s.Bytes) * 8 / seconds / 1e6
	s.Success = true
	s.Stage = "complete"
	return s
}

// 聚滿一塊才檢查樣本時間；保留底層非 EOF 錯誤，不能把截斷回應當正常小檔。
func readBlock(reader io.Reader, buffer []byte) (int, error) {
	used, empty := 0, 0
	for used < len(buffer) {
		n, err := reader.Read(buffer[used:])
		used += n
		if err != nil {
			return used, err
		}
		if n == 0 {
			empty++
			if empty >= 100 {
				return used, io.ErrNoProgress
			}
		} else {
			empty = 0
		}
	}
	return used, nil
}

func connectionError(err error) string {
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var timeout net.Error
	var number syscall.Errno
	switch {
	case errors.Is(err, context.Canceled):
		return "連線已取消"
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout():
		return "連線或 TLS 握手逾時"
	case errors.As(err, &unknown) || errors.As(err, &hostname) || errors.As(err, &invalid):
		return "TLS 憑證驗證失敗"
	case errors.Is(err, syscall.ECONNRESET) || errors.As(err, &number) && number == 10054:
		return "遠端重設連線，TLS／HTTPS 未完成"
	default:
		return "TLS 或 HTTPS 連線失敗"
	}
}
