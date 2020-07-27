package monitor

import (
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

const monitorConf = "unsupported-monitor.conf"
const leaseFile = "lease-%s"
const macAddressBytes = 6

var macPrefixQumranet = [...]byte{0x00, 0x1A, 0x4A}

type VIP struct {
	name string
	ip   net.IP
}

func needLease(cfgPath string) (bool, error) {
	monitorConfFile := filepath.Join(filepath.Dir(cfgPath), monitorConf)

	_, err := os.Stat(monitorConfFile)

	if err == nil {
		log.WithFields(logrus.Fields{
			"file": monitorConfFile,
		}).Info("Monitor conf file exist")
		return true, nil
	}
	if os.IsNotExist(err) {
		log.WithFields(logrus.Fields{
			"file": monitorConfFile,
		}).Info("Monitor conf file doesn't exist")
		return false, nil
	}

	log.WithFields(logrus.Fields{
		"file": monitorConfFile,
	}).WithError(err).Error("Failed to get file status")
	return false, err
}

func leaseVIPs(cfgPath string, clusterName string, vips []VIP) error {
	for _, vip := range vips {
		vipFullName := getInterfaceName(clusterName, vip.name)

		vipMasterIface, _, err := config.GetInterfaceAndNonVIPAddr([]net.IP{vip.ip})
		if err != nil {
			log.WithFields(logrus.Fields{
				"name": vip.name,
				"ip":   vip.ip,
			}).WithError(err).Error("Failed to get the master device for a vip")
			return err
		}

		if err := leaseVIP(cfgPath, vipMasterIface.Name, vipFullName); err != nil {
			log.WithFields(logrus.Fields{
				"name":         vipFullName,
				"masterDevice": vipMasterIface.Name,
				"vip":          vip,
			}).WithError(err).Error("Failed to lease a vip")
			return err
		}
	}

	return nil
}

func leaseVIP(cfgPath, masterDevice, name string) error {
	iface, err := leaseInterface(masterDevice, name)

	if err != nil {
		log.WithFields(logrus.Fields{
			"masterDevice": masterDevice,
			"name":         name,
		}).WithError(err).Error("Failed to lease interface")
		return err
	}

	cmd := exec.Command("dhclient", "-d", "--no-pid", "-sf", "/bin/true",
		"-lf", getLeaseFile(cfgPath, name), "-v", iface.Name, "-H", name)

	return cmd.Start()
}

func leaseInterface(masterDevice string, name string) (*net.Interface, error) {
	mac := generateMac(macPrefixQumranet[:], name)

	// Check if already exist
	if macVlanIfc, err := net.InterfaceByName(name); err == nil {
		return macVlanIfc, nil
	}

	// Read master device
	master, err := netlink.LinkByName(masterDevice)
	if err != nil {
		log.WithFields(logrus.Fields{
			"masterDev": masterDevice,
		}).WithError(err).Error("Failed to read master device")
		return nil, err
	}

	linkAttrs := netlink.LinkAttrs{
		Name:         name,
		ParentIndex:  master.Attrs().Index,
		HardwareAddr: mac,
	}

	mv := &netlink.Macvlan{
		LinkAttrs: linkAttrs,
		Mode:      netlink.MACVLAN_MODE_BRIDGE,
	}

	// Create interface
	if err := netlink.LinkAdd(mv); err != nil {
		log.WithFields(logrus.Fields{
			"masterDev": masterDevice,
			"name":      name,
		}).WithError(err).Error("Failed to create a macvlan")
		return nil, err
	}

	// Read created link
	macvlanInterfaceLink, err := netlink.LinkByName(name)
	if err != nil {
		log.WithFields(logrus.Fields{
			"name": name,
		}).WithError(err).Error("Failed to read new device")
		return nil, err
	}

	// Bring the interface up
	if err = netlink.LinkSetUp(macvlanInterfaceLink); err != nil {
		log.WithFields(logrus.Fields{
			"interface": name,
		}).WithError(err).Error("Failed to bring interface up")
		return nil, err
	}

	// Read created interface
	macVlanIfc, err := net.InterfaceByName(name)
	if err != nil {
		log.WithFields(logrus.Fields{
			"name": name,
		}).WithError(err).Error("Failed to read new device")
		return nil, err
	}

	return macVlanIfc, nil
}

func generateMac(prefix []byte, noise string) (mac net.HardwareAddr) {
	mac = append(net.HardwareAddr{}, prefix...)

	hash := utils.PearsonHash([]byte(noise), int(math.Max(0, float64(macAddressBytes-len(prefix)))))
	mac = append(mac, hash...)

	return
}

func getInterfaceName(clusterName, vipName string) string {
	vipFullName := fmt.Sprintf("%s-%s", clusterName, vipName)

	// Takes the last `interfaceMaxSize` bytes of vipFullName
	return vipFullName[int(math.Max(0, float64(len(vipFullName)-(netlink.IFNAMSIZ-1)))):]
}

func getLeaseFile(cfgPath, name string) string {
	return filepath.Join(filepath.Dir(cfgPath), fmt.Sprintf(leaseFile, name))
}
