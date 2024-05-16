package config

import (
	"net"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	testOvnHostAddressesAnnotation = map[string]string{
		"k8s.ovn.org/host-addresses": "[\"192.168.1.102\",\"192.168.1.99\",\"192.168.1.101\",\"fd00::101\",\"2001:db8::49a\",\"fd00::102\",\"fd00::5\",\"fd69::2\"]",
	}

	testNodeDualStack1 = v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "testNode"},
		Status: v1.NodeStatus{Addresses: []v1.NodeAddress{
			{Type: "InternalIP", Address: "192.168.1.99"},
			{Type: "InternalIP", Address: "fd00::5"},
			{Type: "ExternalIP", Address: "172.16.1.99"},
		}}}
	testNodeDualStack2 = v1.Node{

		Status: v1.NodeStatus{Addresses: []v1.NodeAddress{
			{Type: "InternalIP", Address: "192.168.1.99"},
			{Type: "ExternalIP", Address: "172.16.1.99"},
		}},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "testNode",
			Annotations: testOvnHostAddressesAnnotation,
		},
	}
	testNodeDualStack3 = v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "testNode",
			Annotations: testOvnHostAddressesAnnotation,
		},
	}
	testNodeSingleStackV4 = v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "testNode"},
		Status: v1.NodeStatus{Addresses: []v1.NodeAddress{
			{Type: "InternalIP", Address: "192.168.1.99"},
			{Type: "ExternalIP", Address: "172.16.1.99"},
		}}}
	testNodeSingleStackV6 = v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "testNode"},
		Status: v1.NodeStatus{Addresses: []v1.NodeAddress{
			{Type: "InternalIP", Address: "fd00::5"},
			{Type: "ExternalIP", Address: "2001:db8::49a"},
		}}}

	testMachineNetworkV4 = "192.168.1.0/24"
	testMachineNetworkV6 = "fd00::5/64"
	testApiVipV4         = "192.168.1.101"
	testApiVipV6         = "fd00::101"
	testIngressVipV4     = "192.168.1.102"
	testIngressVipV6     = "fd00::102"
)

var _ = Describe("getNodePeersForIpStack", func() {
	Context("for dual-stack node", func() {
		Context("with address only in status", func() {
			It("matches an IPv4 VIP", func() {
				res, err := getNodeIpForRequestedIpStack(testNodeDualStack1, []string{testApiVipV4, testIngressVipV4}, testMachineNetworkV4)
				Expect(res).To(Equal("192.168.1.99"))
				Expect(err).To(BeNil())
			})
			It("matches an IPv6 VIP", func() {
				res, err := getNodeIpForRequestedIpStack(testNodeDualStack1, []string{testApiVipV6, testIngressVipV6}, testMachineNetworkV6)
				Expect(res).To(Equal("fd00::5"))
				Expect(err).To(BeNil())
			})
		})

		Context("with address only in OVN annotation", func() {
			It("matches an IPv4 VIP", func() {
				res, err := getNodeIpForRequestedIpStack(testNodeDualStack3, []string{testApiVipV4, testIngressVipV4}, testMachineNetworkV4)
				Expect(res).To(Equal("192.168.1.99"))
				Expect(err).To(BeNil())
			})
			It("matches an IPv6 VIP", func() {
				res, err := getNodeIpForRequestedIpStack(testNodeDualStack3, []string{testApiVipV6, testIngressVipV6}, testMachineNetworkV6)
				Expect(res).To(Equal("fd00::5"))
				Expect(err).To(BeNil())
			})
		})

		Context("with address in status and OVN annotation", func() {
			It("matches an IPv4 VIP", func() {
				res, err := getNodeIpForRequestedIpStack(testNodeDualStack2, []string{testApiVipV4, testIngressVipV4}, testMachineNetworkV4)
				Expect(res).To(Equal("192.168.1.99"))
				Expect(err).To(BeNil())
			})
			It("matches an IPv6 VIP", func() {
				res, err := getNodeIpForRequestedIpStack(testNodeDualStack2, []string{testApiVipV6, testIngressVipV6}, testMachineNetworkV6)
				Expect(res).To(Equal("fd00::5"))
				Expect(err).To(BeNil())
			})
		})
	})

	Context("for single-stack v4 node", func() {
		It("matches an IPv4 VIP", func() {
			res, err := getNodeIpForRequestedIpStack(testNodeSingleStackV4, []string{testApiVipV4, testIngressVipV4}, testMachineNetworkV4)
			Expect(res).To(Equal("192.168.1.99"))
			Expect(err).To(BeNil())
		})
		It("empty for IPv6 VIP", func() {
			res, err := getNodeIpForRequestedIpStack(testNodeSingleStackV4, []string{testApiVipV6, testIngressVipV6}, testMachineNetworkV6)
			Expect(res).To(Equal(""))
			Expect(err).To(BeNil())
		})
	})

	Context("for single-stack v6 node", func() {
		It("empty for IPv4 VIP", func() {
			res, err := getNodeIpForRequestedIpStack(testNodeSingleStackV6, []string{testApiVipV4, testIngressVipV4}, testMachineNetworkV4)
			Expect(res).To(Equal(""))
			Expect(err).To(BeNil())
		})
		It("matches an IPv6 VIP", func() {
			res, err := getNodeIpForRequestedIpStack(testNodeSingleStackV6, []string{testApiVipV6, testIngressVipV6}, testMachineNetworkV6)
			Expect(res).To(Equal("fd00::5"))
			Expect(err).To(BeNil())
		})
	})

	It("empty for empty node", func() {
		res, err := getNodeIpForRequestedIpStack(v1.Node{}, []string{testApiVipV4, testIngressVipV4}, testMachineNetworkV4)
		Expect(res).To(Equal(""))
		Expect(err).To(BeNil())
	})

	It("empty for node with IPs and empty VIP requested", func() {
		res, err := getNodeIpForRequestedIpStack(testNodeSingleStackV4, []string{}, testMachineNetworkV4)
		Expect(res).To(Equal(""))
		Expect(err.Error()).To(Equal("for node testNode requested NodeIP detection with empty filterIP list. Cannot detect IP stack"))
	})
})

