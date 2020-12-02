package utils

import (
	"net"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// AddressFilter is a function type to filter addresses
type AddressFilter func(netlink.Addr) bool

// RouteFilter is a function type to filter routes
type RouteFilter func(netlink.Route) bool

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

// AddressesRouting takes a slice of Virtual IPs and returns a slice of configured addresses in the current network namespace that directly route to those vips. You can optionally pass an AddressFilter to further filter down which addresses are considered
func AddressesRouting(vips []net.IP, af AddressFilter) ([]net.IP, error) {
	addrMap, err := getAddrs(af)
	if err != nil {
		return nil, err
	}

	var routeMap map[int][]netlink.Route
	matches := make([]net.IP, 0)
	for link, addresses := range addrMap {
		for _, address := range addresses {
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
						log.Infof("Checking route %+v (mask %s) for address %+v", route, route.Dst.Mask, address)
						containmentNet := net.IPNet{IP: address.IP, Mask: route.Dst.Mask}
						for _, vip := range vips {
							log.Infof("Checking whether address %s with route %s contains VIP %s", address, route, vip)
							if containmentNet.Contains(vip) {
								log.Infof("Address %s with route %s contains VIP %s", address, route, vip)
								matches = append(matches, address.IP)
							}
						}
					}
				}
			} else {
				for _, vip := range vips {
					log.Infof("Checking whether address %s contains VIP %s", address, vip)
					if address.Contains(vip) {
						log.Infof("Address %s contains VIP %s", address, vip)
						matches = append(matches, address.IP)
					}
				}
			}
		}

	}
	return matches, nil
}

// defaultRoute returns true if the passed route is a default route
func defaultRoute(route netlink.Route) bool {
	return route.Dst == nil
}

// AddressesDefault and returns a slice of configured addresses in the current network namespace associated with default routes. You can optionally pass an AddressFilter to further filter down which addresses are considered
func AddressesDefault(af AddressFilter) ([]net.IP, error) {
	addrMap, err := getAddrs(af)
	if err != nil {
		return nil, err
	}
	routeMap, err := getRouteMap(defaultRoute)
	if err != nil {
		return nil, err
	}

	matches := make([]net.IP, 0)
	for link, addresses := range addrMap {
		if routeMap[link.Attrs().Index] == nil {
			continue
		}
		for _, address := range addresses {
			log.Infof("Address %s is on interface %s with default route", address, link.Attrs().Name)
			matches = append(matches, address.IP)
		}
	}
	return matches, nil
}
