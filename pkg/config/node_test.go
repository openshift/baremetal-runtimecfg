package config

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/installer/pkg/types"
	"github.com/openshift/installer/pkg/types/aws"
	"github.com/openshift/installer/pkg/types/azure"
	"github.com/openshift/installer/pkg/types/baremetal"
	"github.com/openshift/installer/pkg/types/gcp"
	"github.com/openshift/installer/pkg/types/none"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

var (
	testOvnHostAddressesAnnotation = map[string]string{
		"k8s.ovn.org/host-addresses": "[\"192.168.1.102\",\"192.168.1.99\",\"192.168.1.101\",\"fd00::101\",\"2001:db8::49a\",\"fd00::102\",\"fd00::5\",\"fd69::2\"]",
	}

	testOvnHostCidrsAnnotation = map[string]string{
		"k8s.ovn.org/host-cidrs": "[\"192.168.1.102/24\",\"192.168.1.99/24\",\"192.168.1.101/24\",\"fd00::101/128\",\"2001:db8::49a/64\",\"fd00::102/128\",\"fd00::5/128\",\"fd69::2/128\"]",
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
	testNodeDualStack4 = v1.Node{
		Status: v1.NodeStatus{Addresses: []v1.NodeAddress{
			{Type: "InternalIP", Address: "192.168.1.99"},
			{Type: "ExternalIP", Address: "172.16.1.99"},
		}},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "testNode",
			Annotations: testOvnHostCidrsAnnotation,
		},
	}
	testNodeDualStack5 = v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "testNode",
			Annotations: testOvnHostCidrsAnnotation,
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

func writeDNSFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverNodeIPs(t *testing.T) {
	cases := []struct {
		name    string
		primary string
		ipv4    string
		ipv6    string
		want    []DNSAddress
		wantErr bool
	}{
		{name: "IPv4 only", primary: "192.0.2.10\n", ipv4: "192.0.2.10", ipv6: "<missing>", want: []DNSAddress{{Address: "192.0.2.10", RecordType: "A"}}},
		{name: "IPv6 only", primary: "2001:db8::10", ipv4: "<missing>", ipv6: "2001:db8::10\n", want: []DNSAddress{{Address: "2001:db8::10", RecordType: "AAAA"}}},
		{name: "dual stack IPv4 primary", primary: "192.0.2.10", ipv4: "192.0.2.10", ipv6: "2001:db8::10", want: []DNSAddress{{Address: "192.0.2.10", RecordType: "A"}, {Address: "2001:db8::10", RecordType: "AAAA"}}},
		{name: "dual stack IPv6 primary", primary: "2001:db8::10", ipv4: "192.0.2.10", ipv6: "2001:db8::10", want: []DNSAddress{{Address: "2001:db8::10", RecordType: "AAAA"}, {Address: "192.0.2.10", RecordType: "A"}}},
		{name: "missing primary", primary: "<missing>", ipv4: "192.0.2.10", ipv6: "<missing>", wantErr: true},
		{name: "empty primary", primary: "", ipv4: "192.0.2.10", ipv6: "<missing>", wantErr: true},
		{name: "invalid primary", primary: "not-an-ip", ipv4: "192.0.2.10", ipv6: "<missing>", wantErr: true},
		{name: "primary family file is not required", primary: "192.0.2.10", ipv4: "<missing>", ipv6: "<missing>", want: []DNSAddress{{Address: "192.0.2.10", RecordType: "A"}}},
		{name: "primary family file is ignored", primary: "2001:db8::10", ipv4: "<missing>", ipv6: "", want: []DNSAddress{{Address: "2001:db8::10", RecordType: "AAAA"}}},
		{name: "invalid optional family", primary: "192.0.2.10", ipv4: "192.0.2.10", ipv6: "invalid", wantErr: true},
		{name: "wrong optional family", primary: "192.0.2.10", ipv4: "192.0.2.10", ipv6: "192.0.2.11", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, value string) string {
				path := filepath.Join(dir, name)
				if value != "<missing>" {
					if err := os.WriteFile(path, []byte(value), 0600); err != nil {
						t.Fatal(err)
					}
				}
				return path
			}

			got, err := discoverNodeIPs(write("primary-ip", tc.primary), write("ipv4", tc.ipv4), write("ipv6", tc.ipv6))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got addresses %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("addresses: got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestFilterDNSUpstreams(t *testing.T) {
	addresses := localAddresses{addresses: []netip.Addr{
		netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("2001:db8::10"),
	}}
	got := filterDNSUpstreams([]string{
		"127.0.0.1", "127.0.0.53", "::1", "0.0.0.0", "::", "192.0.2.10", "2001:db8::10", "invalid", "192.0.2.53", "192.0.2.53", "2001:0db8::53",
	}, addresses)
	want := []string{"192.0.2.53", "2001:db8::53"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("upstreams: got %#v, want %#v", got, want)
	}
}

func dnsKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	writeDNSFixture(t, path, `apiVersion: v1
kind: Config
current-context: test
contexts:
- name: test
  context:
    cluster: test
clusters:
- name: test
  cluster:
    server: https://api.cluster.example.invalid:6443
`)
	return path
}

func TestCollectInterfaceAddresses(t *testing.T) {
	interfaces := []net.Interface{{Index: 3, Name: "ens3"}, {Index: 4, Name: "ens4"}}
	calls := 0
	local, err := collectInterfaceAddresses(interfaces, func(iface *net.Interface) ([]net.Addr, error) {
		calls++
		cidrs := []string{"192.0.2.10/24", "192.0.2.11/24", "2001:db8::10/64", "fe80::10/64"}
		if iface.Name == "ens4" {
			cidrs = []string{"198.51.100.10/24", "fe80::10/64"}
		}
		var addresses []net.Addr
		for _, cidr := range cidrs {
			ip, prefix, err := net.ParseCIDR(cidr)
			if err != nil {
				t.Fatal(err)
			}
			prefix.IP = ip
			addresses = append(addresses, prefix)
		}
		return addresses, nil
	})
	if err != nil || calls != len(interfaces) {
		t.Fatalf("collector calls: %d, error: %v", calls, err)
	}
	want := []netip.Addr{
		netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("192.0.2.11"),
		netip.MustParseAddr("2001:db8::10"), netip.MustParseAddr("fe80::10%ens3"),
		netip.MustParseAddr("198.51.100.10"), netip.MustParseAddr("fe80::10%ens4"),
	}
	if !reflect.DeepEqual(local.addresses, want) || !reflect.DeepEqual(local.interfaceNames, map[int]string{3: "ens3", 4: "ens4"}) {
		t.Fatalf("unexpected interface snapshot: %#v", local)
	}
	errCollection := errors.New("interface address lookup failed")
	_, err = collectInterfaceAddresses(interfaces, func(*net.Interface) ([]net.Addr, error) { return nil, errCollection })
	if !errors.Is(err, errCollection) {
		t.Fatalf("lost collection error: %v", err)
	}
}

func TestFilterDNSUpstreamsScopes(t *testing.T) {
	local := localAddresses{
		addresses: []netip.Addr{
			netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("192.0.2.11"),
			netip.MustParseAddr("2001:db8::10"), netip.MustParseAddr("fe80::10%ens3"),
			netip.MustParseAddr("fe80::20"),
		},
		interfaceNames: map[int]string{3: "ens3", 4: "ens4"},
	}
	got := filterDNSUpstreams([]string{
		"127.0.0.53", "::ffff:127.0.0.1", "::1%ens3", "0.0.0.0", "::%ens3", "invalid",
		"192.0.2.10", "::ffff:192.0.2.11", "2001:0db8::10%ens3",
		"fe80::10%3", "fe80::10%ens3", "fe80::20%ens4",
		"fe80::10%4", "fe80::53%ens3", "fe80::53%3", "fe80::53%ens4",
		"2001:db8::53%ens3", "2001:0db8::53", "::ffff:192.0.2.53", "192.0.2.53",
	}, local)
	want := []string{"fe80::10%ens4", "fe80::53%ens3", "fe80::53%ens4", "2001:db8::53", "192.0.2.53"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("upstreams: got %v, want %v", got, want)
	}
}

func TestDNSUpstreamLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	input := "nameserver\n"
	want := make([]string, 0, maxDNSUpstreams)
	for i := 1; i <= maxDNSUpstreams+1; i++ {
		address := fmt.Sprintf("198.51.100.%d", i)
		input += "nameserver " + address + "\n"
		if i <= maxDNSUpstreams {
			want = append(want, address)
		}
	}
	writeDNSFixture(t, path, input)
	upstreams, err := getDNSUpstreams(path)
	if err != nil || !reflect.DeepEqual(upstreams, want) {
		t.Fatalf("parser upstreams: %v, error: %v", upstreams, err)
	}
	cloud, err := updateNodewithCloudInfo(nil, net.ParseIP("192.0.2.100"), nil, path, Node{})
	if err != nil || !reflect.DeepEqual(cloud.DNSUpstreams, want) {
		t.Fatalf("cloud upstreams: %v, error: %v", cloud.DNSUpstreams, err)
	}
}

func TestCloudDNSUpstreamsUseStrictFiltering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	writeDNSFixture(t, path, `nameserver 0.0.0.0
nameserver ::
nameserver 127.0.0.53
nameserver ::1
nameserver 192.0.2.53
nameserver ::ffff:192.0.2.53
nameserver 2001:0db8::53
nameserver 2001:db8::53
nameserver fe80::53%ens3
nameserver fe80::53%ens4
nameserver invalid
`)
	cloud, err := updateNodewithCloudInfo(nil, net.ParseIP("192.0.2.100"), nil, path, Node{})
	want := []string{"192.0.2.53", "2001:db8::53", "fe80::53%ens3", "fe80::53%ens4"}
	if err != nil || !reflect.DeepEqual(cloud.DNSUpstreams, want) {
		t.Fatalf("cloud upstreams: got %v, want %v, error: %v", cloud.DNSUpstreams, want, err)
	}
}

