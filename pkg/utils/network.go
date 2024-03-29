package utils

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

func SplitCIDR(s string) (string, string, error) {
	split := strings.Split(s, "/")
	if len(split) != 2 {
		return "", "", fmt.Errorf("not a CIDR")
	}

	return split[0], split[1], nil
}

func IsIPv4(ip net.IP) bool {
	return strings.Contains(ip.String(), ".")
}

func IsIPv6(ip net.IP) bool {
	return strings.Contains(ip.String(), ":")
}

func IsNetIPv6(network net.IPNet) bool {
	if IsIPv6(network.IP) {
		return true
	}

	if IsIPv4(network.IP) {
		// Sadly the fact that address has "." doesn't mean it's an IPv4 address. We need to check
		// subnet masks to make sure this is really the case. If subnet mask is longer than 32 we
		// are for sure dealing with IPv6 address. If it is shorter, then we need to look at the
		// total capacity of the mask as it can be simply an IPv4 address but could be as well an
		// IPv6 in really enormous subnet.
		//
		// To simplify this logic because subnet longer than 32 can happen if the capacity of mask
		// matches IPv6, we can simply look at the capacity of the mask and determine IP stack.
		_, n := network.Mask.Size()
		if n == 32 {
			return false
		}
		if n == 128 {
			return true
		}
	}

	log.Debugf("Failed to find IP stack of '%+v'", &network)
	return false
}

func IpInCidr(ipAddr, cidr string) (bool, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false, err
	}
	ip := net.ParseIP(ipAddr)
	if ip == nil {
		return false, errors.New("IP is nil")
	}
	return ipNet.Contains(ip), nil
}

func ConvertIpsToStrings(ips []net.IP) []string {
	var res []string
	for _, ip := range ips {
		if ip.String() != "" {
			res = append(res, ip.String())
		}
	}
	return res
}

func GetLocalCIDRByIP(ip string) (string, error) {
	netIP := net.ParseIP(ip)
	if netIP == nil {
		return "", fmt.Errorf("IP '%s' is not correct", ip)
	}

	_, net, err := GetInterfaceWithCidrByIP(netIP, false)
	if err != nil {
		return "", err
	}

	// In case the computed result is an IPv6 address with /128 mask, we modify it to /64 as this
	// is what it would be in reality. For some reasons they are returned as /128 even if this is
	// not the real configuration.
	return strings.Replace(net.String(), "/128", "/64", 1), nil
}
