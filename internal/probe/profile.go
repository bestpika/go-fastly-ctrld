package probe

import (
	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/fastly"
	"net/netip"
)

// 服務路線與資源策略參考 fastly-hosts-adjust 0.8.3；不是位址分配保證。
type profile struct {
	name, host, path, speed string
	suffix                  int
	anyStatus               bool
}

func service(entry domain.DomainEntry) profile {
	p := profile{name: "自訂網域", host: entry.Hostname, path: "/", suffix: -1, anyStatus: true}
	if entry.TestURL != "" {
		p.speed = "url"
		return p
	}
	switch entry.Hostname {
	case "www.twitch.tv", "gql.twitch.tv", "static-cdn.jtvnw.net", "assets.twitch.tv", "k.twitchcdn.net", "jtvnw.twitchcdn.net":
		p.name, p.suffix = "Twitch", 214
		if entry.Hostname == "jtvnw.twitchcdn.net" {
			// 新網域依自身 DNS 尾碼探索，不假設與其他 Twitch 路線相同。
			p.suffix = -1
		}
		if entry.Hostname == "k.twitchcdn.net" {
			p.suffix = 167
		}
		if entry.Hostname == "static-cdn.jtvnw.net" {
			p.path = "/jtv_user_pictures/xarth/404_user_70x70.png"
		}
		if entry.Hostname == "assets.twitch.tv" {
			p.speed = "twitch"
		}
	case "raw.githubusercontent.com", "release-assets.githubusercontent.com":
		p.name, p.suffix = "GitHub", 133
		if entry.Hostname == "raw.githubusercontent.com" {
			p.path = "/github/gitignore/main/README.md"
		} else {
			p.speed = "github"
		}
	case "abs.twimg.com", "pbs.twimg.com", "video.twimg.com", "video-s.twimg.com":
		p.name = "X"
		if entry.Hostname == "pbs.twimg.com" {
			p.path, p.suffix, p.speed = xImagePath, 159, "x"
		}
	}
	return p
}

func (p profile) routeIP(ip string, ranges domain.RangeSnapshot) string {
	a, err := netip.ParseAddr(ip)
	if err != nil || !a.Is4() || p.suffix < 0 || fastly.Match(ip, ranges) == "" {
		return ip
	}
	v := a.As4()
	v[3] = byte(p.suffix)
	mapped := netip.AddrFrom4(v).String()
	if fastly.Match(mapped, ranges) == "" {
		return ""
	}
	return mapped
}

func (p profile) probeEntry() domain.DomainEntry {
	return domain.DomainEntry{Hostname: p.host, TestURL: "https://" + p.host + p.path}
}

func HasDownload(entry domain.DomainEntry) bool { return service(entry).speed != "" }
