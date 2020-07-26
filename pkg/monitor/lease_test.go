package monitor

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/milosgajdos/tenus"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/syndtr/gocapability/capability"
)

const LeaseTime = 3 * time.Second

var _ = Describe("lease_vip", func() {
	var (
		realIP    net.IP
		realIface net.Interface
		testIface *net.Interface
		testIP    net.IP
		testName  string
		err       error
	)

	BeforeEach(func() {
		if !hasCap(capability.CAP_NET_ADMIN) {
			Skip("Must run with CAP_NET_ADMIN capability")
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

		testName = uuid.New().String()[:4]
	})

	Describe("leaseInterface", func() {
		It("happy_flow", func() {
			testIface, err = leaseInterface(testName, testIP)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("different_names", func() {
			testIface, err = leaseInterface(testName, testIP)
			Expect(err).ShouldNot(HaveOccurred())

			iface2, err := leaseInterface(uuid.New().String()[:4], testIP)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(iface2.Name).ShouldNot(Equal(realIface.Name))
			Expect(iface2.Name).ShouldNot(Equal(testIface.Name))
			Expect(iface2.HardwareAddr).ShouldNot(Equal(realIface.HardwareAddr))
			Expect(iface2.HardwareAddr).ShouldNot(Equal(testIface.HardwareAddr))

			Expect(tenus.DeleteLink(iface2.Name)).ShouldNot(HaveOccurred())
		})

		It("predefined_macvlan", func() {
			testIface, err = leaseInterface(testName, testIP)
			Expect(err).ShouldNot(HaveOccurred())

			iface2, err := leaseInterface(testName, testIP)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(iface2.Name).Should(Equal(testIface.Name))
			Expect(iface2.HardwareAddr).Should(Equal(testIface.HardwareAddr))
		})

		AfterEach(func() {
			Expect(testIface.Name).ShouldNot(Equal(realIface.Name))
			Expect(testIface.HardwareAddr).ShouldNot(Equal(realIface.HardwareAddr))
		})
	})

	Describe("leaseVIP", func() {
		BeforeEach(func() {
			Expect(leaseVIP(testName, testIP)).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)
		})

		It("happy_flow", func() {
			Expect(getLeaseInterface()).Should(Equal(fmt.Sprintf(leaseInterfaceTemplate, testName)))
			Expect(getLeaseFixedAddress()).ShouldNot(BeEmpty())
		})

		It("multiple_vips", func() {
			for i := 2; i < 5; i++ {
				Expect(leaseVIP(testName+strconv.Itoa(i), testIP)).ShouldNot(HaveOccurred())
			}

			time.Sleep(LeaseTime)

			for i := 2; i < 5; i++ {
				Expect(tenus.DeleteLink(fmt.Sprintf(leaseInterfaceTemplate, testName+strconv.Itoa(i)))).ShouldNot(HaveOccurred())
			}
		})

		It("different_mac_different_ip", func() {
			ip := getLeaseFixedAddress()

			cleanEnv()

			Expect(leaseVIP(uuid.New().String()[:4], testIP)).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)
			Expect(getLeaseFixedAddress()).ShouldNot(Equal(ip))
		})

		It("same_mac_same_ip", func() {
			ip := getLeaseFixedAddress()

			cleanEnv()

			Expect(leaseVIP(testName, testIP)).ShouldNot(HaveOccurred())
			time.Sleep(LeaseTime)
			Expect(getLeaseFixedAddress()).Should(Equal(ip))
		})
	})

	It("server_hardcoded_host", func() {
		testName = "test"
		Expect(leaseVIP(testName, testIP)).ShouldNot(HaveOccurred())

		time.Sleep(LeaseTime)
		Expect(getLeaseFixedAddress()).Should(Equal("172.21.0.55"))
	})

	AfterEach(func() {
		cleanEnv()

		iface, err := net.InterfaceByName(fmt.Sprintf(leaseInterfaceTemplate, testName))
		Expect(err).ShouldNot(HaveOccurred())

		// Create new interface / lease without allocating an IP - That's keepalived responsibility
		addrs, err := iface.Addrs()
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(addrs)).Should(Equal(0))

		// Cleanup
		_ = tenus.DeleteLink(testIface.Name)
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

func cleanEnv() {
	_ = getOutput("pkill dhclient || true")
	_ = getOutput(fmt.Sprintf("rm -rf %s", leaseFile))
}

func getLeaseFixedAddress() string {
	return getOutput(fmt.Sprintf("cat %s | awk '$1==\"fixed-address\" {print $2}' | tr -d ';\n'", leaseFile))
}

func getLeaseInterface() string {
	return getOutput(fmt.Sprintf("cat %s | awk '$1==\"interface\" {print $2}' | tr -d '\";\n'", leaseFile))
}

func getOutput(command string) string {
	cmd := exec.Command("bash", "-c", command)
	out, err := cmd.Output()
	if err != nil {
		log.Fatal(command, " : ", err)
	}

	// fmt.Println(cmd.Args)
	return string(out)
}

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "leases tests")
}
