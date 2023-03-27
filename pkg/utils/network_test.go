package utils

import (
	"net"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
)

var (
	sampleV4     = "192.168.1.99"
	sampleV6     = "fd00::5"
	sampleCidrV4 = "192.168.1.0/24"
	sampleCidrV6 = "fd00::/64"
)

var _ = Describe("IP stack detection", func() {
	Context("using IsIPv4Addr", func() {
		It("returns true for IPv4 address", func() {
			res := IsIPv4(net.ParseIP(sampleV4))
			Expect(res).To(Equal(true))
		})
		It("returns false for IPv6 address", func() {
			res := IsIPv4(net.ParseIP(sampleV6))
			Expect(res).To(Equal(false))
		})
		It("returns false for not an IP", func() {
			res := IsIPv4(net.ParseIP("this-is-not-an-ip"))
			Expect(res).To(Equal(false))
		})

	})

	Context("using IsIPv6Addr", func() {
		It("returns false for IPv4 address", func() {
			res := IsIPv6(net.ParseIP(sampleV4))
			Expect(res).To(Equal(false))
		})
		It("returns true for IPv6 address", func() {
			res := IsIPv6(net.ParseIP(sampleV6))
			Expect(res).To(Equal(true))
		})
		It("returns false for not an IP", func() {
			res := IsIPv6(net.ParseIP("this-is-not-an-ip"))
			Expect(res).To(Equal(false))
		})

	})

	Context("for IPv4-mapped-to-IPv6 addresses", func() {
		// (mko) This test suite is intentionally returning wrong results. This is because the
		// mapped form of those addresses is making them impossible to distinguish without
		// knowing more properties of the link.
		// Due to the fact how net.IPv4() initialization is implemented and the fact that it
		// always adds the v4InV6Prefix header in a binary form, those addresses look like IPv4
		// and in most cases behave like such.
		// The tests here are saying that "::ffff:192.168.0.14" is IPv4 and not an IPv6 address
		// whenever fed into our own IsIPv4 and IsIPv6 functions. For a proper answer returning
		// IPv6 for those, we are having IsNetlinkIPv6 function which looks at the broadcast
		// address as this one will never be present in an IPv6 address.

		It("IsIPv4 returns true for IPv4-mapped-to-IPv6 address", func() {
			res := IsIPv4(net.ParseIP("::ffff:192.168.0.14"))
			Expect(res).To(Equal(true))
		})
		It("IsIPv4 returns true for binary IPv4-mapped-to-IPv6 address", func() {
			ip := net.IP([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255, 255, 192, 168, 1, 160})
			res := IsIPv4(ip)
			Expect(res).To(Equal(true))
		})

		It("IsIPv6 returns false for IPv4-mapped-to-IPv6 address", func() {
			res := IsIPv6(net.ParseIP("::ffff:192.168.0.14"))
			Expect(res).To(Equal(false))
		})
		It("IsIPv6 returns true for binary IPv4-mapped-to-IPv6 address", func() {
			ip := net.IP([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255, 255, 192, 168, 1, 160})
			res := IsIPv6(ip)
			Expect(res).To(Equal(false))
		})
	})

	Context("using IsNetlinkIPv6", func() {
		It("returns false for golang-style IPv4 address", func() {
			addr := netlink.Addr{
				IPNet: &net.IPNet{
					IP:   net.IP([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255, 255, 192, 168, 1, 160}),
					Mask: net.IPMask([]byte{255, 255, 255, 255, 255, 255, 255, 255, 0, 0, 0, 0, 0, 0, 0, 0}),
				},
				Broadcast: net.IP([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255, 255, 192, 168, 1, 255}),
			}
			res := IsNetlinkIPv6(addr)
			Expect(res).To(Equal(false))
		})
		It("returns false for binary IPv4 address", func() {
			addr := netlink.Addr{
				IPNet: &net.IPNet{
					IP:   net.IP([]byte{192, 168, 1, 160}),
					Mask: net.IPMask([]byte{255, 255, 255, 0}),
				},
				Broadcast: net.IP([]byte{192, 168, 1, 255}),
			}
			res := IsNetlinkIPv6(addr)
			Expect(res).To(Equal(false))
		})
		It("returns true for IPv6 address", func() {
			addr := netlink.Addr{
				IPNet: &net.IPNet{
					IP:   net.IP([]byte{32, 1, 23, 17, 250, 65, 106, 10, 21, 244, 168, 151, 163, 192, 224, 9}),
					Mask: net.IPMask([]byte{255, 255, 255, 255, 255, 255, 255, 255, 0, 0, 0, 0, 0, 0, 0, 0}),
				},
				Broadcast: nil,
			}
			res := IsNetlinkIPv6(addr)
			Expect(res).To(Equal(true))
		})
		It("returns true for IPv4-mapped-to-IPv6 address", func() {
			addr := netlink.Addr{
				IPNet: &net.IPNet{
					IP:   net.IP([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255, 255, 192, 168, 1, 160}),
					Mask: net.IPMask([]byte{255, 255, 255, 255, 255, 255, 255, 255, 0, 0, 0, 0, 0, 0, 0, 0}),
				},
				Broadcast: nil,
			}
			res := IsNetlinkIPv6(addr)
			Expect(res).To(Equal(true))
		})
	})
})

var _ = Describe("SplitCIDR", func() {
	It("splits v4 correctly", func() {
		ip, mask, err := SplitCIDR(sampleCidrV4)
		Expect(err).ToNot(HaveOccurred())
		Expect(ip).To(Equal("192.168.1.0"))
		Expect(mask).To(Equal("24"))
	})
	It("splits v6 correctly", func() {
		ip, mask, err := SplitCIDR(sampleCidrV6)
		Expect(err).ToNot(HaveOccurred())
		Expect(ip).To(Equal("fd00::"))
		Expect(mask).To(Equal("64"))
	})
	It("returns error for empty", func() {
		_, _, err := SplitCIDR("")
		Expect(err).To(HaveOccurred())
	})
	It("returns error for not a CIDR", func() {
		_, _, err := SplitCIDR("chocobomb")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("IpInCidr", func() {
	Context("using v4", func() {
		It("returns true for matching IP", func() {
			res, err := IpInCidr(sampleV4, sampleCidrV4)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal(true))
		})
		It("returns false for not-matching IP", func() {
			res, err := IpInCidr(sampleV4, "1.0.0.0/16")
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal(false))
		})
	})

	Context("using v6", func() {
		It("returns true for matching IP", func() {
			res, err := IpInCidr(sampleV6, sampleCidrV6)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal(true))
		})
		It("returns false for not-matching IP", func() {
			res, err := IpInCidr(sampleV6, "2006::/64")
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal(false))
		})
	})

	Context("using mixed stack", func() {
		It("returns false for v4 address in v6 subnet", func() {
			res, err := IpInCidr(sampleV4, sampleCidrV6)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal(false))
		})
	})

	It("returns error for empty IP", func() {
		_, err := IpInCidr("", sampleCidrV6)
		Expect(err).To(HaveOccurred())
	})
	It("returns error for empty subnet", func() {
		_, err := IpInCidr(sampleV6, "")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("ConvertIpsToStrings", func() {
	var (
		sampleIpV4 = net.ParseIP(sampleV4)
		sampleIpV6 = net.ParseIP(sampleV6)
	)

	It("generates correct list of strings", func() {
		res := ConvertIpsToStrings([]net.IP{sampleIpV4, sampleIpV6})
		Expect(len(res)).To(Equal(2))
		Expect(res[0]).To(Equal(sampleV4))
		Expect(res[1]).To(Equal(sampleV6))
	})
})
