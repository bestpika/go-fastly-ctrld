package probe

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

func TestFixedIPTLSAndHardBodyBudget(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "example.com" {
			t.Error("Host 錯誤")
		}
		_, _ = w.Write([]byte(strings.Repeat("x", 100000)))
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	h := HTTPS{roots: roots, dial: func(ctx context.Context, _, address string) (net.Conn, error) {
		if address != "151.101.0.1:443" {
			t.Error("未固定 IP")
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	entry := domain.DomainEntry{Hostname: "example.com", TestURL: "https://example.com/test"}
	b := &Budget{Limit: 70000}
	sample := h.Measure(context.Background(), entry, "151.101.0.1", domain.DefaultSettings().Probe, b, true)
	if !sample.Success || sample.Bytes != 70000 || b.Used() != 70000 {
		t.Fatalf("下載錯誤: %+v", sample)
	}
	h.roots = nil
	if h.Measure(context.Background(), entry, "151.101.0.1", domain.DefaultSettings().Probe, b, false).Success {
		t.Fatal("接受未信任憑證")
	}
}

func TestShortBlocksAndTruncatedBodies(t *testing.T) {
	buffer := make([]byte, 16)
	if n, err := readBlock(iotest.OneByteReader(strings.NewReader("abcdefghijklmnop")), buffer); n != 16 || err != nil {
		t.Fatal("短 Read 被當作整塊")
	}
	reader := io.MultiReader(strings.NewReader("abc"), iotest.ErrReader(io.ErrUnexpectedEOF))
	if n, err := readBlock(reader, buffer); n != 3 || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("截斷錯誤被吞掉")
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/truncated" {
			w.Header().Set("Content-Length", "1000")
		} else if r.URL.Path == "/empty" {
			return
		} else if r.URL.Path == "/short" {
			w.WriteHeader(200)
		} else {
			w.Header().Set("Location", "https://other.example.com/")
			w.WriteHeader(302)
		}
		_, _ = io.WriteString(w, "abc")
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	h := HTTPS{roots: roots, dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	entry := domain.DomainEntry{Hostname: "example.com", TestURL: "https://example.com/redirect"}
	sample := h.Measure(context.Background(), entry, "151.101.0.1", domain.DefaultSettings().Probe, &Budget{}, true)
	if sample.Success || sample.Mbps != 0 || sample.HTTPStatus != 302 {
		t.Fatalf("3xx 本文被當成檔案下載：%+v", sample)
	}
	entry.TestURL = "https://example.com/empty"
	if h.Measure(context.Background(), entry, "151.101.0.1", domain.DefaultSettings().Probe, &Budget{}, true).Success {
		t.Fatal("空本文仍是有效樣本")
	}
	entry.TestURL = "https://example.com/short"
	sample = h.Measure(context.Background(), entry, "151.101.0.1", domain.DefaultSettings().Probe, &Budget{}, true)
	if !sample.Success || sample.Bytes != 3 || sample.ElapsedMilliseconds <= 0 || sample.ElapsedMilliseconds < sample.TLSMilliseconds {
		t.Fatalf("合法短檔或完整耗時遺失：%+v", sample)
	}
	entry.TestURL = "https://example.com/truncated"
	if h.Measure(context.Background(), entry, "151.101.0.1", domain.DefaultSettings().Probe, &Budget{}, true).Success {
		t.Fatal("截斷回應被當正常小檔")
	}
}

func TestRedirectUnsafeTargetsAndCancellation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "https://other.example.com/", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	dials := 0
	h := HTTPS{roots: roots, dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		dials++
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	p := domain.DefaultSettings().Probe
	entry := domain.DomainEntry{Hostname: "example.com", TestURL: "https://example.com/redirect"}
	if h.Measure(context.Background(), entry, "151.101.0.1", p, &Budget{Limit: 100000}, false).Success || dials != 1 {
		t.Fatal("重新導向未阻擋")
	}
	if h.Measure(context.Background(), entry, "127.0.0.1", p, &Budget{Limit: 100000}, false).Success || dials != 1 {
		t.Fatal("私有目標仍建立連線")
	}
	entry.TestURL = "https://other.example.com/"
	if h.Measure(context.Background(), entry, "151.101.0.1", p, &Budget{Limit: 100000}, false).Success || dials != 1 {
		t.Fatal("跨域資源仍建立連線")
	}
	entry.TestURL = "https://example.com/slow"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if h.Measure(ctx, entry, "151.101.0.1", p, &Budget{Limit: 100000}, true).Success || time.Since(start) > time.Second {
		t.Fatal("讀取取消未遵守期限")
	}
}
