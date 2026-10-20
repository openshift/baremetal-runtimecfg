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
	"golang.org/x/sys/unix"
)

type fakeVIPState struct {
	// configured maps VIP string -> present
	configured map[string]bool
	// reAdd makes remove() leave the VIP configured, mimicking a
	// still-running keepalived in MASTER state that re-adds monitored
	// addresses as soon as they are deleted.
	reAdd bool
	// resignAfterLists empties configured after this many list calls,
	// mimicking keepalived resigning cleanly after the stop request.
	resignAfterLists int
	removeCalls      int
	listCalls        int
	listErr          error
	removeErr        error
}

func (f *fakeVIPState) list(vips []net.IP) (map[netlink.Link][]netlink.Addr, error) {
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.resignAfterLists > 0 && f.listCalls > f.resignAfterLists {
		f.configured = map[string]bool{}
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
	if f.removeErr != nil {
		return f.removeErr
	}
	f.configured[addr.IP.String()] = f.reAdd
	return nil
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

var _ = Describe("hostStateIsStopping", func() {
	exitErr := errors.New("exit status 1")

	It("reports stopping for 'stopping' output even with a non-zero exit", func() {
		// systemctl is-system-running exits non-zero for every state
		// except "running".
		Expect(hostStateIsStopping([]byte("stopping\n"), exitErr)).To(BeTrue())
	})

	It("reports not stopping for 'running'", func() {
		Expect(hostStateIsStopping([]byte("running\n"), nil)).To(BeFalse())
	})

	It("reports not stopping for 'degraded'", func() {
		Expect(hostStateIsStopping([]byte("degraded\n"), exitErr)).To(BeFalse())
	})

	It("reports not stopping when the state cannot be determined", func() {
		Expect(hostStateIsStopping(nil, exitErr)).To(BeFalse())
		Expect(hostStateIsStopping([]byte(""), nil)).To(BeFalse())
	})
})

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
		// keepalived resigns cleanly right after the stop message.
		fake.resignAfterLists = 1
		handleShutdownResign(conn, vips)
		Expect(conn.String()).To(Equal("stop\n"))
		Expect(fake.removeCalls).To(BeZero())
	})

	It("force-removes the VIPs and exits early once they stay gone", func() {
		fake.configured["192.0.2.5"] = true
		fake.configured["fd00::5"] = true
		start := time.Now()
		handleShutdownResign(conn, vips)
		Expect(conn.String()).To(Equal("stop\n"))
		Expect(fake.removeCalls).To(Equal(2))
		Expect(fake.configured["192.0.2.5"]).To(BeFalse())
		Expect(fake.configured["fd00::5"]).To(BeFalse())
		// The quiet-poll early exit must fire well before the full
		// stop-wait + fight window elapses.
		Expect(time.Since(start)).To(BeNumerically("<", resignStopWait+resignFightDuration))
	})

	It("keeps removing while keepalived re-adds the VIP and never claims success", func() {
		fake.configured["192.0.2.5"] = true
		fake.reAdd = true
		handleShutdownResign(conn, vips)
		// One removal per poll for the duration of the fight window.
		Expect(fake.removeCalls).To(BeNumerically(">=", int(resignFightDuration/resignPollInterval)-1))
	})

	It("does not declare success while removal keeps failing", func() {
		fake.configured["192.0.2.5"] = true
		fake.removeErr = errors.New("operation not permitted")
		handleShutdownResign(conn, vips)
		// The VIP stays configured, so every fight poll must retry until
		// the window elapses instead of exiting via the quiet-poll path.
		Expect(fake.removeCalls).To(BeNumerically(">=", int(resignFightDuration/resignPollInterval)-1))
		Expect(fake.configured["192.0.2.5"]).To(BeTrue())
	})

	It("treats an address already removed by keepalived as removed", func() {
		fake.configured["192.0.2.5"] = true
		firstRemove := true
		removeVIPFn = func(l netlink.Link, a netlink.Addr) error {
			fake.removeCalls++
			if firstRemove {
				firstRemove = false
				// keepalived deleted it between our list and remove.
				fake.configured[a.IP.String()] = false
				return unix.EADDRNOTAVAIL
			}
			return nil
		}
		start := time.Now()
		handleShutdownResign(conn, vips)
		Expect(fake.removeCalls).To(Equal(1))
		Expect(time.Since(start)).To(BeNumerically("<", resignStopWait+resignFightDuration))
	})

	It("proceeds with the resign when the initial listing fails", func() {
		fake.listErr = errors.New("netlink down")
		handleShutdownResign(conn, vips)
		// Unknown address state during a confirmed host shutdown must
		// fail toward resigning: the stop request is still sent and the
		// force-removal phase still polls until the window closes.
		Expect(conn.String()).To(Equal("stop\n"))
	})

	It("falls back to force-removal when the stop request cannot be sent", func() {
		fake.configured["192.0.2.5"] = true
		listCallsBefore := 0
		handleShutdownResign(failingWriter{}, vips)
		Expect(fake.removeCalls).To(BeNumerically(">", listCallsBefore))
		Expect(fake.configured["192.0.2.5"]).To(BeFalse())
	})
})
