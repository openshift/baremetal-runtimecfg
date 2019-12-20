package config

import (
	"fmt"
	"net"
	"strings"
)

func getInterfaceAndNonVIPAddr(vips []net.IP) (vipIface net.Interface, nonVipAddr *net.IPNet, err error) {
	if len(vips) < 1 {
		return vipIface, nonVipAddr, fmt.Errorf("At least one VIP needs to be fed to this function")
	}
	vipMap := make(map[string]net.IP)
	for _, vip := range vips {
		vipMap[vip.String()] = vip
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return vipIface, nonVipAddr, err
	}

	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		for _, addr := range addrs {
			switch n := addr.(type) {
			case *net.IPNet:
				if _, ok := vipMap[n.IP.String()]; ok {
					continue // This is a VIP, let's skip
				}
				// FIXME: Breaks if more then one interface on the host has the same prefix
				// DHCPv6 assigns addresses with a /128 if encountered assume /64
				// so that the n.Contains returns true if the VIP has the same prefix
				_, n, _ = net.ParseCIDR(strings.Replace(addr.String(), "/128", "/64", 1))

				if n.Contains(vips[0]) {
					return iface, n, err
				}
			default:
				fmt.Println("not supported addr")
			}
		}
	}

	return vipIface, nonVipAddr, fmt.Errorf("No interface nor address found for the given VIPs")
}
