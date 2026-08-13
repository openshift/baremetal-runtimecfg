package main

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/openshift/baremetal-runtimecfg/pkg/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestNodeIP(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node IP tests")
}

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

	It("returns false for two IPv4 addresses", func() {
		addrs := []net.IP{net.ParseIP("10.0.0.5"), net.ParseIP("10.0.0.6")}
		Expect(hasBothIPFamilies(addrs)).To(BeFalse())
	})

	It("returns false for two IPv6 addresses", func() {
		addrs := []net.IP{net.ParseIP("fd00::5"), net.ParseIP("fd00::6")}
		Expect(hasBothIPFamilies(addrs)).To(BeFalse())
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

var _ = Describe("needsDualStackWait", func() {
	ipv4 := net.ParseIP("10.0.0.5")
	ipv6 := net.ParseIP("fd00::5")

	It("keeps waiting when a dual-stack cluster has only IPv6", func() {
		Expect(needsDualStackWait(true, true, []net.IP{ipv6})).To(BeTrue())
	})

	It("keeps waiting when a dual-stack cluster has only IPv4", func() {
		Expect(needsDualStackWait(true, true, []net.IP{ipv4})).To(BeTrue())
	})

	It("keeps waiting when nothing has been found yet (empty/nil)", func() {
		Expect(needsDualStackWait(true, true, []net.IP{})).To(BeTrue())
		Expect(needsDualStackWait(true, true, nil)).To(BeTrue())
	})

	It("stops waiting once both families are present", func() {
		Expect(needsDualStackWait(true, true, []net.IP{ipv4, ipv6})).To(BeFalse())
	})

	It("does not wait when the cluster is not dual-stack", func() {
		Expect(needsDualStackWait(false, true, []net.IP{ipv6})).To(BeFalse())
	})

	It("does not wait when retry is disabled", func() {
		Expect(needsDualStackWait(true, false, []net.IP{ipv6})).To(BeFalse())
	})
})

var _ = Describe("logDualStackWait", func() {
	chosen := []net.IP{net.ParseIP("fd00::5")}

	It("initializes both timestamps and logs on the first call", func() {
		var waitStart, lastHeartbeat time.Time
		logDualStackWait(&waitStart, &lastHeartbeat, chosen)
		Expect(waitStart.IsZero()).To(BeFalse())
		Expect(lastHeartbeat.IsZero()).To(BeFalse())
		Expect(lastHeartbeat).To(Equal(waitStart))
	})

	It("preserves the wait start timestamp on subsequent calls", func() {
		waitStart := time.Now().Add(-time.Minute)
		lastHeartbeat := time.Now()
		original := waitStart
		logDualStackWait(&waitStart, &lastHeartbeat, chosen)
		Expect(waitStart).To(Equal(original))
	})

	It("does not emit a heartbeat before the interval elapses", func() {
		waitStart := time.Now().Add(-time.Minute)
		lastHeartbeat := time.Now().Add(-5 * time.Second)
		original := lastHeartbeat
		logDualStackWait(&waitStart, &lastHeartbeat, chosen)
		// lastHeartbeat is only advanced when a heartbeat is emitted.
		Expect(lastHeartbeat).To(Equal(original))
	})

	It("emits a heartbeat once the interval has elapsed", func() {
		waitStart := time.Now().Add(-time.Minute)
		lastHeartbeat := time.Now().Add(-(dualStackHeartbeatInterval + time.Second))
		original := lastHeartbeat
		logDualStackWait(&waitStart, &lastHeartbeat, chosen)
		Expect(lastHeartbeat.After(original)).To(BeTrue())
	})
})

// fakeDiscoverer feeds scripted address-discovery results to
// getSuitableIPsInternal and records the sleep durations it requests, so the
// retry/dual-stack wait logic can be tested without a live network stack.
type fakeDiscoverer struct {
	routingResults [][]net.IP
	defaultResults [][]net.IP
	usableErr      error
	routingCalls   int
	defaultCalls   int
	sleeps         []time.Duration
}

func pickResult(results [][]net.IP, i int) []net.IP {
	if len(results) == 0 {
		return nil
	}
	if i >= len(results) {
		i = len(results) - 1
	}
	return results[i]
}

func (f *fakeDiscoverer) discoverer() ipDiscoverer {
	return ipDiscoverer{
		routing: func(vips []net.IP, af utils.AddressFilter, preferIPv6 bool) ([]net.IP, error) {
			r := pickResult(f.routingResults, f.routingCalls)
			f.routingCalls++
			return r, nil
		},
		byDefault: func(preferIPv6 bool, af utils.AddressFilter) ([]net.IP, error) {
			r := pickResult(f.defaultResults, f.defaultCalls)
			f.defaultCalls++
			return r, nil
		},
		usable: func(chosen []net.IP) error { return f.usableErr },
		sleep: func(d time.Duration) {
			f.sleeps = append(f.sleeps, d)
			// Safety valve: turn an accidental infinite loop into a test
			// failure instead of hanging the suite.
			if len(f.sleeps) > 100000 {
				panic("getSuitableIPsInternal did not terminate")
			}
		},
	}
}

var _ = Describe("getSuitableIPsInternal", func() {
	ipv4 := net.ParseIP("10.0.0.5")
	ipv6 := net.ParseIP("fd00::5")

	Context("backoff timer during the dual-stack wait (B1 regression)", func() {
		It("keeps the retry sleep bounded and positive even after many wait iterations", func() {
			// Force >63 dual-stack wait iterations (whose continue statements
			// bypass the backoff clamp) followed by a no-address iteration that
			// reaches the bottom backoff sleep. Before the fix, timerLoop
			// doubled unclamped on every iteration and overflowed to a
			// negative/zero value around iteration 63, making the bottom
			// time.Sleep non-positive (a CPU busy-loop). After the fix it stays
			// clamped and positive.
			const waitRounds = 65
			defaults := make([][]net.IP, 0, waitRounds+2)
			for i := 0; i < waitRounds; i++ {
				defaults = append(defaults, []net.IP{ipv6}) // single family -> keep waiting
			}
			defaults = append(defaults, []net.IP{})           // nothing found -> reach bottom backoff
			defaults = append(defaults, []net.IP{ipv4, ipv6}) // both families -> return

			f := &fakeDiscoverer{defaultResults: defaults}
			chosen, _, err := getSuitableIPsInternal(true, nil, false, ovn, true, f.discoverer())

			Expect(err).ToNot(HaveOccurred())
			Expect(hasBothIPFamilies(chosen)).To(BeTrue())

			maxSleep := time.Duration(maxSecondsToSuitableIPsLoop) * time.Second
			sawBottomBackoff := false
			for i, d := range f.sleeps {
				Expect(d > 0).To(BeTrue(), "sleep %d must stay positive (backoff must not overflow), got %s", i, d)
				Expect(d <= maxSleep).To(BeTrue(), "sleep %d must be clamped to <= %s, got %s", i, maxSleep, d)
				if d > time.Second {
					sawBottomBackoff = true
				}
			}
			// Ensure the test actually exercised the bottom backoff path (a
			// sleep longer than the fixed 1s dual-stack wait).
			Expect(sawBottomBackoff).To(BeTrue())
		})
	})

	Context("dual-stack default-route path", func() {
		It("waits while a single family is present, then returns once both appear", func() {
			const singleFamilyRounds = 5
			defaults := make([][]net.IP, 0, singleFamilyRounds+1)
			for i := 0; i < singleFamilyRounds; i++ {
				defaults = append(defaults, []net.IP{ipv6})
			}
			defaults = append(defaults, []net.IP{ipv4, ipv6})

			f := &fakeDiscoverer{defaultResults: defaults}
			chosen, matchesVips, err := getSuitableIPsInternal(true, nil, false, ovn, true, f.discoverer())

			Expect(err).ToNot(HaveOccurred())
			Expect(matchesVips).To(BeFalse())
			Expect(hasBothIPFamilies(chosen)).To(BeTrue())
			Expect(f.defaultCalls).To(Equal(singleFamilyRounds + 1))
			// Every wait iteration sleeps exactly one second.
			Expect(f.sleeps).To(HaveLen(singleFamilyRounds))
			for _, d := range f.sleeps {
				Expect(d).To(Equal(time.Second))
			}
		})

		It("does not wait for a one-shot (no retry) caller, even on dual-stack", func() {
			f := &fakeDiscoverer{defaultResults: [][]net.IP{{ipv6}}}
			chosen, _, err := getSuitableIPsInternal(false, nil, false, ovn, true, f.discoverer())

			Expect(err).ToNot(HaveOccurred())
			Expect(chosen).To(HaveLen(1))
			Expect(chosen[0].Equal(ipv6)).To(BeTrue())
			Expect(f.defaultCalls).To(Equal(1))
			Expect(f.sleeps).To(BeEmpty())
		})

		It("returns immediately with a single family when the cluster is not dual-stack", func() {
			f := &fakeDiscoverer{defaultResults: [][]net.IP{{ipv4}}}
			chosen, _, err := getSuitableIPsInternal(true, nil, false, ovn, false, f.discoverer())

			Expect(err).ToNot(HaveOccurred())
			Expect(chosen).To(HaveLen(1))
			Expect(chosen[0].Equal(ipv4)).To(BeTrue())
			Expect(f.defaultCalls).To(Equal(1))
			Expect(f.sleeps).To(BeEmpty())
		})
	})

	Context("dual-stack VIP/routing path", func() {
		It("waits while routing yields a single family, then returns both (same interface)", func() {
			vips := []net.IP{net.ParseIP("10.0.0.2"), net.ParseIP("fd00::2")}
			const singleFamilyRounds = 3
			routing := make([][]net.IP, 0, singleFamilyRounds+1)
			for i := 0; i < singleFamilyRounds; i++ {
				routing = append(routing, []net.IP{ipv4})
			}
			routing = append(routing, []net.IP{ipv4, ipv6})

			f := &fakeDiscoverer{routingResults: routing}
			chosen, matchesVips, err := getSuitableIPsInternal(true, vips, false, ovn, true, f.discoverer())

			Expect(err).ToNot(HaveOccurred())
			Expect(matchesVips).To(BeTrue())
			Expect(hasBothIPFamilies(chosen)).To(BeTrue())
			Expect(f.routingCalls).To(Equal(singleFamilyRounds + 1))
			Expect(f.defaultCalls).To(Equal(0))
		})
	})
})

// fakeProbe records the addresses checkAddressUsableInternal probes and returns
// scripted errors, so usability checking can be tested without real sockets.
type fakeProbe struct {
	calls  []string
	errFor map[string]error
}

func (p *fakeProbe) probe(ip net.IP) error {
	p.calls = append(p.calls, ip.String())
	return p.errFor[ip.String()]
}

var _ = Describe("checkAddressUsableInternal", func() {
	ipv4 := net.ParseIP("10.0.0.5")
	ipv6 := net.ParseIP("fd00::5")

	It("does not probe when only IPv4 is present", func() {
		p := &fakeProbe{}
		Expect(checkAddressUsableInternal([]net.IP{ipv4}, p.probe)).To(Succeed())
		Expect(p.calls).To(BeEmpty())
	})

	It("probes the IPv6 address even when IPv4 comes first", func() {
		p := &fakeProbe{}
		Expect(checkAddressUsableInternal([]net.IP{ipv4, ipv6}, p.probe)).To(Succeed())
		Expect(p.calls).To(Equal([]string{ipv6.String()}))
	})

	It("returns an error when a non-first IPv6 address is unusable", func() {
		// Regression: the previous implementation only probed chosen[0], so a
		// tentative IPv6 selected as the second family (IPv4 preferred first)
		// slipped through and could be written into the kubelet config.
		p := &fakeProbe{errFor: map[string]error{ipv6.String(): errors.New("cannot assign requested address")}}
		err := checkAddressUsableInternal([]net.IP{ipv4, ipv6}, p.probe)
		Expect(err).To(HaveOccurred())
		Expect(p.calls).To(Equal([]string{ipv6.String()}))
	})

	It("returns nil when every IPv6 address is usable", func() {
		p := &fakeProbe{}
		Expect(checkAddressUsableInternal([]net.IP{ipv6, ipv4}, p.probe)).To(Succeed())
		Expect(p.calls).To(Equal([]string{ipv6.String()}))
	})
})
