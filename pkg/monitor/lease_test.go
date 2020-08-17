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

	"github.com/go-openapi/strfmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/syndtr/gocapability/capability"
	"github.com/vishvananda/netlink"
	"gopkg.in/yaml.v2"
)

const LeaseTime = 5 * time.Second

var _ = Describe("lease_vip", func() {
	var (
		log       logrus.FieldLogger
		realIface *net.Interface
		testIface *net.Interface
		testIP    strfmt.IPv4
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
		BeforeEach(func() {
			Expect(LeaseVIP(log, cfgPath, realIface.Name, testName, testMac, testIP.String())).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)
		})

		It("happy_flow", func() {
			Expect(getInterfaceFromLeaseFile(cfgPath, testName)).Should(Equal(testName))
			Expect(getIPFromLeaseFile(cfgPath, testName)).ShouldNot(BeEmpty())
		})

		It("multiple_vips", func() {
			for i := 2; i < 5; i++ {
				Expect(LeaseVIP(log, cfgPath, realIface.Name, testName+strconv.Itoa(i), generateMac(), testIP.String())).ShouldNot(HaveOccurred())
			}

			time.Sleep(LeaseTime)

			for i := 2; i < 5; i++ {
				deleteInterface(testName + strconv.Itoa(i))
			}
		})

		It("different_mac_different_ip", func() {
			ip := getIPFromLeaseFile(cfgPath, testName)

			cleanEnv(cfgPath)

			newName := generateUUID()[:4]
			Expect(LeaseVIP(log, cfgPath, realIface.Name, newName, generateMac(), testIP.String())).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)
			Expect(getIPFromLeaseFile(cfgPath, newName)).ShouldNot(Equal(ip))
			deleteInterface(newName)
		})

		It("same_mac_same_ip", func() {
			ip := getIPFromLeaseFile(cfgPath, testName)

			cleanEnv(cfgPath)

			Expect(LeaseVIP(log, cfgPath, realIface.Name, testName, testMac, testIP.String())).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)
			Expect(getIPFromLeaseFile(cfgPath, testName)).Should(Equal(ip))
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

	It("server_hardcoded_host", func() {
		testIP := "172.99.0.55"
		mac, err := net.ParseMAC("00:1a:4a:92:c8:d7")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(LeaseVIP(log, cfgPath, realIface.Name, testName, mac, testIP)).ShouldNot(HaveOccurred())

		time.Sleep(LeaseTime)
		Expect(getIPFromLeaseFile(cfgPath, testName)).Should(Equal(testIP))
	})

	Describe("LeaseVIPs", func() {
		It("happy_flow", func() {
			clusterName := generateUUID()
			vips := []vip{
				{"api", generateMac().String(), generateIP().String()},
				{"ingress", generateMac().String(), generateIP().String()},
			}
			Expect(LeaseVIPs(log, cfgPath, clusterName, realIface.Name, vips)).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)

			for _, vip := range vips {
				Expect(getInterfaceFromLeaseFile(cfgPath, GetClusterInterfaceName(clusterName, vip.Name))).Should(
					Equal(GetClusterInterfaceName(clusterName, vip.Name)))

				deleteInterface(GetClusterInterfaceName(clusterName, vip.Name))
			}
		})

		It("cluster_long_name", func() {
			clusterName := generateUUID()
			vips := []vip{
				{generateUUID(), testMac.String(), testIP.String()},
			}
			Expect(LeaseVIPs(log, cfgPath, clusterName, realIface.Name, vips)).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)

			for _, vip := range vips {
				Expect(getInterfaceFromLeaseFile(cfgPath, GetClusterInterfaceName(clusterName, vip.Name))).Should(
					Equal(GetClusterInterfaceName(clusterName, vip.Name)))

				deleteInterface(GetClusterInterfaceName(clusterName, vip.Name))
			}
		})

		It("short_name", func() {
			clusterName := generateUUID()[:1]
			vips := []vip{
				{generateUUID()[:1], testMac.String(), testIP.String()},
			}
			Expect(LeaseVIPs(log, cfgPath, clusterName, realIface.Name, vips)).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)

			for _, vip := range vips {
				Expect(getInterfaceFromLeaseFile(cfgPath, GetClusterInterfaceName(clusterName, vip.Name))).Should(
					Equal(GetClusterInterfaceName(clusterName, vip.Name)))

				deleteInterface(GetClusterInterfaceName(clusterName, vip.Name))
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

	It("valid_content", func() {
		data := []vip{
			{"api", generateMac().String(), generateIP().String()},
			{"ingress", generateMac().String(), generateIP().String()},
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

var _ = Describe("WatchLeaseFile", func() {
	var (
		leaseFile string
	)

	BeforeEach(func() {
		file, err := ioutil.TempFile("", "config")
		Expect(err).ShouldNot(HaveOccurred())
		leaseFile = file.Name()
	})

	It("invalid_file", func() {
		logger, hook := test.NewNullLogger()

		Expect(WatchLeaseFile(logger, leaseFile, "", "")).ShouldNot(HaveOccurred())
		Expect(ioutil.WriteFile(leaseFile, []byte("hello world"), 0644)).ShouldNot(HaveOccurred())
		time.Sleep(100 * time.Millisecond) // give system time to sync write change

		Expect(hook.LastEntry().Message).Should(Equal("Failed to get lease information from leasing file"))
	})

	It("wrong_interface", func() {
		logger, hook := test.NewNullLogger()

		iface := "wrong_interface"
		ip := "172.99.0.72"
		data := createLeaseData(iface, ip)

		Expect(WatchLeaseFile(logger, leaseFile, "another_interface", ip)).ShouldNot(HaveOccurred())
		Expect(ioutil.WriteFile(leaseFile, []byte(data), 0644)).ShouldNot(HaveOccurred())
		time.Sleep(100 * time.Millisecond) // give system time to sync write change

		Expect(hook.LastEntry().Message).Should(Equal("A new lease has been written to the lease file with wrong data"))
		Expect(hook.LastEntry().Data["iface"]).Should(Equal(iface))
		Expect(hook.LastEntry().Data["ip"]).Should(Equal(ip))
	})

	It("wrong_ip", func() {
		logger, hook := test.NewNullLogger()

		iface := "wrong_ip"
		ip := "172.99.0.72"
		data := createLeaseData(iface, ip)

		Expect(WatchLeaseFile(logger, leaseFile, iface, "172.99.0.16")).ShouldNot(HaveOccurred())
		Expect(ioutil.WriteFile(leaseFile, []byte(data), 0644)).ShouldNot(HaveOccurred())
		time.Sleep(100 * time.Millisecond) // give system time to sync write change

		Expect(hook.LastEntry().Message).Should(Equal("A new lease has been written to the lease file with wrong data"))
		Expect(hook.LastEntry().Data["iface"]).Should(Equal(iface))
		Expect(hook.LastEntry().Data["ip"]).Should(Equal(ip))
	})

	It("valid_lease_file", func() {
		logger, hook := test.NewNullLogger()

		iface := "valid_lease_file"
		ip := "172.99.0.72"
		data := createLeaseData(iface, ip)

		Expect(WatchLeaseFile(logger, leaseFile, iface, ip)).ShouldNot(HaveOccurred())
		Expect(ioutil.WriteFile(leaseFile, []byte(data), 0644)).ShouldNot(HaveOccurred())
		time.Sleep(100 * time.Millisecond) // give system time to sync write change

		Expect(hook.LastEntry().Message).Should(Equal("A new lease has been written to the lease file with the right data"))
		Expect(hook.LastEntry().Data["iface"]).Should(Equal(iface))
		Expect(hook.LastEntry().Data["ip"]).Should(Equal(ip))
	})

	It("valid_multiple_leases", func() {
		logger, hook := test.NewNullLogger()

		iface := "valid_multiple_leases"
		ip := "172.99.0.72"
		data := ""

		Expect(WatchLeaseFile(logger, leaseFile, iface, ip)).ShouldNot(HaveOccurred())

		for range []int{1, 2, 3} {
			data += createLeaseData(iface, ip)

			Expect(ioutil.WriteFile(leaseFile, []byte(data), 0644)).ShouldNot(HaveOccurred())
			time.Sleep(100 * time.Millisecond) // give system time to sync write change

			Expect(hook.LastEntry().Message).Should(Equal("A new lease has been written to the lease file with the right data"))
			Expect(hook.LastEntry().Data["iface"]).Should(Equal(iface))
			Expect(hook.LastEntry().Data["ip"]).Should(Equal(ip))
		}
	})

	It("invalid_multiple_leases", func() {
		logger, hook := test.NewNullLogger()

		valid_lease := map[string]string{"1": "1.1.1.1"}
		data := ""

		invalid_leases := map[string]string{"2": "2.2.2.2", "3": "3.3.3.3"}

		Expect(WatchLeaseFile(logger, leaseFile, "1", valid_lease["1"])).ShouldNot(HaveOccurred())

		By("first_valid_lease", func() {
			data += createLeaseData("1", valid_lease["1"])

			Expect(ioutil.WriteFile(leaseFile, []byte(data), 0644)).ShouldNot(HaveOccurred())
			time.Sleep(100 * time.Millisecond) // give system time to sync write change

			Expect(hook.LastEntry().Message).Should(Equal("A new lease has been written to the lease file with the right data"))
			Expect(hook.LastEntry().Data["iface"]).Should(Equal("1"))
			Expect(hook.LastEntry().Data["ip"]).Should(Equal(valid_lease["1"]))
		})

		By("other_invalid_leases", func() {
			for iface, ip := range invalid_leases {
				data += createLeaseData(iface, ip)

				Expect(ioutil.WriteFile(leaseFile, []byte(data), 0644)).ShouldNot(HaveOccurred())
				time.Sleep(100 * time.Millisecond) // give system time to sync write change

				Expect(hook.LastEntry().Message).Should(Equal("A new lease has been written to the lease file with wrong data"))
				Expect(hook.LastEntry().Data["iface"]).Should(Equal(iface))
				Expect(hook.LastEntry().Data["ip"]).Should(Equal(ip))
			}
		})

		By("another_valid_lease", func() {
			data += createLeaseData("1", valid_lease["1"])

			Expect(ioutil.WriteFile(leaseFile, []byte(data), 0644)).ShouldNot(HaveOccurred())
			time.Sleep(100 * time.Millisecond) // give system time to sync write change

			Expect(hook.LastEntry().Message).Should(Equal("A new lease has been written to the lease file with the right data"))
			Expect(hook.LastEntry().Data["iface"]).Should(Equal("1"))
			Expect(hook.LastEntry().Data["ip"]).Should(Equal(valid_lease["1"]))
		})
	})

	AfterEach(func() {
		_ = os.RemoveAll(leaseFile)
	})
})

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
	  }
	`, ifaceName, ip)
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
	_ = getOutput("pkill dhclient || true")
	_ = getOutput(fmt.Sprintf("rm -rf %s", getLeaseFile(cfgPath, "*")))
}

func generateUUID() string {
	return getOutput("uuidgen")
}

func generateIP() strfmt.IPv4 {
	buf := make([]byte, 4)
	_, err := rand.Read(buf)
	Expect(err).ShouldNot(HaveOccurred())

	return strfmt.IPv4(fmt.Sprintf("%d.%d.%d.%d", buf[0], buf[1], buf[2], buf[3]))
}

func generateMacString() strfmt.MAC {
	var MacPrefixQumranet = [...]byte{0x00, 0x1A, 0x4A}

	buf := make([]byte, 3)
	_, err := rand.Read(buf)
	Expect(err).ShouldNot(HaveOccurred())

	return strfmt.MAC(fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		MacPrefixQumranet[0], MacPrefixQumranet[1], MacPrefixQumranet[2], buf[0], buf[1], buf[2]))
}

func generateMac() net.HardwareAddr {
	mac, err := net.ParseMAC(generateMacString().String())
	Expect(err).ShouldNot(HaveOccurred())

	return mac
}

func getIPFromLeaseFile(cfgPath, name string) string {
	info, err := os.Stat(getLeaseFile(cfgPath, name))
	Expect(err).ShouldNot(HaveOccurred())
	Expect(info.Mode().IsRegular()).Should(BeTrue())
	return getOutput(fmt.Sprintf("cat %s | awk '$1==\"fixed-address\" {print $2}' | tr -d ';'", getLeaseFile(cfgPath, name)))
}

func getInterfaceFromLeaseFile(cfgPath, name string) string {
	info, err := os.Stat(getLeaseFile(cfgPath, name))
	Expect(err).ShouldNot(HaveOccurred())
	Expect(info.Mode().IsRegular()).Should(BeTrue())
	return getOutput(fmt.Sprintf("cat %s | awk '$1==\"interface\" {print $2}' | tr -d '\";'", getLeaseFile(cfgPath, name)))
}

func getOutput(command string) string {
	cmd := exec.Command("bash", "-c", command)
	out, err := cmd.Output()
	if err != nil {
		log.Fatal(command, ": ", err)
	}

	return strings.TrimSpace(string(out))
}

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Leases tests")
}
