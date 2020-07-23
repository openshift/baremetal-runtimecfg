package monitor

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"testing"

	"github.com/google/uuid"
	"github.com/milosgajdos/tenus"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/baremetal-runtimecfg/pkg/config"
)

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

		printIPs()
		printMACs()
	})

	Describe("createVIPInterface", func() {
		It("happy_flow", func() {
			testIface, err = createVIPInterface(testName, testIP)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("different_names", func() {
			testIface, err = createVIPInterface(testName, testIP)
			Expect(err).ShouldNot(HaveOccurred())

			iface2, err := createVIPInterface(uuid.New().String()[:4], testIP)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(iface2.Name).ShouldNot(Equal(realIface.Name))
			Expect(iface2.Name).ShouldNot(Equal(testIface.Name))
			Expect(iface2.HardwareAddr).ShouldNot(Equal(realIface.HardwareAddr))
			Expect(iface2.HardwareAddr).ShouldNot(Equal(testIface.HardwareAddr))

			Expect(tenus.DeleteLink(iface2.Name)).ShouldNot(HaveOccurred())
		})

		It("same_names", func() {
			testIface, err = createVIPInterface(testName, testIP)
			Expect(err).ShouldNot(HaveOccurred())

			_, err = createVIPInterface(testName, testIP)
			Expect(err).Should(HaveOccurred())
		})

		AfterEach(func() {
			Expect(testIface.Name).ShouldNot(Equal(realIface.Name))
			Expect(testIface.HardwareAddr).ShouldNot(Equal(realIface.HardwareAddr))

			printIPs()
			printMACs()

			// TODO: Verify if there needs to be an IP for the new interface *before* dhclient
			// addrs, err := testIface.Addrs()
			// Expect(err).ShouldNot(HaveOccurred())
			// Expect(len(addrs)).Should(Equal(1))
			// Expect(addrs[0].String).Should(Equal(testIP.String()))

			Expect(tenus.DeleteLink(testIface.Name)).ShouldNot(HaveOccurred())
		})
	})

	// TODO: Set a DHCPd server https://hub.docker.com/r/networkboot/dhcpd
	// Describe("leaseVIP", func() {
	// 	It("happy_flow", func() {
	// 		Expect(leaseVIP(testName, testIP)).ShouldNot(HaveOccurred())
	// 	})
	// })
})

func printIPs() {
	cmd := exec.Command("ip", "-brief", "address")
	out, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(cmd.Args)
	fmt.Printf("%s\n", out)
}

func printMACs() {
	cmd := exec.Command("ip", "-brief", "link")
	out, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(cmd.Args)
	fmt.Printf("%s\n", out)
}

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "leases tests")
}