func TestGetFilteredDNSUpstreams(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	local := localAddresses{addresses: []netip.Addr{netip.MustParseAddr("192.0.2.10")}}
	for _, input := range []string{"", "nameserver\n", "nameserver 127.0.0.1\nnameserver 192.0.2.10\n"} {
		writeDNSFixture(t, path, input)
		if _, err := getFilteredDNSUpstreams(path, local); err == nil {
			t.Fatalf("accepted resolver input without usable upstreams: %q", input)
		}
	}
	input := "nameserver 127.0.0.1\n"
	for i := 1; i <= maxDNSUpstreams+1; i++ {
		input += fmt.Sprintf("nameserver 198.51.100.%d\n", i)
	}
	writeDNSFixture(t, path, input)
	got, err := getFilteredDNSUpstreams(path, local)
	if err != nil || len(got) != maxDNSUpstreams-1 || got[0] != "198.51.100.1" || got[len(got)-1] != "198.51.100.14" {
		t.Fatalf("expected filtering of the first 15 entries: %v, %v", got, err)
	}
}

func TestGetConfigWithNodeIPLocalInputs(t *testing.T) {
	dir, kubeconfig := t.TempDir(), dnsKubeconfig(t)
	paths := nodeIPPaths{filepath.Join(dir, "primary-ip"), filepath.Join(dir, "ipv4"), filepath.Join(dir, "ipv6")}
	resolv := filepath.Join(dir, "resolv.conf")
	writeDNSFixture(t, paths.primary, "2001:db8::10\n")
	writeDNSFixture(t, paths.ipv4, "192.0.2.10")
	writeDNSFixture(t, paths.ipv6, "2001:db8::10")
	writeDNSFixture(t, resolv, "nameserver 192.0.2.10\nnameserver 192.0.2.11\nnameserver 192.0.2.53\n")
	local := localAddresses{addresses: []netip.Addr{netip.MustParseAddr("192.0.2.11")}}
	var collectErr error
	calls := 0
	collect := func() (localAddresses, error) { calls++; return local, collectErr }
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cfg, err := getConfigWithNodeIP(ctx, kubeconfig, "", resolv, paths, collect)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || cfg.Cluster.Name != "cluster" || cfg.Cluster.Domain != "example.invalid" || cfg.ShortHostname == "" {
		t.Fatalf("unexpected local config: %#v, collector calls: %d", cfg, calls)
	}
	if cfg.NonVirtualIP != "2001:db8::10" || len(cfg.DNSAddresses) != 2 || cfg.DNSAddresses[0].RecordType != "AAAA" || !reflect.DeepEqual(cfg.DNSUpstreams, []string{"192.0.2.53"}) {
		t.Fatalf("unexpected DNS config: %#v", cfg)
	}
	if cfg.Configs == nil || len(*cfg.Configs) != 1 || (*cfg.Configs)[0].NonVirtualIP != cfg.NonVirtualIP {
		t.Fatal("missing single-node Configs shape")
	}

	writeDNSFixture(t, paths.primary, "192.0.2.12")
	writeDNSFixture(t, paths.ipv4, "192.0.2.12")
	if err := os.Remove(paths.ipv6); err != nil {
		t.Fatal(err)
	}
	local.addresses = []netip.Addr{netip.MustParseAddr("192.0.2.53")}
	cfg, err = getConfigWithNodeIP(ctx, kubeconfig, "", resolv, paths, collect)
	if err != nil || cfg.NonVirtualIP != "192.0.2.12" || len(cfg.DNSAddresses) != 1 || !reflect.DeepEqual(cfg.DNSUpstreams, []string{"192.0.2.10", "192.0.2.11"}) {
		t.Fatalf("changed inputs not reflected: %#v, %v", cfg, err)
	}
	collectErr = errors.New("interface enumeration failed")
	if _, err := getConfigWithNodeIP(ctx, kubeconfig, "", resolv, paths, collect); !errors.Is(err, collectErr) {
		t.Fatalf("lost enumeration error: %v", err)
	}
}

