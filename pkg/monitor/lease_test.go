package monitor

import (
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/syndtr/gocapability/capability"
	"github.com/vishvananda/netlink"
	"gopkg.in/fsnotify.v1"
	"gopkg.in/yaml.v2"
)

const LeaseTime = 5 * time.Second

var _ = Describe("lease_vip", func() {
	var (
		log       *logrus.Logger
		hook      *test.Hook
		realIface *net.Interface
		testIface *net.Interface
		testIP    string
		testName  string
		testMac   net.HardwareAddr
		err       error
		cfgPath   string
	)

	BeforeEach(func() {
		if !hasCap(capability.CAP_NET_ADMIN, capability.CAP_NET_RAW) {
			Skip("Must run with capabilities: CAP_NET_ADMIN, CAP_NET_RAW")
		}

		log = logrus.New()
		hook = test.NewLocal(log)

		realIface, err = net.InterfaceByName("eth0")
		Expect(err).ShouldNot(HaveOccurred())

		testName = generateUUID()[:4]
		testIP = generateIP()
		testMac = generateMac()

		testName = generateUUID()[:4]

		file, err := ioutil.TempFile("", "config")
		Expect(err).ShouldNot(HaveOccurred())
		cfgPath = file.Name()
	})

	Describe("LeaseInterface", func() {
		It("happy_flow", func() {
			testIface, err = LeaseInterface(log, realIface.Name, testName, testMac)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("different_names", func() {
			testIface, err = LeaseInterface(log, realIface.Name, testName, testMac)
			Expect(err).ShouldNot(HaveOccurred())

			iface2, err := LeaseInterface(log, realIface.Name, generateUUID()[:4], generateMac())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(iface2.Name).ShouldNot(Equal(realIface.Name))
			Expect(iface2.Name).ShouldNot(Equal(testIface.Name))
			Expect(iface2.HardwareAddr).ShouldNot(Equal(realIface.HardwareAddr))
			Expect(iface2.HardwareAddr).ShouldNot(Equal(testIface.HardwareAddr))

			deleteInterface(iface2.Name)
		})

		It("predefined_macvlan", func() {
			testIface, err = LeaseInterface(log, realIface.Name, testName, testMac)
			Expect(err).ShouldNot(HaveOccurred())

			iface2, err := LeaseInterface(log, realIface.Name, testName, testMac)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(iface2.Name).Should(Equal(testIface.Name))
			Expect(iface2.HardwareAddr).Should(Equal(testIface.HardwareAddr))
		})

		AfterEach(func() {
			Expect(testIface.Name).ShouldNot(Equal(realIface.Name))
			Expect(testIface.HardwareAddr).ShouldNot(Equal(realIface.HardwareAddr))

			// Verify link parameters
			link, err := netlink.LinkByName(testIface.Name)
			Expect(err).ShouldNot(HaveOccurred())
			macvlan, ok := link.(*netlink.Macvlan)
			Expect(ok).Should(BeTrue())
			Expect(macvlan.Mode).Should(Equal(netlink.MACVLAN_MODE_PRIVATE))

			iface, err := net.InterfaceByName(testName)
			Expect(err).ShouldNot(HaveOccurred())

			// Create new interface without allocating an IP - That's keepalived responsibility
			addrs, err := iface.Addrs()
			Expect(iface.HardwareAddr).Should(Equal(testIface.HardwareAddr))
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(addrs)).Should(Equal(0))
		})
	})

	Describe("LeaseVIP", func() {
		It("single_infinite_run", func() {
			var ip string

			By("run", func() {
				Expect(LeaseVIP(log, cfgPath, realIface.Name, testName, testMac, "")).ShouldNot(HaveOccurred())
				time.Sleep(LeaseTime)
				Expect(getLastInterfaceFromLeaseFile(cfgPath, testName)).Should(Equal(testName))
				ip = getLastIPFromLeaseFile(cfgPath, testName)
				Expect(ip).ShouldNot(BeEmpty())
				verifyWatcherRecordLog(hook, testName, ip, true)
			})

			By("dhclient_still_running", func() {
				Expect(isDhclientAlive()).Should(BeTrue())
			})

			By("watcher_still_running", func() {
				entries := hook.Entries[:]

				time.Sleep(LeaseTime * 4)
				Expect(hook.Entries).ShouldNot(Equal(entries))
				Expect(getLastIPFromLeaseFile(cfgPath, testName)).ShouldNot(BeEmpty())
				verifyWatcherRecordLog(hook, testName, ip, true)
			})
		})

		Context("multiple_runs", func() {
			BeforeEach(func() {
				Expect(LeaseVIP(log, cfgPath, realIface.Name, testName, testMac, "")).ShouldNot(HaveOccurred())
				time.Sleep(LeaseTime)
			})

			It("multiple_vips", func() {
				for i := 2; i < 5; i++ {
					Expect(LeaseVIP(log, cfgPath, realIface.Name, testName+strconv.Itoa(i), generateMac(), "")).ShouldNot(HaveOccurred())
				}

				time.Sleep(LeaseTime)

				for i := 2; i < 5; i++ {
					deleteInterface(testName + strconv.Itoa(i))
				}
			})

			It("different_mac_different_ip", func() {
				ip := getLastIPFromLeaseFile(cfgPath, testName)
				verifyWatcherRecordLog(hook, testName, ip, true)
				prevEntries := hook.Entries[:]
				newName := generateUUID()[:4]

				Expect(LeaseVIP(log, cfgPath, realIface.Name, newName, generateMac(), "")).ShouldNot(HaveOccurred())
				time.Sleep(LeaseTime)
				newIp := getLastIPFromLeaseFile(cfgPath, newName)
				Expect(newIp).ShouldNot(Equal(ip))
				verifyWatcherRecordLog(hook, newName, newIp, true)
				Expect(hook.Entries).ShouldNot(Equal(prevEntries))
				deleteInterface(newName)
			})

			It("same_mac_same_ip", func() {
				ip := getLastIPFromLeaseFile(cfgPath, testName)
				verifyWatcherRecordLog(hook, testName, ip, true)
				prevEntries := hook.Entries[:]

				Expect(LeaseVIP(log, cfgPath, realIface.Name, testName, testMac, ip)).ShouldNot(HaveOccurred())
				time.Sleep(LeaseTime)
				Expect(getLastIPFromLeaseFile(cfgPath, testName)).Should(Equal(ip))
				verifyWatcherRecordLog(hook, testName, ip, true)
				Expect(hook.Entries).ShouldNot(Equal(prevEntries))
			})

			AfterEach(func() {
				iface, err := net.InterfaceByName(testName)
				Expect(err).ShouldNot(HaveOccurred())

				// Lease without allocating an IP - That's keepalived responsibility
				addrs, err := iface.Addrs()
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(addrs)).Should(Equal(0))
			})
		})

		Context("verify_ip", func() {
			It("skip_verifying_ip", func() {
				Expect(LeaseVIP(log, cfgPath, realIface.Name, testName, testMac, "")).ShouldNot(HaveOccurred())
				time.Sleep(LeaseTime)
				ip := getLastIPFromLeaseFile(cfgPath, testName)
				verifyWatcherRecordLog(hook, testName, ip, true)
			})

			It("ip_mismatch", func() {
				Expect(LeaseVIP(log, cfgPath, realIface.Name, testName, testMac, testIP)).ShouldNot(HaveOccurred())
				time.Sleep(LeaseTime)
				ip := getLastIPFromLeaseFile(cfgPath, testName)
				verifyWatcherRecordLog(hook, testName, ip, false)
			})

			It("verify_ip_after_allocation", func() {
				Expect(LeaseVIP(log, cfgPath, realIface.Name, testName, testMac, "")).ShouldNot(HaveOccurred())
				time.Sleep(LeaseTime)

				ip := getLastIPFromLeaseFile(cfgPath, testName)
				Expect(LeaseVIP(log, cfgPath, realIface.Name, testName, testMac, ip)).ShouldNot(HaveOccurred())
				time.Sleep(LeaseTime)
				ip = getLastIPFromLeaseFile(cfgPath, testName)
				verifyWatcherRecordLog(hook, testName, ip, true)
			})
		})

		It("server_hardcoded_host", func() {
			testIP := "172.99.0.55"
			mac, err := net.ParseMAC("00:1a:4a:92:c8:d7")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(LeaseVIP(log, cfgPath, realIface.Name, testName, mac, testIP)).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)

			ip := getLastIPFromLeaseFile(cfgPath, testName)
			Expect(getLastIPFromLeaseFile(cfgPath, testName)).Should(Equal(testIP))
			verifyWatcherRecordLog(hook, testName, ip, true)
		})
	})

	Describe("LeaseVIPs", func() {
		It("happy_flow", func() {
			vips := []vip{
				{"api", generateMac().String(), ""},
				{"ingress", generateMac().String(), ""},
			}
			Expect(LeaseVIPs(log, cfgPath, realIface.Name, vips)).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)

			for _, vip := range vips {
				Expect(getLastInterfaceFromLeaseFile(cfgPath, vip.Name)).Should(Equal(vip.Name))

				deleteInterface(vip.Name)
			}
		})
	})

	AfterEach(func() {
		cleanEnv(cfgPath)

		// Cleanup
		if testIface != nil {
			deleteInterface(testIface.Name)
		}
	})
})

