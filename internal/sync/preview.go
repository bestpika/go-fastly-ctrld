package rulesync

import (
	"net/netip"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/dns"
	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/fastly"
)

func Build(s domain.Settings, n domain.NetworkSnapshot, entries []domain.DomainEntry, results map[string]domain.TestResult, ranges domain.RangeSnapshot, rules map[string]domain.Rule, groups []domain.Group, detections map[string]domain.DetectionResult) domain.SyncPreview {
	p := domain.SyncPreview{ID: domain.ID(), Kind: "apply", ProfileID: s.ProfileID, CredentialRevision: s.CredentialRevision, SettingsRevision: s.Revision, NetworkFingerprint: n.Fingerprint, ExpiresAt: time.Now().UTC().Add(15 * time.Minute), Items: []domain.PreviewItem{}}
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		i := domain.PreviewItem{ID: domain.ID(), DomainID: entry.ID, Hostname: entry.Hostname, Operation: "create", ForceWrite: s.ForceWrite}
		if before, ok := rules[entry.Hostname]; ok {
			i.Before = &before
			i.BeforeDigest = before.ObservationDigest()
			i.Operation = "update"
		}
		r, ok := results[entry.ID]
		i.ResultID = r.ID
		switch {
		case !domain.RegularFolder(groups, s.DestinationFolderID):
			i.Reason = "目的資料夾無效，請重新選擇"
		case !ok:
			i.Reason = "尚未測試"
		case r.StrategyVersion != domain.ProbeStrategyVersion:
			i.Reason = "測試策略已更新，請重新測試"
		case r.BaselineService:
			i.Reason = "基準結果不可套用"
		case !r.Complete:
			i.Reason = "測試未完成，無法套用"
		case r.DomainID != entry.ID || r.Hostname != entry.Hostname:
			i.Reason = "測試結果與網域不一致"
		case r.SettingsRevision != s.Revision || r.NetworkFingerprint != n.Fingerprint:
			i.Reason = "設定或網路已變更，請重新測試"
		case !fastly.Valid(ranges, time.Now()):
			i.Reason = "官方網段資料無效"
		case r.BestIPv4 == "" && r.BestIPv6 == "" && !s.ForceWrite:
			i.Reason = "沒有符合改善門檻的候選"
		case r.BestIPv6 != "" && (!s.Probe.EnableIPv6 || !n.IPv6):
			i.Reason = "IPv6 未選用或目前不可用，請重新測試"
		case i.Before != nil && !i.Before.Safe:
			i.Reason = i.Before.UnsafeReason
		default:
			fallback := r.BestIPv4 == "" && r.BestIPv6 == ""
			after := domain.Rule{Hostname: entry.Hostname, Do: 2, Status: 1, Safe: true}
			if i.Before != nil {
				after = *i.Before
				after.Do = 2
				after.Status = 1
				after.Raw = nil
				after.Safe = true
				after.UnsafeReason = ""
			}
			if r.BestIPv4 != "" {
				after.Via = r.BestIPv4
			}
			if r.BestIPv6 != "" && n.IPv6 {
				after.ViaV6 = r.BestIPv6
			}
			if fallback {
				d := detections[entry.ID]
				via, viaV6, _, err := dns.OriginalTarget(d, entry, ranges, s.Probe.EnableIPv6 && n.IPv6, time.Now())
				if err == nil {
					err = dns.ValidateOriginalRules(entry.Hostname, d, rules)
				}
				if err != nil {
					i.Reason = domain.Failure[bool](err).Error.Message
					break
				}
				after.Via = via
				if viaV6 != "" {
					after.ViaV6 = viaV6
				}
				i.OriginalDNS = &d
				if d.ExpiresAt.Before(p.ExpiresAt) {
					p.ExpiresAt = d.ExpiresAt
				}
			}
			ip, e := netip.ParseAddr(after.Via)
			if !fallback && (e != nil || !ip.Is4()) {
				i.Reason = "需要已驗證的 IPv4 目標才能建立指定 IP 規則"
				break
			}
			if r.BestIPv4 != "" && fastly.Match(r.BestIPv4, ranges) == "" {
				i.Reason = "IPv4 候選不在官方網段"
				break
			}
			if r.BestIPv6 != "" && fastly.Match(r.BestIPv6, ranges) == "" {
				i.Reason = "IPv6 候選不在官方網段"
				break
			}
			if after.ViaV6 != "" {
				v6, e := netip.ParseAddr(after.ViaV6)
				if e != nil || !v6.Is6() {
					i.Reason = "既有 IPv6 目標無法作為 IPv6 保留"
					break
				}
			}
			after.Group = s.DestinationFolderID
			i.Move = i.Before != nil && i.Before.Group != after.Group
			i.After = &after
			i.Applicable = true
			i.Reason = "已達改善門檻"
			if fallback {
				i.Reason = "強制回填原始 DNS"
			}
			if i.Before != nil && i.Before.SemanticDigest() == after.SemanticDigest() {
				if s.ForceWrite {
					i.Operation = "rewrite"
					i.Reason += "；同值重寫"
				} else {
					i.Applicable = false
					i.Operation = "unchanged"
					i.Reason = "目前規則已是目標值"
				}
			}
			if i.Before != nil && i.Before.Do != 2 {
				i.Reason += "；取代原動作"
			}
			if i.Move {
				i.Reason += "；移動資料夾"
			}
			if r.ExpiresAt.Before(p.ExpiresAt) {
				p.ExpiresAt = r.ExpiresAt
			}
		}
		p.Items = append(p.Items, i)
	}
	// 同一批預覽的新規則也可能涵蓋另一項的 CNAME，必須一併檢查。
	planned := make(map[string]domain.Rule, len(rules)+len(p.Items))
	for host, rule := range rules {
		planned[host] = rule
	}
	for _, item := range p.Items {
		if item.Applicable && item.After != nil {
			planned[item.Hostname] = *item.After
		}
	}
	for index := range p.Items {
		item := &p.Items[index]
		if item.Applicable && item.OriginalDNS != nil {
			if err := dns.ValidateOriginalRules(item.Hostname, *item.OriginalDNS, planned); err != nil {
				item.Applicable = false
				item.Reason = domain.Failure[bool](err).Error.Message
			}
		}
	}
	return p
}

