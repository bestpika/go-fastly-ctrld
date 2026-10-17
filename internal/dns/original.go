package dns

import (
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/fastly"
)

// OriginalTarget 僅使用本輪獨立 DNS 證據，不讀系統 DNS 或既有覆寫。
// ips 是必須以原網域 Host/SNI 重新驗證的固定連線目標。
func OriginalTarget(d domain.DetectionResult, entry domain.DomainEntry, ranges domain.RangeSnapshot, ipv6 bool, now time.Time) (via, viaV6 string, ips []string, err error) {
	fail := func(message string) (string, string, []string, error) {
		return "", "", nil, domain.Fail("UNSAFE_TARGET", message)
	}
	if d.DomainID != entry.ID || d.Hostname != entry.Hostname || d.Classification != "confirmed" || len(d.Sources) != 2 || !fastly.Valid(ranges, now) {
		return fail("原始 DNS 證據不完整或來源不一致")
	}
	sources := map[string]bool{}
	first := ""
	all4, all6 := []string{}, []string{}
	for index, source := range d.Sources {
		if sources[source.Source] || source.Source != "Google" && source.Source != "Cloudflare" || source.Error != "" {
			return fail("原始 DNS 缺少雙來源證據")
		}
		sources[source.Source] = true
		cnames := map[string]string{}
		for _, record := range source.Records {
			name, e := domain.Normalize(record.Name)
			if e != nil || name != record.Name {
				return fail("原始 DNS 名稱無效")
			}
			if record.Type == "CNAME" {
				target, e := domain.Normalize(record.Value)
				if e != nil || target != record.Value || cnames[name] != "" && cnames[name] != target {
					return fail("原始 CNAME 不明確或不安全")
				}
				cnames[name] = target
			}
		}
		if index == 0 {
			first = cnames[entry.Hostname]
		} else if first != cnames[entry.Hostname] {
			return fail("Google 與 Cloudflare 的原始 CNAME 不一致")
		}
		current, visited := entry.Hostname, map[string]bool{}
		for hop := 0; ; hop++ {
			if visited[current] || hop > 8 {
				return fail("原始 CNAME 循環或超過八層")
			}
			visited[current] = true
			next := cnames[current]
			if next == "" {
				break
			}
			if next == entry.Hostname || strings.HasSuffix(next, "."+entry.Hostname) {
				return fail("原始 CNAME 會落回本規則涵蓋的網域")
			}
			current = next
		}
		v4, v6 := []string{}, []string{}
		for _, record := range source.Records {
			if record.Type == "CNAME" {
				if !visited[record.Name] {
					return fail("原始 CNAME 含不相干的鏈結")
				}
				continue
			}
			ip, e := netip.ParseAddr(record.Value)
			if record.Name != current || e != nil || !domain.PublicIP(ip) || fastly.Match(record.Value, ranges) == "" || record.Type != "A" && record.Type != "AAAA" || record.Type == "A" && !ip.Is4() || record.Type == "AAAA" && !ip.Is6() {
				return fail("原始 DNS 位址不在有效官方範圍或不屬於鏈尾")
			}
			if ip.Is4() {
				v4 = append(v4, ip.String())
			} else if ipv6 {
				v6 = append(v6, ip.String())
			}
		}
		if len(v4) == 0 {
			return fail("原始 DNS 缺少可驗證的 IPv4 目標")
		}
		slices.Sort(v4)
		slices.Sort(v6)
		// CNAME 會保留 DNS 路線，兩來源各驗一個實際鏈尾 IPv4。
		ips = append(ips, v4[0])
		all4 = append(all4, v4...)
		all6 = append(all6, v6...)
	}
	slices.Sort(all4)
	slices.Sort(all6)
	via = first
	if first == "" {
		via = all4[0]
		ips = []string{via}
		if len(all6) > 0 {
			viaV6 = all6[0]
			ips = append(ips, viaV6)
		}
	}
	slices.Sort(ips)
	return via, viaV6, slices.Compact(ips), nil
}

// ValidateOriginalRules 保守阻擋 CNAME 鏈落入其他啟用的 Profile 規則，
// 包括父域及通配符；避免回填後又被覆寫或形成跨規則循環。
func ValidateOriginalRules(hostname string, d domain.DetectionResult, rules map[string]domain.Rule) error {
	for _, source := range d.Sources {
		for _, record := range source.Records {
			if record.Type != "CNAME" {
				continue
			}
			for name, rule := range rules {
				if name == hostname || rule.Safe && rule.Status == 0 {
					continue
				}
				name = strings.TrimPrefix(name, "*.")
				if record.Value == name || strings.HasSuffix(record.Value, "."+name) {
					return domain.Fail("CONFLICT", "原始 CNAME 受其他規則涵蓋，無法回填")
				}
			}
		}
	}
	return nil
}
