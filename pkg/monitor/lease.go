package monitor

import (
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"gopkg.in/yaml.v2"
)

const MonitorConfFileName = "unsupported-monitor.conf"
const leaseFile = "lease-%s"

type vip struct {
	Name       string `yaml:"name"`
	MacAddress string `yaml:"mac-address"`
	IpAddress  string `yaml:"ip-address"`
}

func getVipsToLease(cfgPath string) (vips *[]vip, err error) {
	monitorConfPath := filepath.Join(filepath.Dir(cfgPath), MonitorConfFileName)

	info, err := os.Stat(monitorConfPath)

	if err == nil && info.Mode().IsRegular() {
		log.WithFields(logrus.Fields{
			"file": monitorConfPath,
		}).Info("Monitor conf file exist")

		data, err := ioutil.ReadFile(monitorConfPath)

		if err != nil {
			log.WithFields(logrus.Fields{
				"filename": monitorConfPath,
			}).WithError(err).Error("Failed to read monitor file")
			return nil, err
		}

		return parseMonitorFile(data)
	}
	if os.IsNotExist(err) {
		log.WithFields(logrus.Fields{
			"file": monitorConfPath,
		}).Info("Monitor conf file doesn't exist")
		return nil, nil
	}

	log.WithFields(logrus.Fields{
		"file": monitorConfPath,
	}).WithError(err).Error("Failed to get file status")
	return nil, err
}

func parseMonitorFile(buffer []byte) (*[]vip, error) {
	vips := make([]vip, 0)

	if err := yaml.Unmarshal(buffer, &vips); err != nil {
		log.WithFields(logrus.Fields{
			"buffer": buffer,
		}).WithError(err).Error("Failed to parse monitor file")
		return nil, err
	}

	return &vips, nil
}

func LeaseVIPs(log logrus.FieldLogger, cfgPath string, clusterName string, vipMasterIface string, vips []vip) error {
	for _, vip := range vips {
		vipFullName := GetClusterInterfaceName(clusterName, vip.Name)

		mac, err := net.ParseMAC(vip.MacAddress)

		if err != nil {
			log.WithFields(logrus.Fields{
				"vip": vip,
			}).WithError(err).Error("Failed to parse mac")
			return err
		}

		if err := LeaseVIP(log, cfgPath, vipMasterIface, vipFullName, mac); err != nil {
			log.WithFields(logrus.Fields{
				"masterDevice": vipMasterIface,
				"name":         vipFullName,
				"mac":          vip.MacAddress,
			}).WithError(err).Error("Failed to lease a vip")
			return err
		}
	}

	return nil
}

func LeaseVIP(log logrus.FieldLogger, cfgPath, masterDevice, name string, mac net.HardwareAddr) error {
	iface, err := LeaseInterface(log, masterDevice, name, mac)

	if err != nil {
		log.WithFields(logrus.Fields{
			"masterDevice": masterDevice,
			"name":         name,
		}).WithError(err).Error("Failed to lease interface")
		return err
	}

	cmd := exec.Command("dhclient", "-d", "--no-pid", "-sf", "/bin/true",
		"-lf", getLeaseFile(cfgPath, name), "-v", iface.Name, "-H", name)
	cmd.Stderr = os.Stderr

	return cmd.Start()
}

func LeaseInterface(log logrus.FieldLogger, masterDevice string, name string, mac net.HardwareAddr) (*net.Interface, error) {
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
		Mode:      netlink.MACVLAN_MODE_PRIVATE,
	}

	// Create interface
	if err := netlink.LinkAdd(mv); err != nil {
		log.WithFields(logrus.Fields{
			"masterDev": masterDevice,
			"name":      name,
			"mac":       mac,
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

func GetClusterInterfaceName(clusterName, vipName string) string {
	vipFullName := fmt.Sprintf("%s-%s", clusterName, vipName)

	// Takes the last `interfaceMaxSize` bytes of vipFullName
	return vipFullName[int(math.Max(0, float64(len(vipFullName)-(netlink.IFNAMSIZ-1)))):]
}

func getLeaseFile(cfgPath, name string) string {
	return filepath.Join(filepath.Dir(cfgPath), fmt.Sprintf(leaseFile, name))
}
