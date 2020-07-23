package monitor

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/milosgajdos/tenus"
	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
	"github.com/sirupsen/logrus"
)

/*
	After reading the cmd flags and before executing the keepalived-monitor:
	1. Get the directory of args[2] = path_to_config (e.g. /etc/keepalived/keepalived.conf) [V]
	2. Check if the there's a file named "monitor.conf" in that directory. If true, then: [V]
		2.1. for [API interface, Ingress interface]: . [V]
			2.1.1 Generate a unique MAC address
				2.1.1.1. The first 24 bits of the MAC should be the same as the real interface mac address.
						Get the interface by using `GetInterfaceAndNonVIPAddr()` and after that get its mac address.
				2.1.1.2. The last 24 bits are generated using some hash function.
						An example is the way the VRIDs are generated in `PopulateVRIDs()`
			2.1.2. Create an interface type macvlan per vip using the generated mac address [V]
			2.1.3. Run dhclient with the new vip interface in the background (goroutine)
	3. Continue regular run
*/

const monitorConf = "monitor.conf"

type VIP struct {
	name string
	ip   net.IP
}

func needLease(cfgPath string) (bool, error) {
	monitorConfFile := filepath.Join(filepath.Dir(cfgPath), monitorConf)

	_, err := os.Stat(monitorConfFile)

	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func leaseVIPs(vips []VIP) error {
	for _, vip := range vips {
		if err := leaseVIP(vip.name, vip.ip); err != nil {
			log.WithFields(logrus.Fields{
				"vip": vip,
			}).Error("Failed to lease a vip")
			return err
		}
	}

	return nil
}

func leaseVIP(name string, vip net.IP) error {
	iface, err := createVIPInterface(name, vip)

	if err != nil {
		return err
	}

	// Renew with dhclient. Maybe can be replaced with https://github.com/insomniacslk/dhcp
	// TODO: define timeout
	cmd := exec.Command("dhclient", "-d", "-timeout", "5", iface.Name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func createVIPInterface(name string, vip net.IP) (*net.Interface, error) {
	vipIface, _, err := config.GetInterfaceAndNonVIPAddr([]net.IP{vip})
	if err != nil {
		return nil, err
	}

	mac, err := generateMac(name, vipIface)

	if err != nil {
		log.WithFields(logrus.Fields{
			"noise": name,
			"iface": vipIface,
		}).Error("Failed to generate a mac address")
		return nil, err
	}

	// Create a new network macvlan
	macvlanIface, err := tenus.NewMacVlanLinkWithOptions(vipIface.Name, tenus.MacVlanOptions{
		Dev:     fmt.Sprintf("mc-%s-%s", vipIface.Name, name),
		Mode:    "bridge",
		MacAddr: mac.String(),
	})

	if err != nil {
		log.WithFields(logrus.Fields{
			"masterDev": vipIface.Name,
			"macAddr":   mac.String(),
		}).Error("Failed to create a macvlan")
		return nil, err
	}

	// Bring the interface up
	if err = macvlanIface.SetLinkUp(); err != nil {
		log.WithFields(logrus.Fields{
			"interface": macvlanIface.NetInterface().Name,
		}).Error("Failed to bring interface up")
		return nil, err
	}

	return macvlanIface.NetInterface(), nil
}

func generateMac(noise string, iface net.Interface) (mac net.HardwareAddr, err error) {
	// TODO: Build a more sophisticated mac generation. Keep in mind to depend on clusterName
	// 1. All the nodes will have the same mac.
	// 2. Avoid conflicts (with the realIP and the new VIPS)
	highBits := iface.HardwareAddr.String()[:len(iface.HardwareAddr.String())/2]

	lowBits := strings.Repeat(fmt.Sprintf(":%02x", utils.FletcherChecksum8(noise)+1), 3)

	if mac, err = net.ParseMAC(highBits + lowBits); err != nil {
		return nil, err
	}

	return mac, nil
}