func TestWaitForNodeIPsCancellation(t *testing.T) {
	dir := t.TempDir()
	paths := nodeIPPaths{filepath.Join(dir, "primary-ip"), filepath.Join(dir, "ipv4"), filepath.Join(dir, "ipv6")}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := waitForNodeIPs(ctx, paths); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if time.Since(start) >= nodeIPDiscoveryRetryInterval {
		t.Fatal("cancellation waited for discovery retry interval")
	}
}

func TestWaitForNodeIPsAlreadyCanceled(t *testing.T) {
	dir := t.TempDir()
	paths := nodeIPPaths{filepath.Join(dir, "primary-ip"), filepath.Join(dir, "ipv4"), filepath.Join(dir, "ipv6")}
	writeDNSFixture(t, paths.primary, "192.0.2.10")
	writeDNSFixture(t, paths.ipv4, "192.0.2.10")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	addresses, err := waitForNodeIPs(ctx, paths)
	if !errors.Is(err, context.Canceled) || len(addresses) != 0 {
		t.Fatalf("expected cancellation despite ready inputs, got %v, %v", addresses, err)
	}
}

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

		Context("with address only in OVN HostAddresses annotation", func() {
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

		Context("with address only in OVN HostCidrs annotation", func() {
			It("matches an IPv4 VIP", func() {
				res, err := getNodeIpForRequestedIpStack(testNodeDualStack5, []string{testApiVipV4, testIngressVipV4}, testMachineNetworkV4)
				Expect(res).To(Equal("192.168.1.99"))
				Expect(err).To(BeNil())
			})
			It("matches an IPv6 VIP", func() {
				res, err := getNodeIpForRequestedIpStack(testNodeDualStack5, []string{testApiVipV6, testIngressVipV6}, testMachineNetworkV6)
				Expect(res).To(Equal("fd00::5"))
				Expect(err).To(BeNil())
			})
		})

		Context("with address in status and OVN HostAddresses annotation", func() {
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

		Context("with address in status and OVN HostCidrs annotation", func() {
			It("matches an IPv4 VIP", func() {
				res, err := getNodeIpForRequestedIpStack(testNodeDualStack4, []string{testApiVipV4, testIngressVipV4}, testMachineNetworkV4)
				Expect(res).To(Equal("192.168.1.99"))
				Expect(err).To(BeNil())
			})
			It("matches an IPv6 VIP", func() {
				res, err := getNodeIpForRequestedIpStack(testNodeDualStack4, []string{testApiVipV6, testIngressVipV6}, testMachineNetworkV6)
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

// Following are needed for cloud LB IP tests
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

var (
	installConfig = &types.InstallConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: types.InstallConfigVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		BaseDomain: "cluster.openshift.com",
		Publish:    types.ExternalPublishingStrategy,
	}
	baremetalPlatform = baremetal.Platform{
		LibvirtURI:                   "qemu+tcp://192.168.122.1/system",
		ProvisioningNetworkInterface: "ens3",
	}
	awsPlatform = aws.Platform{
		Region: "us-east-1",
	}
	azurePlatform = azure.Platform{
		Region: "us-east-1",
	}
	gcpPlatform = gcp.Platform{
		ProjectID: "test-project",
		Region:    "us-east-1",
	}
	nonePlatform = none.Platform{}
)

func gcpInstallConfig() *types.InstallConfig {
	installConfig.Platform = types.Platform{
		GCP: &gcpPlatform,
	}
	return installConfig
}

func awsInstallConfig() *types.InstallConfig {
	installConfig.Platform = types.Platform{
		AWS: &awsPlatform,
	}
	return installConfig
}

func azureInstallConfig() *types.InstallConfig {
	installConfig.Platform = types.Platform{
		Azure: &azurePlatform,
	}
	return installConfig
}

func baremetalInstallConfig() *types.InstallConfig {
	installConfig.Platform = types.Platform{
		BareMetal: &baremetalPlatform,
	}
	return installConfig
}

func noneInstallConfig() *types.InstallConfig {
	installConfig.Platform = types.Platform{
		None: &nonePlatform,
	}
	return installConfig
}

func createTempInstallConfig(installConfig *types.InstallConfig) string {
	icData, _ := yaml.Marshal(installConfig)

	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-config-v1",
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"install-config": string(icData),
		},
	}

	controllerConfig, _ := yaml.Marshal(cm)
	f, _ := os.CreateTemp("", "install-config.yaml")
	f.Write(controllerConfig)
	return f.Name()
}

func deleteTempInstallConfig(icFilePath string) {
	os.Remove(icFilePath)
}

var _ = Describe("isOnPremPlatform", func() {
	Context("for on-prem and cloud platforms", func() {
		Context("without platformType", func() {
			It("handles supported cloud platform install-config.yaml", func() {
				ic := gcpInstallConfig()
				icFilePath := createTempInstallConfig(ic)
				onPrem, err := isOnPremPlatform(icFilePath, "")
				Expect(err).To(BeNil())
				Expect(onPrem).To(BeFalse())
				deleteTempInstallConfig(icFilePath)
			})
			It("handles supported on-prem platform install-config.yaml", func() {
				ic := baremetalInstallConfig()
				icFilePath := createTempInstallConfig(ic)
				onPrem, err := isOnPremPlatform(icFilePath, "")
				Expect(err).To(BeNil())
				Expect(onPrem).To(BeTrue())
				deleteTempInstallConfig(icFilePath)
			})
			It("handles unsupported platform install-config.yaml", func() {
				ic := noneInstallConfig()
				icFilePath := createTempInstallConfig(ic)
				onPrem, err := isOnPremPlatform(icFilePath, "")
				Expect(err).To(BeNil())
				Expect(onPrem).To(BeFalse())
				deleteTempInstallConfig(icFilePath)
			})
			It("without install-config.yaml", func() {
				onPrem, err := isOnPremPlatform("", "")
				Expect(err).To(BeNil())
				Expect(onPrem).To(BeTrue())
			})
		})
		Context("with platformType", func() {
			It("handles supported cloud platform install-config.yaml", func() {
				ic := awsInstallConfig()
				icFilePath := createTempInstallConfig(ic)
				onPrem, err := isOnPremPlatform(icFilePath, "AWS")
				Expect(err).To(BeNil())
				Expect(onPrem).To(BeFalse())
				deleteTempInstallConfig(icFilePath)
			})
			It("handles supported on-prem platform install-config.yaml", func() {
				ic := baremetalInstallConfig()
				icFilePath := createTempInstallConfig(ic)
				onPrem, err := isOnPremPlatform(icFilePath, "BareMetal")
				Expect(err).To(BeNil())
				Expect(onPrem).To(BeTrue())
				deleteTempInstallConfig(icFilePath)
			})
			It("handles unsupported platform install-config.yaml", func() {
				ic := noneInstallConfig()
				icFilePath := createTempInstallConfig(ic)
				onPrem, err := isOnPremPlatform(icFilePath, "None")
				Expect(err).To(BeNil())
				Expect(onPrem).To(BeFalse())
				deleteTempInstallConfig(icFilePath)
			})
			It("handles mismatched cloud platform install-config.yaml", func() {
				ic := azureInstallConfig()
				icFilePath := createTempInstallConfig(ic)
				onPrem, err := isOnPremPlatform(icFilePath, "AWS")
				Expect(err).ShouldNot(BeNil())
				Expect(onPrem).To(BeFalse())
				deleteTempInstallConfig(icFilePath)
			})
			It("without install-config.yaml", func() {
				onPrem, err := isOnPremPlatform("", "AWS")
				Expect(err).To(BeNil())
				Expect(onPrem).To(BeFalse())
			})
		})
	})
})

var _ = Describe("filterDNSUpstreams", func() {
	nodeAddrs := localAddresses{addresses: []netip.Addr{netip.MustParseAddr("10.0.0.5"), netip.MustParseAddr("fd00::5")}}

	It("keeps real upstreams unchanged", func() {
		upstreams := filterDNSUpstreams([]string{"169.254.169.254", "8.8.8.8"}, nodeAddrs)
		Expect(upstreams).To(Equal([]string{"169.254.169.254", "8.8.8.8"}))
	})

	It("drops the node's own IPv4 and IPv6 addresses", func() {
		upstreams := filterDNSUpstreams([]string{"10.0.0.5", "168.63.129.16", "fd00::5"}, nodeAddrs)
		Expect(upstreams).To(Equal([]string{"168.63.129.16"}))
	})

	It("drops IPv4 and IPv6 loopback addresses", func() {
		upstreams := filterDNSUpstreams([]string{"127.0.0.1", "127.0.0.53", "::1", "8.8.8.8"}, nodeAddrs)
		Expect(upstreams).To(Equal([]string{"8.8.8.8"}))
	})

	It("drops unparseable entries", func() {
		upstreams := filterDNSUpstreams([]string{"not-an-ip", "8.8.8.8"}, nodeAddrs)
		Expect(upstreams).To(Equal([]string{"8.8.8.8"}))
	})

	It("matches node addresses regardless of textual form", func() {
		upstreams := filterDNSUpstreams([]string{"fd00:0:0:0:0:0:0:5", "8.8.8.8"}, nodeAddrs)
		Expect(upstreams).To(Equal([]string{"8.8.8.8"}))
	})

	It("returns an empty (non-nil) slice when everything is filtered", func() {
		upstreams := filterDNSUpstreams([]string{"10.0.0.5", "127.0.0.1"}, nodeAddrs)
		Expect(upstreams).NotTo(BeNil())
		Expect(upstreams).To(BeEmpty())
	})

	It("filters upstreams when node addresses are unavailable", func() {
		upstreams := filterDNSUpstreams([]string{"10.0.0.5", "127.0.0.1", "0.0.0.0", "::", "8.8.8.8", "8.8.8.8"}, localAddresses{})
		Expect(upstreams).To(Equal([]string{"10.0.0.5", "8.8.8.8"}))
	})
})

func Test(t *testing.T) {
	createTempResolvConf()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config tests")
	deleteTempResolvConf()
}
