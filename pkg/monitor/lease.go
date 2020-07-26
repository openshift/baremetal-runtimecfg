package monitor

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/milosgajdos/tenus"
	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
	"github.com/sirupsen/logrus"
)

const monitorConf = "monitor.conf"
const macPrefixQumranet = "00:1A:4A"
const leaseInterfaceTemplate = "mc-%s"
const leaseFile = "/tmp/my.lease"

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

func leaseVIPs(clusterName string, vips []VIP) error {
	for _, vip := range vips {
		vipFullName := fmt.Sprintf("%s-%s", clusterName, vip.name)
		if err := leaseVIP(vipFullName, vip.ip); err != nil {
			log.WithFields(logrus.Fields{
				"name": vipFullName,
				"vip":  vip,
			}).Error("Failed to lease a vip")
			return err
		}
	}

	return nil
}

func leaseVIP(name string, vip net.IP) error {
	iface, err := leaseInterface(name, vip)

	if err != nil {
		return err
	}

	cmd := exec.Command("dhclient", "-d", "--no-pid", "-sf", "/bin/true", "-lf", leaseFile, "-v", iface.Name, "-H", name)
	cmd.Stderr = os.Stderr

	return cmd.Start()
}

func leaseInterface(name string, vip net.IP) (*net.Interface, error) {
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

	// Check if already exist
	if iface, err := net.InterfaceByName(fmt.Sprintf(leaseInterfaceTemplate, name)); err == nil {
		return iface, nil
	}

	// Create a new network macvlan
	macvlanIface, err := tenus.NewMacVlanLinkWithOptions(vipIface.Name, tenus.MacVlanOptions{
		Dev:     fmt.Sprintf(leaseInterfaceTemplate, name),
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

	// macvlanIface.NetInterface() doesn't acquire the correct interface. Thus reading it from OS
	if iface, err := net.InterfaceByName(fmt.Sprintf(leaseInterfaceTemplate, name)); err == nil {
		return iface, nil
	} else {
		log.WithFields(logrus.Fields{
			"interface": macvlanIface.NetInterface().Name,
		}).Error("Failed to get interface")
		return nil, err
	}
}

func generateMac(noise string, iface net.Interface) (mac net.HardwareAddr, err error) {
	hash := utils.PearsonHash([]byte(noise), 3)
	lowBits := fmt.Sprintf(":%02x:%02x:%02x", hash[0], hash[1], hash[2])

	if mac, err = net.ParseMAC(macPrefixQumranet + lowBits); err != nil {
		return nil, err
	}

	return mac, nil
}
