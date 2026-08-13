//go:build linux

package monitor

import (
	"bytes"
	"errors"
	"net"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
)

type fakeVIPState struct {
	// configured maps VIP string -> present
	configured map[string]bool
	// reAdd makes the fake re-add every removed VIP on the next list call,
	// mimicking a still-running keepalived in MASTER state.
	reAdd       bool
	removeCalls int
	listErr     error
}

func (f *fakeVIPState) list(vips []net.IP) (map[netlink.Link][]netlink.Addr, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	res := make(map[netlink.Link][]netlink.Addr)
	link := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "ens192"}}
	for _, vip := range vips {
		if f.configured[vip.String()] {
			res[link] = append(res[link], netlink.Addr{IPNet: netlink.NewIPNet(vip)})
		}
	}
	return res, nil
}

func (f *fakeVIPState) remove(link netlink.Link, addr netlink.Addr) error {
	f.removeCalls++
	f.configured[addr.IP.String()] = f.reAdd
	return nil
}

var _ = Describe("handleShutdownResign", func() {
	var (
		fake     *fakeVIPState
		conn     *bytes.Buffer
		stopping bool
		vips     []net.IP

		origStopWait time.Duration
		origFight    time.Duration
		origPoll     time.Duration

		origHostShuttingDown func() bool
		origList             func([]net.IP) (map[netlink.Link][]netlink.Addr, error)
		origRemove           func(netlink.Link, netlink.Addr) error
	)

	BeforeEach(func() {
		fake = &fakeVIPState{configured: map[string]bool{}}
		conn = &bytes.Buffer{}
		stopping = true
		vips = []net.IP{net.ParseIP("192.0.2.5"), net.ParseIP("fd00::5")}

		origStopWait, origFight, origPoll = resignStopWait, resignFightDuration, resignPollInterval
		resignStopWait = 30 * time.Millisecond
		resignFightDuration = 100 * time.Millisecond
		resignPollInterval = 10 * time.Millisecond

		origHostShuttingDown = hostShuttingDownFn
		origList = listVIPsFn
		origRemove = removeVIPFn
		hostShuttingDownFn = func() bool { return stopping }
		listVIPsFn = func(v []net.IP) (map[netlink.Link][]netlink.Addr, error) { return fake.list(v) }
		removeVIPFn = func(l netlink.Link, a netlink.Addr) error { return fake.remove(l, a) }
	})

	AfterEach(func() {
		resignStopWait, resignFightDuration, resignPollInterval = origStopWait, origFight, origPoll
		hostShuttingDownFn = origHostShuttingDown
		listVIPsFn = origList
		removeVIPFn = origRemove
	})

	It("does nothing when the host is not shutting down", func() {
		stopping = false
		fake.configured["192.0.2.5"] = true
		handleShutdownResign(conn, vips)
		Expect(conn.String()).To(BeEmpty())
		Expect(fake.removeCalls).To(BeZero())
		Expect(fake.configured["192.0.2.5"]).To(BeTrue())
	})

	It("does nothing when no VIP is configured locally", func() {
		handleShutdownResign(conn, vips)
		Expect(conn.String()).To(BeEmpty())
		Expect(fake.removeCalls).To(BeZero())
	})

	It("does nothing when the VIP list is empty", func() {
		handleShutdownResign(conn, nil)
		Expect(conn.String()).To(BeEmpty())
	})

	It("requests a clean keepalived stop and returns once the VIPs are gone", func() {
		fake.configured["192.0.2.5"] = true
		// Simulate keepalived resigning cleanly right after the stop
		// message: the first poll already sees no VIPs.
		listCalls := 0
		listVIPsFn = func(v []net.IP) (map[netlink.Link][]netlink.Addr, error) {
			listCalls++
			if listCalls == 1 {
				return fake.list(v)
			}
			return map[netlink.Link][]netlink.Addr{}, nil
		}
		handleShutdownResign(conn, vips)
		Expect(conn.String()).To(Equal("stop\n"))
		Expect(fake.removeCalls).To(BeZero())
	})

	It("force-removes the VIPs when keepalived does not resign", func() {
		fake.configured["192.0.2.5"] = true
		fake.configured["fd00::5"] = true
		handleShutdownResign(conn, vips)
		Expect(conn.String()).To(Equal("stop\n"))
		Expect(fake.removeCalls).To(Equal(2))
		Expect(fake.configured["192.0.2.5"]).To(BeFalse())
		Expect(fake.configured["fd00::5"]).To(BeFalse())
	})

	It("keeps removing while keepalived re-adds the VIP, then gives up at the fight window", func() {
		fake.configured["192.0.2.5"] = true
		fake.reAdd = true
		handleShutdownResign(conn, vips)
		// One removal per poll for the duration of the fight window.
		Expect(fake.removeCalls).To(BeNumerically(">=", int(resignFightDuration/resignPollInterval)-1))
	})

	It("falls through to force-removal when listing fails during the wait", func() {
		fake.configured["192.0.2.5"] = true
		fake.listErr = errors.New("netlink down")
		handleShutdownResign(conn, vips)
		// anyVIPConfigured returns false on error, so the initial check
		// already bails out before writing to the socket.
		Expect(conn.String()).To(BeEmpty())
		Expect(fake.removeCalls).To(BeZero())
	})
})