var _ = Describe("getVipsToLease", func() {
	var (
		path    string = filepath.Join("/tmp", MonitorConfFileName)
		cfgPath string = filepath.Join("/tmp", "cfg")
	)

	It("file_doesnt_exist", func() {
		vips, err := getVipsToLease(cfgPath)
		Expect(err).Should(BeNil())
		Expect(vips).Should(BeNil())
	})

	It("path_is_directory", func() {
		var buffer []byte

		Expect(ioutil.WriteFile(path, buffer, os.ModeDir)).ShouldNot(HaveOccurred())

		vips, err := getVipsToLease(cfgPath)
		Expect(err).Should(HaveOccurred())
		Expect(vips).Should(BeNil())
	})

	It("invalid_content", func() {
		var buffer []byte = []byte("hello\n")

		Expect(ioutil.WriteFile(path, buffer, 0644)).ShouldNot(HaveOccurred())

		vips, err := getVipsToLease(cfgPath)
		Expect(err).Should(HaveOccurred())
		Expect(vips).Should(BeNil())
	})

	It("invalid_array_content", func() {
		data := []vip{
			{"api", generateMac().String(), generateIP()},
			{"ingress", generateMac().String(), generateIP()},
		}

		buffer, err := yaml.Marshal(&data)
		Expect(err).ShouldNot(HaveOccurred())

		Expect(ioutil.WriteFile(path, buffer, 0644)).ShouldNot(HaveOccurred())

		vips, err := getVipsToLease(cfgPath)
		Expect(err).Should(HaveOccurred())
		Expect(vips).Should(BeNil())
	})

	It("invalid_yaml_content", func() {
		data := yamlVips{
			APIVip:     nil,
			IngressVip: &vip{"ingress", generateMac().String(), generateIP()},
		}

		buffer, err := yaml.Marshal(&data)
		Expect(err).ShouldNot(HaveOccurred())

		Expect(ioutil.WriteFile(path, buffer, 0644)).ShouldNot(HaveOccurred())

		vips, err := getVipsToLease(cfgPath)
		Expect(err).Should(HaveOccurred())
		Expect(vips).Should(BeNil())
	})

	It("valid_yaml_content", func() {
		data := yamlVips{
			APIVip:     &vip{"api", generateMac().String(), generateIP()},
			IngressVip: &vip{"ingress", generateMac().String(), generateIP()},
		}

		buffer, err := yaml.Marshal(&data)
		Expect(err).ShouldNot(HaveOccurred())

		Expect(ioutil.WriteFile(path, buffer, 0644)).ShouldNot(HaveOccurred())

		vips, err := getVipsToLease(cfgPath)
		Expect(err).Should(BeNil())
		Expect(*vips).Should(Equal(data))
	})

	AfterEach(func() {
		_ = os.RemoveAll(path)
	})
})

