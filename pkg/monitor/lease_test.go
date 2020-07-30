package monitor

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/syndtr/gocapability/capability"
	"github.com/vishvananda/netlink"
)

const LeaseTime = 5 * time.Second

var _ = Describe("lease_vip", func() {
	var (
		realIP    net.IP
		realIface net.Interface
		testIface *net.Interface
		testIP    net.IP
		testName  string
		err       error
		cfgPath   string
	)

	BeforeEach(func() {
		if !hasCap(capability.CAP_NET_ADMIN, capability.CAP_NET_RAW) {
			Skip("Must run with capabilities: CAP_NET_ADMIN, CAP_NET_RAW")
		}

		host, _ := os.Hostname()
		addrs, _ := net.LookupIP(host)
		for _, addr := range addrs {
			if ipv4 := addr.To4(); ipv4 != nil {
				realIP = ipv4

				testAddr := append(addr[:15], (addr[15] + 1))
				testIP = testAddr.To4()

				realIface, _, err = config.GetInterfaceAndNonVIPAddr([]net.IP{realIP})
				Expect(err).ShouldNot(HaveOccurred())
			}
		}

		testName = generateUUID()[:4]

		file, err := ioutil.TempFile("", "config")
		Expect(err).ShouldNot(HaveOccurred())
		cfgPath = file.Name()
	})

	Describe("leaseInterface", func() {
		It("happy_flow", func() {
			testIface, err = leaseInterface(realIface.Name, testName)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("different_names", func() {
			testIface, err = leaseInterface(realIface.Name, testName)
			Expect(err).ShouldNot(HaveOccurred())

			iface2, err := leaseInterface(realIface.Name, generateUUID()[:4])
			Expect(err).ShouldNot(HaveOccurred())
			Expect(iface2.Name).ShouldNot(Equal(realIface.Name))
			Expect(iface2.Name).ShouldNot(Equal(testIface.Name))
			Expect(iface2.HardwareAddr).ShouldNot(Equal(realIface.HardwareAddr))
			Expect(iface2.HardwareAddr).ShouldNot(Equal(testIface.HardwareAddr))

			deleteInterface(iface2.Name)
		})

		It("predefined_macvlan", func() {
			testIface, err = leaseInterface(realIface.Name, testName)
			Expect(err).ShouldNot(HaveOccurred())

			iface2, err := leaseInterface(realIface.Name, testName)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(iface2.Name).Should(Equal(testIface.Name))
			Expect(iface2.HardwareAddr).Should(Equal(testIface.HardwareAddr))
		})

		AfterEach(func() {
			Expect(testIface.Name).ShouldNot(Equal(realIface.Name))
			Expect(testIface.HardwareAddr).ShouldNot(Equal(realIface.HardwareAddr))

			iface, err := net.InterfaceByName(testName)
			Expect(err).ShouldNot(HaveOccurred())

			// Create new interface without allocating an IP - That's keepalived responsibility
			addrs, err := iface.Addrs()
			Expect(iface.HardwareAddr).Should(Equal(testIface.HardwareAddr))
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(addrs)).Should(Equal(0))
		})
	})

	Describe("leaseVIP", func() {
		BeforeEach(func() {
			Expect(leaseVIP(cfgPath, realIface.Name, testName)).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)
		})

		It("happy_flow", func() {
			Expect(getInterfaceFromLeaseFile(cfgPath, testName)).Should(Equal(testName))
			Expect(getIPFromLeaseFile(cfgPath, testName)).ShouldNot(BeEmpty())
		})

		It("multiple_vips", func() {
			for i := 2; i < 5; i++ {
				Expect(leaseVIP(cfgPath, realIface.Name, testName+strconv.Itoa(i))).ShouldNot(HaveOccurred())
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
			Expect(leaseVIP(cfgPath, realIface.Name, newName)).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)
			Expect(getIPFromLeaseFile(cfgPath, newName)).ShouldNot(Equal(ip))
			deleteInterface(newName)
		})

		It("same_mac_same_ip", func() {
			ip := getIPFromLeaseFile(cfgPath, testName)

			cleanEnv(cfgPath)

			Expect(leaseVIP(cfgPath, realIface.Name, testName)).ShouldNot(HaveOccurred())
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
		testName = "test"
		Expect(leaseVIP(cfgPath, realIface.Name, testName)).ShouldNot(HaveOccurred())

		time.Sleep(LeaseTime)
		Expect(getIPFromLeaseFile(cfgPath, testName)).Should(Equal("172.99.0.55"))
	})

	Describe("leaseVIPs", func() {
		It("happy_flow", func() {
			clusterName := generateUUID()
			vips := []VIP{
				{"api", testIP},
				{"ingress", testIP},
			}
			Expect(leaseVIPs(cfgPath, clusterName, vips)).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)

			for _, vip := range vips {
				Expect(getInterfaceFromLeaseFile(cfgPath, GetClusterInterfaceName(clusterName, vip.name))).Should(
					Equal(GetClusterInterfaceName(clusterName, vip.name)))

				deleteInterface(GetClusterInterfaceName(clusterName, vip.name))
			}
		})

		It("cluster_long_name", func() {
			clusterName := generateUUID()
			vips := []VIP{
				{generateUUID(), testIP},
			}
			Expect(leaseVIPs(cfgPath, clusterName, vips)).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)

			for _, vip := range vips {
				Expect(getInterfaceFromLeaseFile(cfgPath, GetClusterInterfaceName(clusterName, vip.name))).Should(
					Equal(GetClusterInterfaceName(clusterName, vip.name)))

				deleteInterface(GetClusterInterfaceName(clusterName, vip.name))
			}
		})

		It("short_name", func() {
			clusterName := generateUUID()[:1]
			vips := []VIP{
				{generateUUID()[:1], testIP},
			}
			Expect(leaseVIPs(cfgPath, clusterName, vips)).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)

			for _, vip := range vips {
				Expect(getInterfaceFromLeaseFile(cfgPath, GetClusterInterfaceName(clusterName, vip.name))).Should(
					Equal(GetClusterInterfaceName(clusterName, vip.name)))

				deleteInterface(GetClusterInterfaceName(clusterName, vip.name))
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

func getIPFromLeaseFile(cfgPath, name string) string {
	return getOutput(fmt.Sprintf("cat %s | awk '$1==\"fixed-address\" {print $2}' | tr -d ';'", getLeaseFile(cfgPath, name)))
}

func getInterfaceFromLeaseFile(cfgPath, name string) string {
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
	RunSpecs(t, "leases tests")
}
