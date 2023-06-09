package config

import (
	"fmt"
	"io/ioutil"
	"net"
	"strings"

	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
)

const (
	NodeIpIpV6File = "/run/nodeip-configuration/ipv6"
	NodeIpIpV4File = "/run/nodeip-configuration/ipv4"
)

// Return ip from primaryIp file if file and ip exists and readable
// In case of error return empty string
func GetIpFromFile(filePath string) (net.IP, error) {
	b, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.WithError(err).Infof("Failed to read ip from file %s", filePath)
		return nil, err
	}
	ip := net.ParseIP(string(b))
	if ip == nil {
		msg := fmt.Sprintf("Failed to parse ip from file %s", filePath)
		log.Errorf(msg)
		return nil, fmt.Errorf(msg)
	}
	return ip, err
}

func getInterfaceAndNonVIPAddrFromFile(vip net.IP) (*net.Interface, *net.IPNet, error) {
	ipFile := NodeIpIpV4File
	if utils.IsIPv6(vip) {
		ipFile = NodeIpIpV6File
	}

	ip, err := GetIpFromFile(ipFile)
	if err != nil {
		return nil, nil, err
	}
	return utils.GetInterfaceWithCidrByIP(ip, true)
}

// NOTE(bnemec): All addresses in the vips array must be the same ip version
func getInterfaceAndNonVIPAddr(vips []net.IP) (vipIface net.Interface, nonVipAddr *net.IPNet, err error) {
	if len(vips) < 1 {
		return vipIface, nonVipAddr, fmt.Errorf("at least one correct IPv4/IPv6 VIP needs to be fed to this function")
	}
	vipMap := make(map[string]net.IP)
	for _, vip := range vips {
		vipMap[vip.String()] = vip
	}

	// try to get interface from ip file filled by node ip service
	iface, addr, err := getInterfaceAndNonVIPAddrFromFile(vips[0])
	if err == nil {
		return *iface, addr, err
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return vipIface, nonVipAddr, err
	}

	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return vipIface, nonVipAddr, err
		}
		for _, addr := range addrs {
			switch n := addr.(type) {
			case *net.IPNet:
				if _, ok := vipMap[n.IP.String()]; ok {
					continue // This is a VIP, let's skip
				}
				_, nn, _ := net.ParseCIDR(strings.Replace(addr.String(), "/128", "/64", 1))

				if nn.Contains(vips[0]) {
					// Since IPV6 subnet is set to /64 we should also verify that
					// the candidate address and VIP address are L2 connected.
					// To make sure that the correct interface being chosen for cases like:
					// 2 interfaces , subnetA: 1001:db8::/120 , subnetB: 1001:db8::f00/120 and VIP address  1001:db8::64
					nodeAddrs, err := utils.AddressesRouting(vips, utils.ValidNodeAddress, utils.IsIPv6(vips[0]))
					if err == nil && len(nodeAddrs) > 0 && n.IP.Equal(nodeAddrs[0]) {
						return iface, n, nil
					}
				}
			default:
				fmt.Println("not supported addr")
			}
		}
	}

	nodeAddrs, err := utils.AddressesDefault(false, utils.ValidNodeAddress)
	if err != nil {
		return vipIface, nonVipAddr, err
	}
	if len(nodeAddrs) == 0 {
		return vipIface, nonVipAddr, fmt.Errorf("No interface nor address found")
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return vipIface, nonVipAddr, err
		}
		for _, addr := range addrs {
			switch n := addr.(type) {
			case *net.IPNet:
				if n.IP.String() == nodeAddrs[0].String() {
					return iface, n, nil
				}
			default:
				fmt.Println("not supported addr")
			}
		}
	}
	return vipIface, nonVipAddr, fmt.Errorf("No interface nor address found")
}
