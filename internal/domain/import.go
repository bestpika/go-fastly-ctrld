package domain

import (
	"encoding/csv"
	"io"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

type ImportRow struct {
	Line        int         `json:"line"`
	Entry       DomainEntry `json:"entry"`
	State       string      `json:"state"`
	Message     string      `json:"message"`
	PreviousURL string      `json:"previousUrl"`
}

type ImportPreview struct {
	ID       string      `json:"id"`
	Revision string      `json:"revision"`
	Rows     []ImportRow `json:"rows"`
}

func Normalize(host string) (string, error) {
	host = strings.TrimSpace(strings.TrimPrefix(host, "\ufeff"))
	if !utf8.ValidString(host) || strings.ContainsAny(host, "/:\\*?#@[]") || strings.ContainsFunc(host, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return "", Fail("INVALID_INPUT", "請輸入公開網域，不含 IP、埠或萬用字元")
	}
	host = strings.TrimSuffix(host, ".")
	ascii, err := idna.Lookup.ToASCII(host)
	ascii = strings.ToLower(ascii)
	if err != nil || len(ascii) > 253 {
		return "", Fail("INVALID_INPUT", "網域格式不正確")
	}
	if _, err = netip.ParseAddr(ascii); err == nil {
		return "", Fail("INVALID_INPUT", "不接受 IP 位址")
	}
	parts := strings.Split(ascii, ".")
	if len(parts) < 2 {
		return "", Fail("INVALID_INPUT", "不接受本機或內部網域")
	}
	for _, p := range parts {
		if len(p) == 0 || len(p) > 63 || p[0] == '-' || p[len(p)-1] == '-' {
			return "", Fail("INVALID_INPUT", "網域標籤格式不正確")
		}
		for _, r := range p {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
				return "", Fail("INVALID_INPUT", "網域標籤格式不正確")
			}
		}
	}
	for _, suffix := range []string{"local", "localhost", "internal", "lan", "home", "test", "invalid", "example", "onion"} {
		if parts[len(parts)-1] == suffix {
			return "", Fail("UNSAFE_TARGET", "不接受特殊用途或內部網域")
		}
	}
	return ascii, nil
}

func ValidateURL(host, raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") || len(raw) > 4096 {
		return Fail("INVALID_INPUT", "測速網址限同網域 HTTPS，不含帳密或片段")
	}
	h, err := Normalize(u.Hostname())
	if err != nil || h != host {
		return Fail("UNSAFE_TARGET", "測速網址只能使用相同網域與 HTTPS 443")
	}
	return nil
}

func ParseImport(text, format, revision string, existing []DomainEntry) (ImportPreview, error) {
	if len(text) > 1<<20 || !utf8.ValidString(text) {
		return ImportPreview{}, Fail("INVALID_INPUT", "匯入內容必須是 UTF-8，且不可超過 1 MiB")
	}
	p := ImportPreview{ID: ID(), Revision: revision, Rows: []ImportRow{}}
	seen := map[string]DomainEntry{}
	for _, e := range existing {
		seen[e.Hostname] = e
	}
	add := func(line int, raw, enabled, testURL string) {
		r := ImportRow{Line: line, State: "valid", Entry: DomainEntry{ID: ID(), DisplayName: strings.TrimSpace(raw), Enabled: true, TestURL: strings.TrimSpace(testURL)}}
		value := strings.TrimSpace(raw)
		var extractErr error
		if strings.Contains(value, "://") {
			u, e := url.Parse(value)
			if e != nil {
				extractErr = Fail("INVALID_INPUT", "網址格式不正確")
			} else {
				raw = u.Hostname()
				if r.Entry.TestURL != "" && r.Entry.TestURL != value {
					extractErr = Fail("INVALID_INPUT", "網址與測速欄位衝突")
				} else {
					r.Entry.TestURL = value
				}
			}
		}
		h, err := Normalize(raw)
		if extractErr != nil {
			err = extractErr
		}
		if err == nil && enabled != "" {
			r.Entry.Enabled, err = strconv.ParseBool(strings.TrimSpace(enabled))
		}
		if err == nil {
			err = ValidateURL(h, r.Entry.TestURL)
		}
		r.Entry.Hostname = h
		if err != nil {
			r.State = "invalid"
			r.Message = "網域、啟用欄位或測速網址格式不正確"
		} else if before, exists := seen[h]; exists {
			r.Entry.ID = before.ID
			r.Entry.Enabled = before.Enabled
			r.PreviousURL = before.TestURL
			if r.Entry.TestURL == "" || r.Entry.TestURL == before.TestURL {
				r.State = "duplicate"
				r.Message = "完整主機名稱與資源已存在"
			} else if before.TestURL == "" {
				r.State = "update"
				r.Message = "確認後補入測速網址"
				seen[h] = r.Entry
			} else {
				r.State = "conflict"
				r.Message = "已有不同網址；須明確選擇是否取代"
			}
		} else {
			seen[h] = r.Entry
		}
		p.Rows = append(p.Rows, r)
	}
	if format == "csv" {
		r := csv.NewReader(strings.NewReader(strings.TrimPrefix(text, "\ufeff")))
		r.TrimLeadingSpace = true
		header, err := r.Read()
		if err != nil {
			return p, Fail("INVALID_INPUT", "CSV 必須有 domain 標題欄位")
		}
		columns := map[string]int{}
		for i, v := range header {
			columns[strings.ToLower(strings.TrimSpace(v))] = i
		}
		if _, ok := columns["domain"]; !ok {
			return p, Fail("INVALID_INPUT", "CSV 缺少 domain 標題欄位")
		}
		for {
			row, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return p, Fail("INVALID_INPUT", "CSV 欄位數或引號格式不正確")
			}
			line, _ := r.FieldPos(0)
			get := func(key string) string {
				if i, ok := columns[key]; ok && i < len(row) {
					return row[i]
				}
				return ""
			}
			add(line, get("domain"), get("enabled"), get("test_url"))
			if len(p.Rows) > 5000 {
				return p, Fail("INVALID_INPUT", "單次匯入最多 5,000 個網域")
			}
		}
	} else if format == "txt" || format == "paste" {
		for i, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) != "" {
				add(i+1, line, "", "")
			}
		}
	} else {
		return p, Fail("INVALID_INPUT", "僅支援貼上、TXT 或 CSV")
	}
	if len(p.Rows) > 5000 {
		return p, Fail("INVALID_INPUT", "單次匯入最多 5,000 個網域")
	}
	return p, nil
}
