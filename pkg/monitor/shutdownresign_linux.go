//go:build linux

package monitor

import (
	"io"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// Vars rather than consts so unit tests can shrink the windows.
var (
	// resignStopWait is how long we wait for keepalived to perform a clean
	// shutdown (VRRP priority-0 advertisement + VIP removal) after sending
	// "stop" over the control socket, before force-removing the VIPs.
	resignStopWait = 10 * time.Second
	// resignFightDuration bounds the force-removal phase. While keepalived
	// is still alive in MASTER state it monitors its VIPs via netlink and
	// re-adds any address deleted out from under it, so removal is retried
	// until keepalived is gone or the node has finished shutting down the
	// containers. This must stay below the pod termination grace period
	// (65s) so we never block pod shutdown.
	resignFightDuration = 30 * time.Second
	// resignPollInterval is the poll/retry cadence for both phases.
	resignPollInterval = time.Second
)

// Stubbed in unit tests.
var (
	hostShuttingDownFn = isHostShuttingDown
	listVIPsFn         = listConfiguredVIPs
	removeVIPFn        = removeVIP
)

// isHostShuttingDown reports whether the host (not just this pod) is being
// shut down or rebooted. The keepalived-monitor container mounts the host
// filesystem at /host and runs with CAP_SYS_CHROOT, so we can ask the host's
// systemd directly. Note systemctl is-system-running exits non-zero for every
// state except "running", so the output must be inspected instead of the
// error.
func isHostShuttingDown() bool {
	out, err := exec.Command("chroot", "/host", "systemctl", "is-system-running").Output()
	state := strings.TrimSpace(string(out))
	if state == "" && err != nil {
		log.WithError(err).Info("handleShutdownResign: could not determine host state")
		return false
	}
	return state == "stopping"
}

// listConfiguredVIPs returns the subset of vips currently configured on any
// interface, together with the netlink handles needed to remove them.
// Comparison is done on the parsed address, so textual differences (IPv6
// compression, case) do not matter.
func listConfiguredVIPs(vips []net.IP) (map[netlink.Link][]netlink.Addr, error) {
	nlHandle, err := netlink.NewHandle(unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	defer nlHandle.Delete()

	links, err := nlHandle.LinkList()
	if err != nil {
		return nil, err
	}

	configured := make(map[netlink.Link][]netlink.Addr)
	for _, link := range links {
		addresses, err := nlHandle.AddrList(link, netlink.FAMILY_ALL)
		if err != nil {
			return nil, err
		}
		for _, address := range addresses {
			for _, vip := range vips {
				if address.IP.Equal(vip) {
					configured[link] = append(configured[link], address)
					break
				}
			}
		}
	}
	return configured, nil
}

func removeVIP(link netlink.Link, addr netlink.Addr) error {
	return netlink.AddrDel(link, &addr)
}

func anyVIPConfigured(vips []net.IP) bool {
	configured, err := listVIPsFn(vips)
	if err != nil {
		log.WithError(err).Error("handleShutdownResign: failed to list addresses")
		return false
	}
	return len(configured) > 0
}

func forceRemoveVIPs(vips []net.IP) bool {
	configured, err := listVIPsFn(vips)
	if err != nil {
		log.WithError(err).Error("handleShutdownResign: failed to list addresses")
		return false
	}
	removedAll := true
	for link, addrs := range configured {
		for _, addr := range addrs {
			log.WithFields(logrus.Fields{
				"address": addr.IPNet.String(),
				"link":    link.Attrs().Name,
			}).Info("handleShutdownResign: force-removing VIP")
			if err := removeVIPFn(link, addr); err != nil {
				log.WithError(err).Error("handleShutdownResign: failed to remove VIP")
				removedAll = false
			}
		}
	}
	return removedAll && len(configured) > 0
}

// handleShutdownResign makes sure no VIP stays configured on this node while
// it shuts down. During "systemctl reboot" systemd tears down all container
// scopes in parallel and keepalived may be killed before it can send its VRRP
// priority-0 resign advertisement and remove the VIPs. The stale VIP then
// keeps attracting client connections which land directly on the local
// kube-apiserver - still serving its graceful drain with readyz=false - for
// up to ~70 seconds (OCPBUGS-109633).
//
// Called when dynkeepalived receives SIGTERM/SIGINT. It only acts when the
// host itself is shutting down: an ordinary pod restart (MachineConfig
// rollout, liveness restart, upgrade) must not touch the VIPs of a healthy
// node.
func handleShutdownResign(conn io.Writer, vips []net.IP) {
	if len(vips) == 0 {
		return
	}
	if !hostShuttingDownFn() {
		log.Info("handleShutdownResign: host is not shutting down, leaving VIPs alone")
		return
	}
	if !anyVIPConfigured(vips) {
		log.Info("handleShutdownResign: no VIP configured on this node, nothing to do")
		return
	}

	// Ask the keepalived container to stop keepalived cleanly: on SIGTERM
	// keepalived sends a priority-0 advertisement (instant failover on the
	// peers) and removes the VIPs. Wrappers that do not implement "stop"
	// simply ignore the message and we fall through to force-removal.
	if _, err := conn.Write([]byte("stop\n")); err != nil {
		log.WithError(err).Error("handleShutdownResign: failed to write stop to keepalived control socket")
	} else {
		log.Info("handleShutdownResign: requested keepalived stop, waiting for clean VIP resign")
		for deadline := time.Now().Add(resignStopWait); time.Now().Before(deadline); {
			if !anyVIPConfigured(vips) {
				log.Info("handleShutdownResign: all VIPs resigned cleanly")
				return
			}
			time.Sleep(resignPollInterval)
		}
	}

	// Last resort: remove the VIPs directly. The peers take over via the
	// VRRP master-down timeout (~3-4s). A still-running keepalived in
	// MASTER state re-adds monitored addresses, so keep removing until the
	// VIPs stay gone or the fight window closes.
	quietPolls := 0
	for deadline := time.Now().Add(resignFightDuration); time.Now().Before(deadline); {
		if forceRemoveVIPs(vips) {
			quietPolls = 0
		} else {
			quietPolls++
			if quietPolls >= 3 {
				log.Info("handleShutdownResign: VIPs stayed removed")
				return
			}
		}
		time.Sleep(resignPollInterval)
	}
	log.Warn("handleShutdownResign: fight window elapsed, VIPs may still be re-added by keepalived")
}