// Following needed for cloud LB IP tests
var (
	testKubeconfigPath    = "/test/path/kubeconfig"
	testClusterConfigPath = "/test/path/clusterConfig"
	testResolvConfPath    = "/tmp/resolvConf"
	testApiLBIPv4         = net.ParseIP("192.168.0.111")
	testApiIntLBIPv4      = net.ParseIP("10.10.10.20")
	testIngressOneIPv4    = net.ParseIP("192.168.20.140")
	testIngressTwoIPv4    = net.ParseIP("10.10.10.40")
	testClusterLBConfig   = ClusterLBConfig{
		ApiLBIPs:     []net.IP{testApiLBIPv4},
		ApiIntLBIPs:  []net.IP{testApiIntLBIPv4},
		IngressLBIPs: []net.IP{testIngressOneIPv4, testIngressTwoIPv4}}
	expectedApiLBIPv4      = "192.168.0.111"
	expectedApiIntLBIPv4   = "10.10.10.20"
	expectedIngressOneIPv4 = "192.168.20.140"
	expectedIngressTwoIPv4 = "10.10.10.40"

	emptyLBIPs = []net.IP{}
)

var _ = Describe("PopulateCloudLBIPAddresses", func() {
	Context("for IPV4 Cloud LB IPs", func() {
		Context("with multiple Ingress LB IPs", func() {
			It("matches IPv4 API and Ingress LB IPs", func() {
				newNode := Node{}
				newNode, err := PopulateCloudLBIPAddresses(testClusterLBConfig, newNode)
				Expect(newNode.Cluster.APILBIPs[0]).To(Equal(expectedApiLBIPv4))
				Expect(newNode.Cluster.IngressLBIPs[1]).To(Equal(expectedIngressTwoIPv4))
				Expect(newNode.Cluster.CloudLBRecordType).To(Equal("A"))
				Expect(err).To(BeNil())
			})
			It("handles Empty API LB IPs", func() {
				newNode := Node{}
				// Empty API LB IP
				emptyApiLBIPLBConfig := ClusterLBConfig{
					ApiLBIPs:     []net.IP{},
					ApiIntLBIPs:  []net.IP{testApiIntLBIPv4},
					IngressLBIPs: []net.IP{testIngressOneIPv4}}
				newNode, err := PopulateCloudLBIPAddresses(emptyApiLBIPLBConfig, newNode)
				Expect(len(newNode.Cluster.APILBIPs)).To(Equal(len(emptyLBIPs)))
				Expect(newNode.Cluster.APIIntLBIPs[0]).To(Equal(expectedApiIntLBIPv4))
				Expect(newNode.Cluster.IngressLBIPs[0]).To(Equal(expectedIngressOneIPv4))
				Expect(newNode.Cluster.CloudLBRecordType).To(Equal("A"))
				Expect(err).To(BeNil())
			})
			It("handles Empty API Int LB IPs", func() {
				newNode := Node{}
				// Empty API-Int LB IP
				emptyApiIntLBIPLBConfig := ClusterLBConfig{
					ApiLBIPs:     []net.IP{testApiLBIPv4},
					ApiIntLBIPs:  []net.IP{},
					IngressLBIPs: []net.IP{testIngressOneIPv4}}
				newNode, err := PopulateCloudLBIPAddresses(emptyApiIntLBIPLBConfig, newNode)
				Expect(newNode.Cluster.APILBIPs[0]).To(Equal(expectedApiLBIPv4))
				Expect(len(newNode.Cluster.APIIntLBIPs)).To(Equal(len(emptyLBIPs)))
				Expect(newNode.Cluster.IngressLBIPs[0]).To(Equal(expectedIngressOneIPv4))
				Expect(newNode.Cluster.CloudLBRecordType).To(Equal("A"))
				Expect(err).To(BeNil())
			})
			It("handles Empty Ingress LB IPs", func() {
				newNode := Node{}
				// Empty Ingress LB IP
				emptyIngressLBIPLBConfig := ClusterLBConfig{
					ApiLBIPs:     []net.IP{testApiLBIPv4},
					ApiIntLBIPs:  []net.IP{testApiIntLBIPv4},
					IngressLBIPs: []net.IP{}}
				newNode, err := PopulateCloudLBIPAddresses(emptyIngressLBIPLBConfig, newNode)
				Expect(newNode.Cluster.APILBIPs[0]).To(Equal(expectedApiLBIPv4))
				Expect(newNode.Cluster.APIIntLBIPs[0]).To(Equal(expectedApiIntLBIPv4))
				Expect(len(newNode.Cluster.IngressLBIPs)).To(Equal(len(emptyLBIPs)))
				Expect(newNode.Cluster.CloudLBRecordType).To(Equal("A"))
				Expect(err).To(BeNil())
			})
			It("handles Empty All LB IPs", func() {
				newNode := Node{}
				// Empty All LB IPs
				emptyAllLBIPLBConfig := ClusterLBConfig{
					ApiLBIPs:     []net.IP{},
					ApiIntLBIPs:  []net.IP{},
					IngressLBIPs: []net.IP{}}
				newNode, err := PopulateCloudLBIPAddresses(emptyAllLBIPLBConfig, newNode)
				Expect(len(newNode.Cluster.APILBIPs)).To(Equal(len(emptyLBIPs)))
				Expect(len(newNode.Cluster.APIIntLBIPs)).To(Equal(len(emptyLBIPs)))
				Expect(len(newNode.Cluster.IngressLBIPs)).To(Equal(len(emptyLBIPs)))
				Expect(newNode.Cluster.CloudLBRecordType).To(Equal("A"))
				Expect(err).To(BeNil())
			})
		})
	})
})

