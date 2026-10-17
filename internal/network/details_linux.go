//go:build linux

package network

import (
	"os"
	"strings"
)

func platformDetails() ([]string, []string, error) {
	routes, dns := []string{}, []string{}
	for _, path := range []string{"/proc/net/route", "/proc/net/ipv6_route"} {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		for _, line := range strings.Split(string(body), "\n") {
			fields := strings.Fields(line)
			if path == "/proc/net/route" && len(fields) >= 8 && fields[0] != "Iface" {
				routes = append(routes, strings.Join([]string{fields[0], fields[1], fields[2], fields[6], fields[7]}, "|"))
			} else if path == "/proc/net/ipv6_route" && len(fields) >= 10 {
				routes = append(routes, strings.Join([]string{fields[0], fields[1], fields[2], fields[3], fields[4], fields[5], fields[9]}, "|"))
			}
		}
	}
	for _, path := range []string{"/etc/resolv.conf", "/run/systemd/resolve/resolv.conf"} {
		body, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		for _, line := range strings.Split(string(body), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "nameserver" {
				dns = append(dns, path+"|"+fields[1])
			}
		}
	}
	return routes, dns, nil
}