func BuildRestore(s domain.Settings, n domain.NetworkSnapshot, job domain.Job, rules map[string]domain.Rule) domain.SyncPreview {
	p := domain.SyncPreview{ID: domain.ID(), Kind: "restore", ProfileID: s.ProfileID, CredentialRevision: s.CredentialRevision, SettingsRevision: s.Revision, NetworkFingerprint: n.Fingerprint, ExpiresAt: time.Now().UTC().Add(15 * time.Minute), Items: []domain.PreviewItem{}}
	for _, item := range job.Items {
		if item.State != "verified" {
			continue
		}
		i := domain.PreviewItem{ID: domain.ID(), DomainID: item.Preview.DomainID, Hostname: item.Preview.Hostname, RestoreOf: job.ID, Operation: "delete", After: item.Preview.Before}
		if i.After != nil {
			i.Operation = "restore"
		}
		current, ok := rules[i.Hostname]
		if ok {
			i.Before = &current
			i.BeforeDigest = current.ObservationDigest()
		}
		if job.ProfileID != s.ProfileID {
			i.Reason = "目前 Profile 與歷史作業不同"
		} else if item.After == nil {
			if !ok {
				i.Applicable = true
				i.Reason = "目標仍不存在"
			} else {
				i.Reason = "目標已有後續規則，不能覆蓋"
			}
		} else if !ok || current.ObservationDigest() != item.After.ObservationDigest() {
			i.Reason = "目前規則與工具後值不同，保留後續修改"
		} else if i.After != nil && !i.After.Safe {
			i.Reason = "原值無法安全寫回"
		} else {
			i.Applicable = true
			i.Reason = "回復前再次核對後值；不刪除資料夾"
		}
		p.Items = append(p.Items, i)
	}
	return p
}
