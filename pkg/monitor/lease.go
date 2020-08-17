package monitor

import (
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"gopkg.in/fsnotify.v1"
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

		if err := LeaseVIP(log, cfgPath, vipMasterIface, vipFullName, mac, vip.IpAddress); err != nil {
			log.WithFields(logrus.Fields{
				"masterDevice": vipMasterIface,
				"name":         vipFullName,
				"mac":          mac,
				"ip":           vip.IpAddress,
			}).WithError(err).Error("Failed to lease a vip")
			return err
		}
	}

	return nil
}

func LeaseVIP(log logrus.FieldLogger, cfgPath, masterDevice, name string, mac net.HardwareAddr, ip string) error {
	iface, err := LeaseInterface(log, masterDevice, name, mac)

	if err != nil {
		log.WithFields(logrus.Fields{
			"masterDevice": masterDevice,
			"name":         name,
		}).WithError(err).Error("Failed to lease interface")
		return err
	}

	lease_file := getLeaseFile(cfgPath, name)
	if f, err := os.Create(lease_file); err != nil {
		log.WithFields(logrus.Fields{
			"name": lease_file,
		}).WithError(err).Error("Failed to create lease file")
		return err
	} else {
		f.Close()
	}

	if err := WatchLeaseFile(log, lease_file, iface.Name, ip); err != nil {
		log.WithFields(logrus.Fields{
			"filename":      lease_file,
			"expectedIface": iface.Name,
			"expectedIp":    ip,
		}).WithError(err).Error("Failed to watch the lease file")
		return err
	}

	cmd := exec.Command("dhclient", "-d", "--no-pid", "-sf", "/bin/true",
		"-lf", lease_file, "-v", iface.Name, "-H", name)
	cmd.Stderr = os.Stderr

	return cmd.Start()
}

func WatchLeaseFile(log logrus.FieldLogger, fileName, expectedIface, expectedIp string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.WithError(err).Error("Failed to add a create a new watcher")
		return err
	}

	err = watcher.Add(fileName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"filename": fileName,
		}).WithError(err).Error("Failed to add a watcher to lease file")
		return err
	}

	go func(log logrus.FieldLogger) {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Op&fsnotify.Write == fsnotify.Write {
					if iface, ip, err := GetLastLeaseFromFile(log, fileName); err != nil {
						log.WithFields(logrus.Fields{
							"filename": fileName,
						}).WithError(err).Error("Failed to get lease information from leasing file")
					} else if iface != expectedIface || ip != expectedIp {
						log.WithFields(logrus.Fields{
							"filename":      fileName,
							"iface":         iface,
							"expectedIface": expectedIface,
							"ip":            ip,
							"expectedIp":    expectedIp,
						}).WithError(err).Error("A new lease has been written to the lease file with wrong data")
					} else {
						log.WithFields(logrus.Fields{
							"fileName": fileName,
							"iface":    iface,
							"ip":       ip,
						}).Info("A new lease has been written to the lease file with the right data")
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}

				log.WithFields(logrus.Fields{
					"filename": fileName,
				}).WithError(err).Error("Lease file watcher error")
			}
		}
	}(log)

	return nil
}

func GetLastLeaseFromFile(log logrus.FieldLogger, fileName string) (string, string, error) {
	data, err := ioutil.ReadFile(fileName)

	if err != nil {
		log.WithFields(logrus.Fields{
			"filename": fileName,
		}).WithError(err).Error("Failed to read lease file")
		return "", "", err
	}

	patternIface := regexp.MustCompile(`\s*interface\s+\"(\w+)\";`)
	matchesIface := patternIface.FindAllStringSubmatch(string(data), -1)

	if len(matchesIface) == 0 {
		err := fmt.Errorf("No interfaces in lease file")
		log.WithFields(logrus.Fields{
			"filename": fileName,
		}).Error(err)

		return "", "", err
	}

	patternIp := regexp.MustCompile(`.+fixed-address\s+(.+);`)
	matchesIp := patternIp.FindAllStringSubmatch(string(data), -1)

	if len(matchesIp) == 0 {
		err := fmt.Errorf("No fixed addresses in lease file")
		log.WithFields(logrus.Fields{
			"filename": fileName,
		}).Error(err)

		return "", "", err
	}

	if len(matchesIp) != len(matchesIface) {
		err := fmt.Errorf("Mismatch amount of interfaces and ips")
		log.WithFields(logrus.Fields{
			"matchesIp":    matchesIp,
			"matchesIface": matchesIface,
		}).Error(err)

		return "", "", err
	}

	return matchesIface[len(matchesIface)-1][1], matchesIp[len(matchesIp)-1][1], nil
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
