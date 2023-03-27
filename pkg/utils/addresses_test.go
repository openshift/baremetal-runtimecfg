package utils

import (
	"fmt"
	"net"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var lo = &netlink.Device{
	LinkAttrs: netlink.LinkAttrs{
		Index: 0,
		Name:  "lo",
	},
}
var eth0 = &netlink.Device{
	LinkAttrs: netlink.LinkAttrs{
		Index: 1,
		Name:  "eth0",
	},
}
var eth1 = &netlink.Device{
	LinkAttrs: netlink.LinkAttrs{
		Index: 2,
		Name:  "eth1",
	},
}

func maybeAddAddress(addrMap map[netlink.Link][]netlink.Addr, af AddressFilter, link netlink.Link, addrStr string, deprecated bool) {
	addr, err := netlink.ParseAddr(addrStr)
	if err != nil {
		panic(fmt.Sprintf("bad address string %q in test case", addrStr))
	}
	if !deprecated {
		addr.PreferedLft = 999
	}
	if !strings.Contains(addrStr, ":") {
		// IPv6 has no broadcast addresses at all. On the other hand IPv4 has them. We are going
		// to use this property in order to distinguish vanilla IPv4 from IPv4-mapped-to-IPv6.
		// For the purpose of testing we don't care about the value but only about nil-ness.
		addr.Broadcast = net.ParseIP("255.255.255.255")
	}
	if af != nil && !af(*addr) {
		return
	}
	addrMap[link] = append(addrMap[link], *addr)
}

func maybeAddRoute(routeMap map[int][]netlink.Route, rf RouteFilter, link netlink.Link, destination string, ra bool, priority int, gw string) {
	var dst *net.IPNet
	var err error
	if destination != "" {
		_, dst, err = net.ParseCIDR(destination)
		if err != nil {
			panic(fmt.Sprintf("bad route string %q in test case", destination))
		}
	}
	prot := unix.RTPROT_KERNEL
	if ra {
		prot = unix.RTPROT_RA
	}
	linkIndex := link.Attrs().Index
	route := netlink.Route{
		LinkIndex: linkIndex,
		Dst:       dst,
		Protocol:  prot,
		Priority:  priority,
		Gw:        net.ParseIP(gw),
	}
	if rf != nil && !rf(route) {
		return
	}
	routeMap[linkIndex] = append(routeMap[linkIndex], route)
}

func addIPv4Addrs(addrs map[netlink.Link][]netlink.Addr, af AddressFilter) {
	maybeAddAddress(addrs, af, lo, "127.0.0.1/8", false)
	maybeAddAddress(addrs, af, lo, "::1/128", false)
	maybeAddAddress(addrs, af, eth0, "10.0.0.5/24", false)
	maybeAddAddress(addrs, af, eth0, "169.254.10.10/16", false)
	maybeAddAddress(addrs, af, eth0, "10.0.0.100/24", false)
	maybeAddAddress(addrs, af, eth1, "192.168.1.2/24", false)
}

func addIPv4Routes(routes map[int][]netlink.Route, rf RouteFilter) {
	maybeAddRoute(routes, rf, eth0, "", false, 100, "")
	maybeAddRoute(routes, rf, eth0, "10.0.0.0/24", false, 100, "")
	maybeAddRoute(routes, rf, eth1, "192.168.1.0/24", false, 100, "")
}

func addIPv4RoutesDefaultEth1(routes map[int][]netlink.Route, rf RouteFilter) {
	maybeAddRoute(routes, rf, eth0, "10.0.0.0/24", false, 100, "")
	maybeAddRoute(routes, rf, eth1, "", false, 100, "")
	maybeAddRoute(routes, rf, eth1, "192.168.1.0/24", false, 100, "")
}

func addIPv6Addrs(addrs map[netlink.Link][]netlink.Addr, af AddressFilter) {
	maybeAddAddress(addrs, af, lo, "127.0.0.1/8", false)
	maybeAddAddress(addrs, af, lo, "::1/128", false)
	maybeAddAddress(addrs, af, eth0, "fd00::5/64", false)
	maybeAddAddress(addrs, af, eth0, "fe80::1234/64", false)
	maybeAddAddress(addrs, af, eth1, "fd69::2/125", false)
	maybeAddAddress(addrs, af, eth1, "fd01::3/64", true)
	maybeAddAddress(addrs, af, eth1, "fd01::4/64", true)
	maybeAddAddress(addrs, af, eth1, "fd01::5/64", false)
	maybeAddAddress(addrs, af, eth1, "::ffff:192.168.1.160/64", false)
}

func addIPv6Routes(routes map[int][]netlink.Route, rf RouteFilter) {
	maybeAddRoute(routes, rf, eth0, "", false, 100, "")
	maybeAddRoute(routes, rf, eth0, "fd00::/64", false, 100, "")
	maybeAddRoute(routes, rf, eth0, "fd02::/64", false, 100, "")
	maybeAddRoute(routes, rf, eth1, "fd01::/64", false, 100, "")
}

func addIPv6AddrsOVN(addrs map[netlink.Link][]netlink.Addr, af AddressFilter) {
	maybeAddAddress(addrs, af, eth0, "fd69::2/125", false)
}

func addIPv6RoutesWithOVN(routes map[int][]netlink.Route, rf RouteFilter) {
	maybeAddRoute(routes, rf, eth0, "", false, 100, "")
	maybeAddRoute(routes, rf, eth0, "fd69::/64", false, 48, "")
}

func addOverlappingIPv6Addrs(addrs map[netlink.Link][]netlink.Addr, af AddressFilter) {
	maybeAddAddress(addrs, af, lo, "127.0.0.1/8", false)
	maybeAddAddress(addrs, af, lo, "::1/128", false)
	maybeAddAddress(addrs, af, eth0, "fd00::f05/120", false)
	maybeAddAddress(addrs, af, eth0, "fe80::1234/64", false)
	maybeAddAddress(addrs, af, eth1, "fd00::3/120", true)
	maybeAddAddress(addrs, af, eth1, "fd00::4/120", true)
	maybeAddAddress(addrs, af, eth1, "fd00::5/120", false)
}

func addOverlappingIPv6Routes(routes map[int][]netlink.Route, rf RouteFilter) {
	maybeAddRoute(routes, rf, eth0, "", false, 100, "")
	maybeAddRoute(routes, rf, eth0, "fd00::f00/120", false, 100, "")
	maybeAddRoute(routes, rf, eth0, "fd00::e00/120", false, 100, "")
	maybeAddRoute(routes, rf, eth1, "fd00::/120", false, 100, "")
}

func addMultipleDefaultRoutes(routes map[int][]netlink.Route, rf RouteFilter) {
	maybeAddRoute(routes, rf, eth0, "", false, 100, "")
	maybeAddRoute(routes, rf, eth1, "", false, 101, "")
}

func addMultipleDefaultRoutesReversePriority(routes map[int][]netlink.Route, rf RouteFilter) {
	maybeAddRoute(routes, rf, eth0, "", false, 101, "")
	maybeAddRoute(routes, rf, eth1, "", false, 100, "")
}

func addDummyAddrs(addrs map[netlink.Link][]netlink.Addr, af AddressFilter) {
	// when addrMap only contains eth0 and eth1, golang seems to reliably return them
	// in that order when iterating the map, such that the "multiple default routes
	// with same priority" test always gets the right answer just by accident. Adding
	// more elements to the map causes the iteration order to become unpredictable, so
	// we test that the code does the right thing even if it sees the interfaces in
	// the "wrong" order.
	for i := 10; i < 100; i++ {
		maybeAddAddress(
			addrs,
			af,
			&netlink.Device{
				LinkAttrs: netlink.LinkAttrs{
					Index: i,
					Name:  fmt.Sprintf("eth%d", i),
				},
			},
			fmt.Sprintf("1.2.3.%d/24", i),
			false,
		)
	}
}

func addMultipleDefaultRoutesSamePriority(routes map[int][]netlink.Route, rf RouteFilter) {
	maybeAddRoute(routes, rf, eth0, "", false, 100, "")
	maybeAddRoute(routes, rf, eth1, "", false, 100, "")
}

func ipv4AddrMap(af AddressFilter) (map[netlink.Link][]netlink.Addr, error) {
	addrs := make(map[netlink.Link][]netlink.Addr)
	addIPv4Addrs(addrs, af)
	return addrs, nil
}

func ipv4RouteMap(rf RouteFilter) (map[int][]netlink.Route, error) {
	routes := make(map[int][]netlink.Route)
	addIPv4Routes(routes, rf)
	return routes, nil
}

func ipv4RouteMapDefaultEth1(rf RouteFilter) (map[int][]netlink.Route, error) {
	routes := make(map[int][]netlink.Route)
	addIPv4RoutesDefaultEth1(routes, rf)
	return routes, nil
}

func ipv6AddrMap(af AddressFilter) (map[netlink.Link][]netlink.Addr, error) {
	addrs := make(map[netlink.Link][]netlink.Addr)
	addIPv6Addrs(addrs, af)
	return addrs, nil
}

func ipv6AddrMapOVN(af AddressFilter) (map[netlink.Link][]netlink.Addr, error) {
	addrs := make(map[netlink.Link][]netlink.Addr)
	addIPv6AddrsOVN(addrs, af)
	return addrs, nil
}
func ipv6RouteMapOVN(rf RouteFilter) (map[int][]netlink.Route, error) {
	routes := make(map[int][]netlink.Route)
	addIPv6RoutesWithOVN(routes, rf)
	return routes, nil
}

func ipv6RouteMap(rf RouteFilter) (map[int][]netlink.Route, error) {
	routes := make(map[int][]netlink.Route)
	addIPv6Routes(routes, rf)
	return routes, nil
}

func dualStackAddrMap(af AddressFilter) (map[netlink.Link][]netlink.Addr, error) {
	addrs := make(map[netlink.Link][]netlink.Addr)
	addIPv4Addrs(addrs, af)
	addIPv6Addrs(addrs, af)
	return addrs, nil
}

func ipv6AddrMapWithGlobalUnicast(af AddressFilter) (map[netlink.Link][]netlink.Addr, error) {
	addrs := make(map[netlink.Link][]netlink.Addr)
	maybeAddAddress(addrs, af, lo, "127.0.0.1/8", false)
	maybeAddAddress(addrs, af, lo, "::1/128", false)
	maybeAddAddress(addrs, af, eth0, "fe80::1234/64", false)
	maybeAddAddress(addrs, af, eth0, "fd00::5/64", false)
	maybeAddAddress(addrs, af, eth0, "fe00::5/64", false)
	maybeAddAddress(addrs, af, eth0, "2000::2/64", false)
	maybeAddAddress(addrs, af, eth1, "fd01::3/64", true)
	maybeAddAddress(addrs, af, eth1, "fd01::4/64", true)
	maybeAddAddress(addrs, af, eth1, "fd01::5/64", false)
	return addrs, nil
}

func dualStackRouteMap(rf RouteFilter) (map[int][]netlink.Route, error) {
	routes := make(map[int][]netlink.Route)
	addIPv4Routes(routes, rf)
	addIPv6Routes(routes, rf)
	return routes, nil
}

func ipv6RouteMapWithGwSet(rf RouteFilter) (map[int][]netlink.Route, error) {
	routes := make(map[int][]netlink.Route)
	maybeAddRoute(routes, rf, eth0, "", false, 100, "fe00::1")
	maybeAddRoute(routes, rf, eth0, "fd00::/64", false, 100, "")
	maybeAddRoute(routes, rf, eth0, "fd02::/64", false, 100, "")
	maybeAddRoute(routes, rf, eth1, "fd01::/64", false, 100, "")
	return routes, nil
}

func overlappingIpv6AddrMap(af AddressFilter) (map[netlink.Link][]netlink.Addr, error) {
	addrs := make(map[netlink.Link][]netlink.Addr)
	addOverlappingIPv6Addrs(addrs, af)
	return addrs, nil
}

func overlappingIpv6RouteMap(rf RouteFilter) (map[int][]netlink.Route, error) {
	routes := make(map[int][]netlink.Route)
	addOverlappingIPv6Routes(routes, rf)
	return routes, nil
}

func overlappingDualStackAddrMap(af AddressFilter) (map[netlink.Link][]netlink.Addr, error) {
	addrs := make(map[netlink.Link][]netlink.Addr)
	addIPv4Addrs(addrs, af)
	addOverlappingIPv6Addrs(addrs, af)
	return addrs, nil
}

func overlappingDualStackRouteMap(rf RouteFilter) (map[int][]netlink.Route, error) {
	routes := make(map[int][]netlink.Route)
	addIPv4Routes(routes, rf)
	addOverlappingIPv6Routes(routes, rf)
	return routes, nil
}

func multipleDefaultRouteMap(rf RouteFilter) (map[int][]netlink.Route, error) {
	routes := make(map[int][]netlink.Route)
	addMultipleDefaultRoutes(routes, rf)
	return routes, nil
}

func multipleDefaultRouteMapReversePriority(rf RouteFilter) (map[int][]netlink.Route, error) {
	routes := make(map[int][]netlink.Route)
	addMultipleDefaultRoutesReversePriority(routes, rf)
	return routes, nil
}

func ipv4DummyAddrMap(af AddressFilter) (map[netlink.Link][]netlink.Addr, error) {
	addrs := make(map[netlink.Link][]netlink.Addr)
	addIPv4Addrs(addrs, af)
	addDummyAddrs(addrs, af)
	return addrs, nil
}

func multipleDefaultRouteMapSamePriority(rf RouteFilter) (map[int][]netlink.Route, error) {
	routes := make(map[int][]netlink.Route)
	addMultipleDefaultRoutesSamePriority(routes, rf)
	return routes, nil
}

var _ = Describe("addresses", func() {
	It("matches an IPv4 VIP on the primary interface", func() {
		addrs, err := addressesRoutingInternal(
			[]net.IP{net.ParseIP("10.0.0.2")},
			ValidNodeAddress,
			ipv4AddrMap,
			ipv4RouteMap,
			false,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("10.0.0.5")}))
	})

	It("matches an IPv4 VIP on the secondary interface", func() {
		addrs, err := addressesRoutingInternal(
			[]net.IP{net.ParseIP("192.168.1.99")},
			ValidNodeAddress,
			ipv4AddrMap,
			ipv4RouteMap,
			false,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("192.168.1.2")}))
	})

	It("matches an IPv4 VIP when the default route is on another interface", func() {
		addrs, err := addressesRoutingInternal(
			[]net.IP{net.ParseIP("10.0.0.2")},
			ValidNodeAddress,
			ipv4AddrMap,
			ipv4RouteMapDefaultEth1,
			false,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("10.0.0.5")}))
	})

	It("matches an IPv6 VIP on the primary interface", func() {
		addrs, err := addressesRoutingInternal(
			[]net.IP{net.ParseIP("fd00::2")},
			ValidNodeAddress,
			ipv6AddrMap,
			ipv6RouteMap,
			false,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("fd00::5")}))
	})

	It("matches an IPv6 VIP on an interface with temporary IPs", func() {
		addrs, err := addressesRoutingInternal(
			[]net.IP{net.ParseIP("fd01::2")},
			ValidNodeAddress,
			ipv6AddrMap,
			ipv6RouteMap,
			false,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("fd01::5")}))
	})

	It("matches an IPv4 VIP on a dual-stack interface", func() {
		addrs, err := addressesRoutingInternal(
			[]net.IP{net.ParseIP("10.0.0.2")},
			ValidNodeAddress,
			dualStackAddrMap,
			dualStackRouteMap,
			false,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("10.0.0.5"), net.ParseIP("fd00::5")}))
	})

	It("matches an IPv6 VIP on a dual-stack interface", func() {
		addrs, err := addressesRoutingInternal(
			[]net.IP{net.ParseIP("fd01::2")},
			ValidNodeAddress,
			dualStackAddrMap,
			dualStackRouteMap,
			true,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("fd01::5"), net.ParseIP("192.168.1.2")}))
	})

	It("finds an interface with a default route in an IPv4 cluster", func() {
		addrs, err := addressesDefaultInternal(
			false,
			ValidNodeAddress,
			ipv4AddrMap,
			ipv4RouteMap,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("10.0.0.5")}))
	})

	It("finds an interface with a default route when that's not the first interface", func() {
		addrs, err := addressesDefaultInternal(
			false,
			ValidNodeAddress,
			ipv4AddrMap,
			ipv4RouteMapDefaultEth1,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("192.168.1.2")}))
	})

	It("finds an interface with a default route in an IPv6 cluster", func() {
		addrs, err := addressesDefaultInternal(
			false,
			ValidNodeAddress,
			ipv6AddrMap,
			ipv6RouteMap,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("fd00::5")}))
	})

	It("finds an interface with a default route in ovn in an IPv6 cluster", func() {

		By("Verify ovn ip will be chosen without filter")
		addrs, err := addressesDefaultInternal(
			true,
			ValidNodeAddress,
			ipv6AddrMapOVN,
			ipv6RouteMapOVN,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("fd69::2")}))

		By("Verify ovn ip will not be chosen with filter")
		addrs, err = addressesDefaultInternal(
			true,
			ValidOVNNodeAddress,
			ipv6AddrMapOVN,
			ipv6RouteMapOVN,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{}))

	})

	It("finds an interface with a default route in a dual-stack cluster", func() {
		addrs, err := addressesDefaultInternal(
			false,
			ValidNodeAddress,
			dualStackAddrMap,
			dualStackRouteMap,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("10.0.0.5"), net.ParseIP("fd00::5")}))
	})

	It("prefers an IPv6 address in a dual-stack cluster when using --prefer-ipv6", func() {
		addrs, err := addressesDefaultInternal(
			true,
			ValidNodeAddress,
			dualStackAddrMap,
			dualStackRouteMap,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("fd00::5"), net.ParseIP("10.0.0.5")}))
	})

	It("prefers an IPv6 public unicast address over a private unicast address", func() {
		addrs, err := addressesDefaultInternal(
			true,
			ValidNodeAddress,
			ipv6AddrMapWithGlobalUnicast,
			ipv6RouteMap,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("2000::2")}))
	})

	It("prefers the IPv6 address that matches the default route gw", func() {
		addrs, err := addressesDefaultInternal(
			true,
			ValidNodeAddress,
			ipv6AddrMapWithGlobalUnicast,
			ipv6RouteMapWithGwSet,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("fe00::5")}))
	})

	It("overlapping IPV6 subnets: matches an IPv6 VIP on the primary interface", func() {
		addrs, err := addressesRoutingInternal(
			[]net.IP{net.ParseIP("fd00::f02")},
			ValidNodeAddress,
			overlappingIpv6AddrMap,
			overlappingIpv6RouteMap,
			false,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("fd00::f05")}))
	})

	It("overlapping IPV6 subnets: matches an IPv6 VIP on an interface with temporary IPs", func() {
		addrs, err := addressesRoutingInternal(
			[]net.IP{net.ParseIP("fd00::2")},
			ValidNodeAddress,
			overlappingIpv6AddrMap,
			overlappingIpv6RouteMap,
			false,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("fd00::5")}))
	})

	It("overlapping IPV6 subnets: matches an IPv4 VIP on a dual-stack interface", func() {
		addrs, err := addressesRoutingInternal(
			[]net.IP{net.ParseIP("10.0.0.2")},
			ValidNodeAddress,
			overlappingDualStackAddrMap,
			overlappingDualStackRouteMap,
			false,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("10.0.0.5"), net.ParseIP("fd00::f05")}))
	})

	It("overlapping IPV6 subnets: matches an IPv6 VIP on a dual-stack interface", func() {
		addrs, err := addressesRoutingInternal(
			[]net.IP{net.ParseIP("fd00::2")},
			ValidNodeAddress,
			overlappingDualStackAddrMap,
			overlappingDualStackRouteMap,
			true,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("fd00::5"), net.ParseIP("192.168.1.2")}))
	})

	It("overlapping IPV6 subnets: finds an interface with a default route in an IPv6 cluster", func() {
		addrs, err := addressesDefaultInternal(
			false,
			ValidNodeAddress,
			overlappingIpv6AddrMap,
			overlappingIpv6RouteMap,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("fd00::f05")}))
	})

	It("overlapping IPV6 subnets: finds an interface with a default route in a dual-stack cluster", func() {
		addrs, err := addressesDefaultInternal(
			false,
			ValidNodeAddress,
			overlappingDualStackAddrMap,
			overlappingDualStackRouteMap,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("10.0.0.5"), net.ParseIP("fd00::f05")}))
	})

	It("handles multiple default routes consistently", func() {
		addrs, err := addressesDefaultInternal(
			false,
			ValidNodeAddress,
			ipv4AddrMap,
			multipleDefaultRouteMap,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("10.0.0.5")}))
	})

	It("handles multiple default routes consistently opposite priority", func() {
		addrs, err := addressesDefaultInternal(
			false,
			ValidNodeAddress,
			ipv4AddrMap,
			multipleDefaultRouteMapReversePriority,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("192.168.1.2")}))
	})

	It("handles multiple default routes with same priority consistently", func() {
		addrs, err := addressesDefaultInternal(
			false,
			ValidNodeAddress,
			ipv4DummyAddrMap,
			multipleDefaultRouteMapSamePriority,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(addrs).To(Equal([]net.IP{net.ParseIP("10.0.0.5")}))
	})
})

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Addresses tests")
}
