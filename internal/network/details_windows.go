//go:build windows

package network

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func platformDetails() ([]string, []string, error) {
	size := uint32(16384)
	for range 3 {
		buffer := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, windows.GAA_FLAG_INCLUDE_GATEWAYS, 0, first, &size)
		if err == windows.ERROR_BUFFER_OVERFLOW {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		routes, dns := []string{}, []string{}
		for a := first; a != nil; a = a.Next {
			if a.OperStatus != windows.IfOperStatusUp {
				continue
			}
			for g := a.FirstGatewayAddress; g != nil; g = g.Next {
				if g.Address.Sockaddr != nil {
					routes = append(routes, fmt.Sprintf("%d|%s|%d|%d", a.IfIndex, g.Address.IP(), a.Ipv4Metric, a.Ipv6Metric))
				}
			}
			for d := a.FirstDnsServerAddress; d != nil; d = d.Next {
				if d.Address.Sockaddr != nil {
					dns = append(dns, fmt.Sprintf("%d|%s", a.IfIndex, d.Address.IP()))
				}
			}
		}
		return routes, dns, nil
	}
	return nil, nil, fmt.Errorf("網路設定變動過於頻繁")
}
