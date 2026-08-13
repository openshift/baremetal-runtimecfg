//go:build linux

package monitor

import (
	"errors"
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
	// containers. Together with resignStopWait this must stay below the
	// keepalived pod's deletionGracePeriodSeconds (65s, see the keepalived
	// pod template in machine-config-operator) so we never block pod
	// shutdown.
	resignFightDuration = 30 * time.Second
	// resignPollInterval is the poll/retry cadence for both phases.
	resignPollInterval = time.Second
	// resignQuietPolls is how many consecutive polls must find no VIP
	// before the force-removal phase declares the VIPs gone for good.
	resignQuietPolls = 3
)

// Stubbed in unit tests.
var (
	hostShuttingDownFn = isHostShuttingDown
	listVIPsFn         = listConfiguredVIPs
	removeVIPFn        = removeVIP
)

// hostStateIsStopping interprets `systemctl is-system-running` results.
// systemctl exits non-zero for every state except "running", so the output
// must be inspected instead of the error. An empty output means the state
// could not be determined; be conservative and report "not stopping" so that
// a mere pod restart can never flap the VIPs of a healthy node.
func hostStateIsStopping(out []byte, err error) bool {
	state := strings.TrimSpace(string(out))
	if state == "" {
		if err != nil {
			log.WithError(err).Warn("handleShutdownResign: could not determine host state, assuming the host is not shutting down")
		}
		return false
	}
	return state == "stopping"
}

// isHostShuttingDown reports whether the host (not just this pod) is being
// shut down or rebooted. The keepalived-monitor container mounts the host
// filesystem at /host and runs with CAP_SYS_CHROOT, so we can ask the host's
// systemd directly. The chroot binary path is pinned so a PATH change cannot
// silently disable the check.
func isHostShuttingDown() bool {
	out, err := exec.Command("/usr/sbin/chroot", "/host", "systemctl", "is-system-running").Output()
	return hostStateIsStopping(out, err)
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

// anyVIPConfigured reports whether any of the vips is configured locally.
// Errors are propagated so callers can decide how to fail: toward removal
// during shutdown, never toward silently keeping a VIP.
func anyVIPConfigured(vips []net.IP) (bool, error) {
	configured, err := listVIPsFn(vips)
	if err != nil {
		return false, err
	}
	return len(configured) > 0, nil
}

// removeConfiguredVIPs removes all locally configured vips. It returns how
// many addresses were found and how many failed to be removed. An address
// that disappeared between listing and removal (keepalived's own clean
// shutdown racing with us) counts as removed.
func removeConfiguredVIPs(vips []net.IP) (found, failed int, err error) {
	configured, err := listVIPsFn(vips)
	if err != nil {
		return 0, 0, err
	}
	for link, addrs := range configured {
		for _, addr := range addrs {
			found++
			log.WithFields(logrus.Fields{
				"address": addr.IPNet.String(),
				"link":    link.Attrs().Name,
			}).Info("handleShutdownResign: force-removing VIP")
			if err := removeVIPFn(link, addr); err != nil {
				if errors.Is(err, unix.EADDRNOTAVAIL) || errors.Is(err, unix.ENOENT) {
					// Already gone: someone else (keepalived) removed it first.
					continue
				}
				log.WithError(err).Error("handleShutdownResign: failed to remove VIP")
				failed++
			}
		}
	}
	return found, failed, nil
}

// handleShutdownResign makes sure no VIP stays configured on this node while
// it shuts down. During "systemctl reboot" systemd tears down all container
// scopes in parallel and keepalived may be killed before it can send its VRRP
// priority-0 resign advertisement and remove the VIPs. The stale VIP then
// keeps attracting client connections which land directly on the local
// kube-apiserver - still serving its graceful drain (~70s, the apiserver
// shutdown-delay window) with readyz=false (OCPBUGS-109633).
//
// Called when dynkeepalived exits, normally on SIGTERM/SIGINT. It only acts
// when the host itself is shutting down: an ordinary pod restart
// (MachineConfig rollout, liveness restart, upgrade) must not touch the VIPs
// of a healthy node.
func handleShutdownResign(conn io.Writer, vips []net.IP) {
	if len(vips) == 0 {
		return
	}
	if !hostShuttingDownFn() {
		log.Info("handleShutdownResign: host is not shutting down, leaving VIPs alone")
		return
	}
	if present, err := anyVIPConfigured(vips); err != nil {
		// Unknown state during a confirmed host shutdown: fail toward
		// resigning, never toward keeping a possibly stale VIP.
		log.WithError(err).Warn("handleShutdownResign: failed to list addresses, proceeding with resign anyway")
	} else if !present {
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
			if present, err := anyVIPConfigured(vips); err == nil && !present {
				log.Info("handleShutdownResign: all VIPs resigned cleanly")
				return
			}
			time.Sleep(resignPollInterval)
		}
	}

	// Last resort: remove the VIPs directly. The peers take over via the
	// VRRP master-down timeout (~3-4s). A still-running keepalived in
	// MASTER state re-adds monitored addresses, so keep removing until the
	// VIPs stay gone for several consecutive polls or the fight window
	// closes. Errors leave the state unknown and only ever prolong the
	// fight; they are never mistaken for success.
	quietPolls := 0
	for deadline := time.Now().Add(resignFightDuration); time.Now().Before(deadline); {
		found, failed, err := removeConfiguredVIPs(vips)
		switch {
		case err != nil:
			log.WithError(err).Warn("handleShutdownResign: failed to list addresses, retrying")
			quietPolls = 0
		case found == 0:
			quietPolls++
			if quietPolls >= resignQuietPolls {
				log.Info("handleShutdownResign: VIPs stayed removed")
				return
			}
		default:
			quietPolls = 0
			if failed > 0 {
				log.WithFields(logrus.Fields{
					"failed": failed,
				}).Warn("handleShutdownResign: some VIPs could not be removed, retrying")
			}
		}
		time.Sleep(resignPollInterval)
	}
	log.Warn("handleShutdownResign: fight window elapsed, VIPs may still be configured")
}
