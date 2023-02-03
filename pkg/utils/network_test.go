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
