package utils

import (
	"net"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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
		//
		// Due to the fact how net.IPv4() initialization is implemented and the fact that it
		// always adds the v4InV6Prefix header in a binary form, those addresses look like IPv4
		// and in most cases behave like such.
		//
		// The tests here are saying that "::ffff:192.168.0.14" is IPv4 and not an IPv6 address
		// whenever fed into our own IsIPv4 and IsIPv6 functions. For a proper answer returning
		// IPv6 for those, we have IsNetIPv6 function which looks at IP address as well as subnet
		// mask.

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

	Context("using IsNetIPv6", func() {
		It("returns false for IPv4 addresses", func() {
			addrs := []string{
				"192.168.0.1/24",
				"192.168.0.1/32",
				"192.168.0.1/0",
				"0.0.0.0/0",
			}
			for _, addr := range addrs {
				_, addr, _ := net.ParseCIDR(addr)
				res := IsNetIPv6(*addr)
				Expect(res).To(Equal(false))
			}
		})
		It("returns true for IPv6 address", func() {
			addrs := []string{
				"fe80::84ca:4a24:ffbf:1778/64",
				"::/0",
				"::1/128",
				"::ffff:192.168.0.14/64",
				"::ffff:192.168.0.14/16",
			}
			for _, addr := range addrs {
				_, addr, _ := net.ParseCIDR(addr)
				res := IsNetIPv6(*addr)
				Expect(res).To(Equal(true))
			}
		})
		It("returns true for IPv4-mapped-to-IPv6 address", func() {
			// Because of non-obvious behaviour of net library when it comes to
			// IPv4-mapped-to-IPv6 addresses (more in https://github.com/golang/go/issues/51906)
			// we are adding tests where we explicitly create net.IPNet struct from a binary
			// input.
			addrs := []net.IPNet{
				{
					IP:   net.IP([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255, 255, 192, 168, 1, 160}),
					Mask: net.IPMask([]byte{255, 255, 255, 255, 255, 255, 255, 255, 0, 0, 0, 0, 0, 0, 0, 0}),
				},
				{
					IP:   net.IP([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255, 255, 192, 168, 1, 160}),
					Mask: net.IPMask([]byte{255, 255, 255, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}),
				},
			}

			for _, addr := range addrs {
				res := IsNetIPv6(addr)
				Expect(res).To(Equal(true))
			}
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