var _ = Describe("RunWatcher", func() {
	var (
		logger    *logrus.Logger
		hook      *test.Hook
		leaseFile string
		watcher   *fsnotify.Watcher
	)

	BeforeEach(func() {
		file, err := ioutil.TempFile("", "config")
		Expect(err).ShouldNot(HaveOccurred())
		leaseFile = file.Name()

		logger = logrus.New()
		hook = test.NewLocal(logger)

		watcher, err = utils.CreateFileWatcher(log, leaseFile)
		Expect(err).ShouldNot(HaveOccurred())

	})

	Context("RunFiniteWatcher", func() {
		var (
			write chan error
		)

		BeforeEach(func() {
			write = make(chan error)
		})

		It("invalid_file", func() {
			RunFiniteWatcher(logger, watcher, leaseFile, "", "", write)
			appendToFile(leaseFile, "hello world")

			Expect(<-write).Should(HaveOccurred())
			Expect(hook.LastEntry().Message).Should(Equal("Failed to get lease information from leasing file"))
		})

		It("wrong_interface", func() {
			iface := "wrong_interface"
			ip := "172.99.0.72"

			RunFiniteWatcher(logger, watcher, leaseFile, "another_interface", ip, write)
			appendToFile(leaseFile, createLeaseData(iface, ip))

			Expect(<-write).Should(HaveOccurred())
			verifyWatcherRecordLog(hook, iface, ip, false)
		})

		It("wrong_ip", func() {
			iface := "wrong_ip"
			ip := "172.99.0.72"

			RunFiniteWatcher(logger, watcher, leaseFile, iface, "172.99.0.16", write)
			appendToFile(leaseFile, createLeaseData(iface, ip))

			Expect(<-write).Should(HaveOccurred())
			verifyWatcherRecordLog(hook, iface, ip, false)
		})

		It("valid_lease_file", func() {
			iface := "valid_lease_file"
			ip := "172.99.0.72"

			RunFiniteWatcher(logger, watcher, leaseFile, iface, ip, write)
			appendToFile(leaseFile, createLeaseData(iface, ip))

			Expect(<-write).ShouldNot(HaveOccurred())
			verifyWatcherRecordLog(hook, iface, ip, true)
		})

		AfterEach(func() {
			close(write)
		})
	})

	Context("RunInfiniteWatcher", func() {
		It("single_lease", func() {
			iface := "valid_lease_file"
			ip := "172.99.0.72"

			RunInfiniteWatcher(logger, watcher, leaseFile, iface, ip)
			appendToFile(leaseFile, createLeaseData(iface, ip))
			time.Sleep(100 * time.Millisecond)

			verifyWatcherRecordLog(hook, iface, ip, true)
		})

		It("valid_multiple_leases", func() {
			iface := "valid_multiple_leases"
			ip := "172.99.0.72"

			RunInfiniteWatcher(logger, watcher, leaseFile, iface, ip)

			for range []int{1, 2, 3} {
				appendToFile(leaseFile, createLeaseData(iface, ip))
				time.Sleep(100 * time.Millisecond)

				verifyWatcherRecordLog(hook, iface, ip, true)
			}
		})

		It("various_multiple_leases", func() {
			validIface := "valid_lease"
			validIp := "1.1.1.1"

			invalid_leases := []string{"2.2.2.2", "3.3.3.3", "4.4.4.4"}

			RunInfiniteWatcher(logger, watcher, leaseFile, validIface, validIp)

			By("first_valid_lease", func() {
				appendToFile(leaseFile, createLeaseData(validIface, validIp))
				time.Sleep(100 * time.Millisecond)

				verifyWatcherRecordLog(hook, validIface, validIp, true)
			})

			By("other_invalid_leases", func() {
				for iface, ip := range invalid_leases {
					appendToFile(leaseFile, createLeaseData(strconv.Itoa(iface), ip))
					time.Sleep(100 * time.Millisecond)

					verifyWatcherRecordLog(hook, strconv.Itoa(iface), ip, false)
				}
			})

			By("another_valid_lease", func() {
				appendToFile(leaseFile, createLeaseData(validIface, validIp))
				time.Sleep(100 * time.Millisecond)

				verifyWatcherRecordLog(hook, validIface, validIp, true)
			})
		})
	})

	AfterEach(func() {
		_ = os.RemoveAll(leaseFile)
	})
})

