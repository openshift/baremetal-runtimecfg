package main

import (
	"net"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestNodeIP(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node IP tests")
}

var _ = Describe("vipsAreDualStack", func() {
	It("returns false for empty VIPs", func() {
		Expect(vipsAreDualStack([]net.IP{})).To(BeFalse())
	})

	It("returns false for single IPv4 VIP", func() {
		Expect(vipsAreDualStack([]net.IP{net.ParseIP("10.0.0.1")})).To(BeFalse())
	})

	It("returns false for single IPv6 VIP", func() {
		Expect(vipsAreDualStack([]net.IP{net.ParseIP("fd00::1")})).To(BeFalse())
	})

	It("returns false for multiple IPv4 VIPs", func() {
		vips := []net.IP{net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2")}
		Expect(vipsAreDualStack(vips)).To(BeFalse())
	})

	It("returns false for multiple IPv6 VIPs", func() {
		vips := []net.IP{net.ParseIP("fd00::1"), net.ParseIP("fd00::2")}
		Expect(vipsAreDualStack(vips)).To(BeFalse())
	})

	It("returns true for dual-stack VIPs (IPv4 + IPv6)", func() {
		vips := []net.IP{net.ParseIP("10.0.0.1"), net.ParseIP("fd00::1")}
		Expect(vipsAreDualStack(vips)).To(BeTrue())
	})

	It("returns true for dual-stack VIPs with multiple of each family", func() {
		vips := []net.IP{
			net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"),
			net.ParseIP("fd00::1"), net.ParseIP("fd00::2"),
		}
		Expect(vipsAreDualStack(vips)).To(BeTrue())
	})

	It("returns true for dual-stack VIPs (IPv6 first)", func() {
		vips := []net.IP{net.ParseIP("2620:52:0:2ebb::1"), net.ParseIP("10.46.187.1")}
		Expect(vipsAreDualStack(vips)).To(BeTrue())
	})
})

var _ = Describe("hasBothIPFamilies", func() {
	It("returns false for empty list", func() {
		Expect(hasBothIPFamilies([]net.IP{})).To(BeFalse())
	})

	It("returns false for IPv4 only", func() {
		Expect(hasBothIPFamilies([]net.IP{net.ParseIP("10.0.0.5")})).To(BeFalse())
	})

	It("returns false for IPv6 only", func() {
		Expect(hasBothIPFamilies([]net.IP{net.ParseIP("fd00::5")})).To(BeFalse())
	})

	It("returns true for both families", func() {
		addrs := []net.IP{net.ParseIP("10.0.0.5"), net.ParseIP("fd00::5")}
		Expect(hasBothIPFamilies(addrs)).To(BeTrue())
	})

	It("returns true for both families (IPv6 first)", func() {
		addrs := []net.IP{net.ParseIP("fd00::5"), net.ParseIP("10.0.0.5")}
		Expect(hasBothIPFamilies(addrs)).To(BeTrue())
	})
})

var _ = Describe("shouldKeepWaitingForDualStack", func() {
	chosen := []net.IP{net.ParseIP("fd00::5")}

	It("initializes wait timer on first call and returns true", func() {
		var waitStart time.Time
		result := shouldKeepWaitingForDualStack(&waitStart, chosen)
		Expect(result).To(BeTrue())
		Expect(waitStart.IsZero()).To(BeFalse())
	})

	It("keeps waiting within timeout", func() {
		waitStart := time.Now()
		result := shouldKeepWaitingForDualStack(&waitStart, chosen)
		Expect(result).To(BeTrue())
	})

	It("stops waiting after timeout", func() {
		waitStart := time.Now().Add(-time.Duration(maxDualStackWaitSeconds+1) * time.Second)
		result := shouldKeepWaitingForDualStack(&waitStart, chosen)
		Expect(result).To(BeFalse())
	})
})
