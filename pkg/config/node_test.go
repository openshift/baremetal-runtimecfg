package config

import (
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

	testNodeDualStack1 = &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "testNode"},
		Status: v1.NodeStatus{Addresses: []v1.NodeAddress{
			{Type: "InternalIP", Address: "192.168.1.99"},
			{Type: "InternalIP", Address: "fd00::5"},
			{Type: "ExternalIP", Address: "172.16.1.99"},
		}}}
	testNodeDualStack2 = &v1.Node{

		Status: v1.NodeStatus{Addresses: []v1.NodeAddress{
			{Type: "InternalIP", Address: "192.168.1.99"},
			{Type: "ExternalIP", Address: "172.16.1.99"},
		}},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "testNode",
			Annotations: testOvnHostAddressesAnnotation,
		},
	}
	testNodeDualStack3 = &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "testNode",
			Annotations: testOvnHostAddressesAnnotation,
		},
	}
	testNodeSingleStackV4 = &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "testNode"},
		Status: v1.NodeStatus{Addresses: []v1.NodeAddress{
			{Type: "InternalIP", Address: "192.168.1.99"},
			{Type: "ExternalIP", Address: "172.16.1.99"},
		}}}
	testNodeSingleStackV6 = &v1.Node{
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
		res, err := getNodeIpForRequestedIpStack(&v1.Node{}, []string{testApiVipV4, testIngressVipV4}, testMachineNetworkV4)
		Expect(res).To(Equal(""))
		Expect(err).To(BeNil())
	})

	It("empty for node with IPs and empty VIP requested", func() {
		res, err := getNodeIpForRequestedIpStack(testNodeSingleStackV4, []string{}, testMachineNetworkV4)
		Expect(res).To(Equal(""))
		Expect(err.Error()).To(Equal("for node testNode requested NodeIP detection with empty filterIP list. Cannot detect IP stack"))
	})
})

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config tests")
}