func appendToFile(filename string, data string) {
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	Expect(err).ShouldNot(HaveOccurred())
	_, err = f.WriteString(data)
	Expect(err).ShouldNot(HaveOccurred())
}

func createLeaseData(ifaceName, ip string) string {
	return fmt.Sprintf(`
lease {
	interface "%s";
	fixed-address %s;
	option subnet-mask 255.255.255.0;
	option dhcp-lease-time 43200;
	option dhcp-message-type 5;
	option dhcp-server-identifier 172.99.0.2;
	renew 1 2020/08/17 21:11:32;
	rebind 2 2020/08/18 01:43:00;
	expire 2 2020/08/18 03:13:00;
}`, ifaceName, ip)
}

func verifyWatcherRecordLog(hook *test.Hook, iface string, ip string, match bool) {
	Expect(hook.LastEntry()).ShouldNot(BeNil())

	if match {
		Expect(hook.LastEntry().Message).Should(Equal("A new lease has been written to the lease file with the right data"))
	} else {
		Expect(hook.LastEntry().Message).Should(Equal("A new lease has been written to the lease file with wrong data"))
	}

	Expect(hook.LastEntry().Data["iface"]).Should(Equal(iface))
	Expect(hook.LastEntry().Data["ip"]).Should(Equal(ip))
}

