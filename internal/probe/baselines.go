package probe

import (
	"net/netip"
	"strings"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

// 基準不是管理清單，不會由此取得修改帳號規則的資格。
// 前兩項是共享參考的下載來源，其餘網域仍各自驗證 Host／TLS。
func BaselineEntries() []domain.DomainEntry {
	return []domain.DomainEntry{
		{ID: "baseline-github", Hostname: "release-assets.githubusercontent.com", Enabled: true},
		{ID: "baseline-twitch", Hostname: "assets.twitch.tv", Enabled: true},
		{ID: "baseline-x", Hostname: "pbs.twimg.com", Enabled: true},
		{ID: "baseline-github-raw", Hostname: "raw.githubusercontent.com", Enabled: true},
		{ID: "baseline-twitch-www", Hostname: "www.twitch.tv", Enabled: true},
		{ID: "baseline-twitch-gql", Hostname: "gql.twitch.tv", Enabled: true},
		{ID: "baseline-twitch-k", Hostname: "k.twitchcdn.net", Enabled: true},
		{ID: "baseline-twitch-static", Hostname: "static-cdn.jtvnw.net", Enabled: true},
		{ID: "baseline-twitch-jtvnw", Hostname: "jtvnw.twitchcdn.net", Enabled: true},
		{ID: "baseline-x-abs", Hostname: "abs.twimg.com", Enabled: true},
		{ID: "baseline-x-video", Hostname: "video.twimg.com", Enabled: true},
		{ID: "baseline-x-video-s", Hostname: "video-s.twimg.com", Enabled: true},
	}
}

// 所選服務展開全部網域；共享參考僅補上本輪兩個下載來源，X 不依賴 GitHub。
func SelectedBaselines(p domain.ProbeSettings, entries []domain.DomainEntry) []domain.DomainEntry {
	p = domain.FixedProbe(p)
	chosen := map[string]bool{}
	for _, name := range p.Services {
		chosen[name] = true
	}
	shared := false
	for _, entry := range entries {
		shared = shared || WantsReference(entry, p)
	}
	out := []domain.DomainEntry{}
	for _, entry := range BaselineEntries() {
		group := strings.Split(strings.TrimPrefix(entry.ID, "baseline-"), "-")[0]
		if chosen[group] || shared && (entry.ID == "baseline-github" || entry.ID == "baseline-twitch") {
			out = append(out, entry)
		}
	}
	return out
}

func ReferenceFromBaselines(results []domain.TestResult, s domain.Settings, r domain.RangeSnapshot, n domain.NetworkSnapshot) (domain.SharedReference, error) {
	ref := domain.SharedReference{ID: domain.ID(), StrategyVersion: domain.ProbeStrategyVersion, NetworkFingerprint: n.Fingerprint, RangesDigest: r.Digest, ProbeDigest: domain.Digest(s.Probe), ObservedAt: time.Now().UTC()}
	ref.ExpiresAt = minTime(ref.ObservedAt.Add(15*time.Minute), r.ExpiresAt)
	for _, source := range []struct{ name, host string }{{"Twitch", "assets.twitch.tv"}, {"GitHub", "release-assets.githubusercontent.com"}} {
		found := false
		for _, result := range results {
			if result.Service != source.name || result.Hostname != source.host {
				continue
			}
			if !result.Complete || result.Mode != "download" || result.DNSClassification != "confirmed" {
				return ref, domain.Fail("NO_REFERENCE", "本輪基準下載未完成，不能當完整共享成績")
			}
			ref.Sources = append(ref.Sources, domain.ReferenceSource{Name: source.name, ProbeHost: result.ProbeHost, SpeedHost: result.SpeedHost, Candidates: result.Candidates})
			ref.Bytes += result.Bytes
			ref.ExpiresAt = minTime(ref.ExpiresAt, result.ExpiresAt)
			found = true
			break
		}
		if !found {
			return ref, domain.Fail("NO_REFERENCE", "缺少本輪基準")
		}
	}
	ref.Digest = ReferenceDigest(ref)
	return ref, nil
}

func routeIP(route string, suffix int) string {
	if p, err := netip.ParsePrefix(route); err == nil && p.Addr().Is4() {
		a := p.Addr().As4()
		a[3] = byte(suffix)
		return netip.AddrFrom4(a).String()
	}
	return route
}