var _ = Describe("updateNodewithCloudLBIPs", func() {
	Context("for IPV4 Cloud LB IPs", func() {
		Context("with one LB IP per Node", func() {
			It("matches IPv4 API and Ingress LB IPs", func() {
				updateNode := Node{}
				updateNode, err := updateNodewithCloudInfo(testApiLBIPv4, testApiIntLBIPv4, testIngressOneIPv4, testResolvConfPath, updateNode)
				Expect(updateNode.Cluster.APIIntLBIPs[0]).To(Equal(expectedApiIntLBIPv4))
				Expect(updateNode.Cluster.IngressLBIPs[0]).To(Equal(expectedIngressOneIPv4))
				Expect(updateNode.Cluster.CloudLBRecordType).To(Equal("A"))
				Expect(len(updateNode.DNSUpstreams)).To(Equal(1))
				Expect(updateNode.DNSUpstreams[0]).To(Equal("169.254.169.254"))
				Expect(err).To(BeNil())
			})
			It("handles nil API LB IP", func() {
				updateNode := Node{}
				updateNode, err := updateNodewithCloudInfo(nil, testApiIntLBIPv4, testIngressOneIPv4, testResolvConfPath, updateNode)
				Expect(len(updateNode.Cluster.APILBIPs)).To(Equal(0))
				Expect(updateNode.Cluster.APIIntLBIPs[0]).To(Equal(expectedApiIntLBIPv4))
				Expect(updateNode.Cluster.IngressLBIPs[0]).To(Equal(expectedIngressOneIPv4))
				Expect(updateNode.Cluster.CloudLBRecordType).To(Equal("A"))
				Expect(err).To(BeNil())
			})
			It("handles nil API-Int LB IP", func() {
				updateNode := Node{}
				updateNode, err := updateNodewithCloudInfo(testApiLBIPv4, nil, testIngressOneIPv4, testResolvConfPath, updateNode)
				Expect(updateNode.Cluster.APILBIPs[0]).To(Equal(expectedApiLBIPv4))
				Expect(len(updateNode.Cluster.APIIntLBIPs)).To(Equal(0))
				Expect(updateNode.Cluster.IngressLBIPs[0]).To(Equal(expectedIngressOneIPv4))
				Expect(updateNode.Cluster.CloudLBRecordType).To(Equal("A"))
				Expect(err).To(BeNil())
			})
			It("handles nil API and Ingress LBs IP", func() {
				updateNode := Node{}
				updateNode, err := updateNodewithCloudInfo(nil, testApiIntLBIPv4, nil, testResolvConfPath, updateNode)
				Expect(updateNode.Cluster.APIIntLBIPs[0]).To(Equal(expectedApiIntLBIPv4))
				Expect(len(updateNode.Cluster.APILBIPs)).To(Equal(0))
				Expect(len(updateNode.Cluster.IngressLBIPs)).To(Equal(0))
				Expect(updateNode.Cluster.CloudLBRecordType).To(Equal("A"))
				Expect(err).To(BeNil())
			})
		})
	})
})

func createTempResolvConf() {
	f, _ := os.Create("/tmp/resolvConf")
	defer f.Close()

	f.WriteString("# Generated by NetworkManager\nsearch us-central1-a.c.openshift-qe.internal c.openshift-qe.internal google.internal\nnameserver 169.254.169.254\n")
	f.Sync()
}

func deleteTempResolvConf() {
	os.Remove("/tmp/resolvConf")
}

func Test(t *testing.T) {
	createTempResolvConf()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config tests")
	deleteTempResolvConf()
}