func hasCap(newCaps ...capability.Cap) bool {
	caps, err := capability.NewPid(0)
	if err != nil {
		log.Fatal(err)
		return false
	}
	caps.Clear(capability.CAPS)
	caps.Set(capability.CAPS, newCaps...)

	return caps.Apply(capability.CAPS) == nil
}

func deleteInterface(name string) {
	if iface, err := netlink.LinkByName(name); err == nil {
		Expect(netlink.LinkDel(iface)).ShouldNot(HaveOccurred())
	}
}

func cleanEnv(cfgPath string) {
	_ = getOutput(`pkill dhclient || true`)
}

func isDhclientAlive() bool {
	return getOutput(`ps -ef | grep dhclient | egrep -v "(grep|defunct)"`) != "exit status 1"
}

func generateUUID() string {
	return getOutput("uuidgen")
}

func generateIP() string {
	buf := make([]byte, 4)
	_, err := rand.Read(buf)
	Expect(err).ShouldNot(HaveOccurred())

	return fmt.Sprintf("%d.%d.%d.%d", buf[0], buf[1], buf[2], buf[3])
}

func generateMacString() string {
	var MacPrefixQumranet = [...]byte{0x00, 0x1A, 0x4A}

	buf := make([]byte, 3)
	_, err := rand.Read(buf)
	Expect(err).ShouldNot(HaveOccurred())

	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		MacPrefixQumranet[0], MacPrefixQumranet[1], MacPrefixQumranet[2], buf[0], buf[1], buf[2])
}

func generateMac() net.HardwareAddr {
	mac, err := net.ParseMAC(generateMacString())
	Expect(err).ShouldNot(HaveOccurred())

	return mac
}

func getLastIPFromLeaseFile(cfgPath, name string) string {
	leaseFile := GetLeaseFile(cfgPath, name)

	info, err := os.Stat(leaseFile)
	Expect(err).ShouldNot(HaveOccurred())
	Expect(info.Mode().IsRegular()).Should(BeTrue())

	_, ip, err := GetLastLeaseFromFile(log, leaseFile)
	Expect(err).ShouldNot(HaveOccurred())

	return ip
}

func getLastInterfaceFromLeaseFile(cfgPath, name string) string {
	leaseFile := GetLeaseFile(cfgPath, name)

	info, err := os.Stat(leaseFile)
	Expect(err).ShouldNot(HaveOccurred())
	Expect(info.Mode().IsRegular()).Should(BeTrue())

	iface, _, err := GetLastLeaseFromFile(log, leaseFile)
	Expect(err).ShouldNot(HaveOccurred())

	return iface
}

func getOutput(command string) string {
	cmd := exec.Command("bash", "-c", command)
	out, err := cmd.Output()
	if err != nil {
		return err.Error()
	}

	return strings.TrimSpace(string(out))
}

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Leases tests")
}
