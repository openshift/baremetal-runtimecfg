package utils

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// AddressFilter is a function type to filter addresses
type AddressFilter func(netlink.Addr) bool

// RouteFilter is a function type to filter routes
type RouteFilter func(netlink.Route) bool

type addressMapFunc func(filter AddressFilter) (map[netlink.Link][]netlink.Addr, error)
type routeMapFunc func(filter RouteFilter) (map[int][]netlink.Route, error)

func getAddrs(filter AddressFilter) (addrMap map[netlink.Link][]netlink.Addr, err error) {
	nlHandle, err := netlink.NewHandle(unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	defer nlHandle.Delete()

	links, err := nlHandle.LinkList()
	if err != nil {
		return nil, err
	}

	addrMap = make(map[netlink.Link][]netlink.Addr)
	for _, link := range links {
		addresses, err := nlHandle.AddrList(link, netlink.FAMILY_ALL)
		if err != nil {
			return nil, err
		}

		if link.Attrs().OperState.String() != "up" {
			log.Debugf("Link with addresses not up %+v", addresses)
			continue
		}
		for _, address := range addresses {
			if filter != nil && !filter(address) {
				log.Debugf("Ignoring filtered address %+v", address)
				continue
			}

			if _, ok := addrMap[link]; ok {
				addrMap[link] = append(addrMap[link], address)
			} else {
				addrMap[link] = []netlink.Addr{address}
			}
		}
	}
	log.Debugf("retrieved Address map %+v", addrMap)
	return addrMap, nil
}

func getRouteMap(filter RouteFilter) (routeMap map[int][]netlink.Route, err error) {
	nlHandle, err := netlink.NewHandle(unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	defer nlHandle.Delete()

	routes, err := nlHandle.RouteList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return nil, err
	}

	routeMap = make(map[int][]netlink.Route)
	for _, route := range routes {
		if filter != nil && !filter(route) {
			log.Debugf("Ignoring filtered route %+v", route)
			continue
		}
		if _, ok := routeMap[route.LinkIndex]; ok {
			routeMap[route.LinkIndex] = append(routeMap[route.LinkIndex], route)
		} else {
			routeMap[route.LinkIndex] = []netlink.Route{route}
		}
	}

	log.Debugf("Retrieved route map %+v", routeMap)

	return routeMap, nil
}

// ValidNodeAddress returns true if the address is suitable for a node's primary IP
func ValidNodeAddress(address netlink.Addr) bool {
	// Ignore link-local addresses
	if address.IP.IsLinkLocalUnicast() {
		return false
	}

	// Ignore deprecated IPv6 addresses
	if net.IPv6len == len(address.IP) && address.PreferedLft == 0 {
		return false
	}

	return true
}

// ValidOVNNodeAddress returns true if the address is suitable for a node's primary IP
// and is not fd69::2
// we intentionally don't filter ipv4 because it isn't necessary.
func ValidOVNNodeAddress(address netlink.Addr) bool {
	if IsIPv6(address.IP) && address.IP.String() == "fd69::2" {
		return false
	}
	return ValidNodeAddress(address)
}

// usableIPv6Route returns true if the passed route is acceptable for AddressesRouting
func usableIPv6Route(route netlink.Route) bool {
	// Ignore default routes
	if route.Dst == nil {
		return false
	}
	// Ignore non-IPv6 routes
	if net.IPv6len != len(route.Dst.IP) {
		return false
	}
	// Ignore non-advertised routes
	if route.Protocol != unix.RTPROT_RA {
		return false
	}

	return true
}

// AddressesRouting takes a slice of Virtual IPs and returns a configured address in the current network namespace that directly routes to at least one of those vips. If the interface containing that address is dual-stack, it will also return a single address of the opposite IP family. You can optionally pass an AddressFilter to further filter down which addresses are considered
func AddressesRouting(vips []net.IP, af AddressFilter, preferIPv6 bool) ([]net.IP, error) {
	return addressesRoutingInternal(vips, af, getAddrs, getRouteMap, preferIPv6)
}

func addressesRoutingInternal(vips []net.IP, af AddressFilter, getAddrs addressMapFunc, getRouteMap routeMapFunc, preferIPv6 bool) ([]net.IP, error) {
	addrMap, err := getAddrs(af)
	if err != nil {
		return nil, err
	}

	var routeMap map[int][]netlink.Route
	matches := make([]net.IPNet, 0)
	for link, addresses := range addrMap {
	addrLoop:
		for _, address := range addresses {
			isVip := false
			for _, vip := range vips {
				if address.IP.String() == vip.String() {
					log.Debugf("Address %s is VIP %s. Skipping.", address, vip)
					isVip = true
				}
			}
			if isVip {
				continue
			}
			maskPrefix, maskBits := address.Mask.Size()
			if net.IPv6len == len(address.IP) && maskPrefix == maskBits {
				if routeMap == nil {
					routeMap, err = getRouteMap(usableIPv6Route)
					if err != nil {
						return nil, err
					}
				}
				if routes, ok := routeMap[link.Attrs().Index]; ok {
					for _, route := range routes {
						log.Debugf("Checking route %+v (mask %s) for address %+v", route, route.Dst.Mask, address)
						containmentNet := net.IPNet{IP: address.IP, Mask: route.Dst.Mask}
						for _, vip := range vips {
							log.Debugf("Checking whether address %s with route %s contains VIP %s", address, route, vip)
							if IsNetIPv6(containmentNet) != IsIPv6(vip) {
								log.Debugf("Stack of address '%s' and VIP '%s' does not match. Skipping.", address, vip)
							}
							if containmentNet.Contains(vip) {
								log.Debugf("Address %s with route %s contains VIP %s", address, route, vip)
								matches = append(matches, *address.IPNet)
								break addrLoop
							}
						}
					}
				}
			} else {
				for _, vip := range vips {
					log.Debugf("Checking whether address %s contains VIP %s", address, vip)
					if IsNetIPv6(*address.IPNet) != IsIPv6(vip) {
						log.Debugf("Stack of address '%s' and VIP '%s' does not match. Skipping.", address, vip)
					}
					if address.Contains(vip) {
						log.Debugf("Address %s contains VIP %s", address, vip)
						matches = append(matches, *address.IPNet)
						break addrLoop
					}
				}
			}
		}

		if len(matches) > 0 {
			// Find an address of the opposite IP family on the same interface
			for _, address := range addresses {
				if IsNetIPv6(*address.IPNet) != IsNetIPv6(matches[0]) {
					matches = append(matches, *address.IPNet)
					break
				}
			}
			break
		}
	}
	// Ensure we return addresses in the desired order
	sort.SliceStable(matches, func(i, j int) bool {
		// Very simplified since we only ever have two items in this list
		return IsNetIPv6(matches[i]) == preferIPv6
	})
	return Mapper(matches, func(ipnet net.IPNet) net.IP { return ipnet.IP }), nil
}

// defaultRoute returns true if the passed route is a default route
func defaultRoute(route netlink.Route) bool {
	return route.Dst == nil
}

// AddressesDefault returns a slice of configured addresses in the current network namespace associated with default routes; IPv4 first (if any), then IPv6 (if any). You can optionally pass an AddressFilter to further filter down which addresses are considered
func AddressesDefault(preferIPv6 bool, af AddressFilter) ([]net.IP, error) {
	return addressesDefaultInternal(preferIPv6, af, getAddrs, getRouteMap)
}

type FoundAddress struct {
	Network         net.IPNet
	Priority        int
	LinkIndex       int
	GatewayOnSubnet bool
}

func addressesDefaultInternal(preferIPv6 bool, af AddressFilter, getAddrs addressMapFunc, getRouteMap routeMapFunc) ([]net.IP, error) {
	addrMap, err := getAddrs(af)
	if err != nil {
		return nil, err
	}
	routeMap, err := getRouteMap(defaultRoute)
	if err != nil {
		return nil, err
	}

	matches := make([]net.IP, 0)
	addrs := make([]FoundAddress, 0)
	for link, addresses := range addrMap {
		linkIndex := link.Attrs().Index
		if routeMap[linkIndex] == nil {
			continue
		}
		for _, address := range addresses {
			log.Debugf("Address %s is on interface %s with default route", address, link.Attrs().Name)
			// We should only have one default route per interface
			addrs = append(addrs, FoundAddress{
				Network:         *address.IPNet,
				Priority:        routeMap[linkIndex][0].Priority,
				LinkIndex:       linkIndex,
				GatewayOnSubnet: address.IPNet.Contains(routeMap[linkIndex][0].Gw),
			})
		}
	}

	// Sort addresses into a stable order, based on:
	// a) default route priority
	// b) link
	// c) IP address family
	// d) The gateway being on the IP's CIDR
	// e) The address being public
	sort.SliceStable(addrs, func(i, j int) bool {
		if addrs[i].Priority == addrs[j].Priority {
			if addrs[i].LinkIndex == addrs[j].LinkIndex {
				if IsNetIPv6(addrs[i].Network) == IsNetIPv6(addrs[j].Network) {
					if addrs[i].GatewayOnSubnet == addrs[j].GatewayOnSubnet {
						return !addrs[i].Network.IP.IsPrivate()
					}
					return addrs[i].GatewayOnSubnet
				}
				return IsNetIPv6(addrs[i].Network) == preferIPv6
			}
			return addrs[i].LinkIndex < addrs[j].LinkIndex
		}
		return addrs[i].Priority < addrs[j].Priority
	})

	foundv4 := false
	foundv6 := false
	for _, addr := range addrs {
		if (IsNetIPv6(addr.Network) && foundv6) || (!IsNetIPv6(addr.Network) && foundv4) {
			continue
		}
		matches = append(matches, addr.Network.IP)
		if IsNetIPv6(addr.Network) {
			foundv6 = true
		} else {
			foundv4 = true
		}
	}
	return matches, nil
}

// GetInterfaceWithCidrByIP returns the interface and network that has the passed IP address
// configured. It allows to run in a non-strict mode in which it's not required to match the
// exact IP address but only a subnet.
//
// E.g. for interface configured as "192.168.1.1/24" strict mode asked about "192.168.1.2" returns
// FALSE whereas in non-strict mode it returns TRUE.
func GetInterfaceWithCidrByIP(ip net.IP, strictMatch bool) (*net.Interface, *net.IPNet, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, err
	}
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			log.WithError(err).Warnf("Failed to get addresses for %s interface", iface.Name)
			continue
		}
		for _, addr := range addrs {
			switch n := addr.(type) {
			case *net.IPNet:
				addrOffset := strings.Replace(addr.String(), "/128", "/64", 1)
				ifaceIp, _, err := net.ParseCIDR(addrOffset)
				if err == nil {
					if ifaceIp.Equal(ip) {
						return &iface, n, nil
					}
					if !strictMatch {
						match, _ := IpInCidr(ip.String(), addrOffset)
						if match {
							return &iface, n, nil
						}
					}
				}
			default:
				fmt.Println("not supported addr")
			}
		}
	}
	return nil, nil, errors.New(fmt.Sprintf("failed find a interface for the ip %s", ip.String()))
}
