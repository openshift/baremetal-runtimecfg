package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
	"github.com/openshift/installer/pkg/types"
)

const (
	LocalhostKubeApiServerUrl string = "https://localhost:6443"
	// labelNodeRolePrefix is a label prefix for node roles
	labelNodeRolePrefix = "node-role.kubernetes.io/"
)

var log = logrus.New()

type NodeAddress struct {
	Address string
	Name    string
	Ipv6    bool
}

type Cluster struct {
	Name                   string
	Domain                 string
	APIVIP                 string
	APIVirtualRouterID     uint8
	APIVIPRecordType       string
	APIVIPEmptyType        string
	IngressVIP             string
	IngressVirtualRouterID uint8
	IngressVIPRecordType   string
	IngressVIPEmptyType    string
	VIPNetmask             int
	MasterAmount           int64
	NodeAddresses          []NodeAddress
}

type Backend struct {
	Host    string
	Address string
	Port    uint16
}

type ApiLBConfig struct {
	ApiPort      uint16
	LbPort       uint16
	StatPort     uint16
	Backends     []Backend
	FrontendAddr string
}

type IngressConfig struct {
	Peers []string
}

type Node struct {
	Cluster       Cluster
	LBConfig      ApiLBConfig
	NonVirtualIP  string
	ShortHostname string
	VRRPInterface string
	DNSUpstreams  []string
	IngressConfig IngressConfig
	EnableUnicast bool
	Configs       *[]Node
}

func getDNSUpstreams(resolvConfPath string) (upstreams []string, err error) {
	dnsFile, err := os.Open(resolvConfPath)
	if err != nil {
		return upstreams, err
	}
	defer dnsFile.Close()

	scanner := bufio.NewScanner(dnsFile)

	// Scanner's default SplitFunc is bufio.ScanLines
	upstreams = make([]string, 0)
	for scanner.Scan() {
		line := string(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		switch fields[0] {
		case "nameserver":
			// CoreDNS forward plugin takes up to 15 upstream servers
			if len(fields) > 1 && len(upstreams) < 15 {
			}
			upstreams = append(upstreams, fields[1])
		}
	}
	if err := scanner.Err(); err != nil {
		return upstreams, err
	}
	return upstreams, nil
}

func GetKubeconfigClusterNameAndDomain(kubeconfigPath string) (name, domain string, err error) {
	kubeCfg, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return "", "", err
	}
	ctxt := kubeCfg.Contexts[kubeCfg.CurrentContext]
	cluster := kubeCfg.Clusters[ctxt.Cluster]
	serverUrl, err := url.Parse(cluster.Server)
	if err != nil {
		return "", "", err
	}

	apiHostname := serverUrl.Hostname()
	apiHostnameSlices := strings.SplitN(apiHostname, ".", 3)

	return apiHostnameSlices[1], apiHostnameSlices[2], nil
}

func getClusterConfigClusterNameAndDomain(configPath string) (name, domain string, err error) {
	ic, err := getClusterConfigMapInstallConfig(configPath)
	if err != nil {
		return name, domain, err
	}

	return ic.ObjectMeta.Name, ic.BaseDomain, nil
}

func getClusterConfigMasterAmount(configPath string) (amount *int64, err error) {
	ic, err := getClusterConfigMapInstallConfig(configPath)
	if err != nil {
		return amount, err
	}

	return ic.ControlPlane.Replicas, nil
}

func getClusterConfigMapInstallConfig(configPath string) (installConfig types.InstallConfig, err error) {
	yamlFile, err := ioutil.ReadFile(configPath)
	if err != nil {
		return installConfig, err
	}

	cm := v1.ConfigMap{}
	err = yaml.Unmarshal(yamlFile, &cm)
	if err != nil {
		return installConfig, err
	}

	ic := types.InstallConfig{}
	err = yaml.Unmarshal([]byte(cm.Data["install-config"]), &ic)

	return ic, err
}

// PopulateVRIDs fills in the Virtual Router information for the provided Node configuration
func (c *Cluster) PopulateVRIDs() error {
	// Add one to the fletcher8 result because 0 is an invalid vrid in
	// keepalived. This is safe because fletcher8 can never return 255 due to
	// the modulo arithmetic that happens. The largest value it can return is
	// 238 (0xEE).
	if c.Name == "" {
		return fmt.Errorf("Cluster name can't be empty")
	}
	c.APIVirtualRouterID = utils.FletcherChecksum8(c.Name+"-api") + 1
	c.IngressVirtualRouterID = utils.FletcherChecksum8(c.Name+"-ingress") + 1
	if c.IngressVirtualRouterID == c.APIVirtualRouterID {
		c.IngressVirtualRouterID++
	}
	return nil
}

func GetVRRPConfig(apiVip, ingressVip net.IP) (vipIface net.Interface, nonVipAddr *net.IPNet, err error) {
	vips := make([]net.IP, 0)
	if apiVip != nil && (utils.IsIPv4(apiVip) || utils.IsIPv6(apiVip)) {
		vips = append(vips, apiVip)
	}
	if ingressVip != nil && (utils.IsIPv4(ingressVip) || utils.IsIPv6(ingressVip)) {
		vips = append(vips, ingressVip)
	}
	return getInterfaceAndNonVIPAddr(vips)
}

// IsUpgradeStillRunning check if the upgrade is still running by looking at
// the nodes' machineconfiguration state and kubelet version. Once all of the
// machineconfigurations are Done and all kubelet versions match we know it
// is safe to trigger the unicast migration.
//
// Args:
//   - kubeClient as *KubeClient
//
// Returns:
//   - true (upgrade still running), false (upgrade complete) or error
func IsUpgradeStillRunning(kubeClient *KubeClient) (bool, error) {
	nodes, err := kubeClient.ListNodes("")
	if err != nil {
		return false, err
	}

	kubeletVersion := ""
	stateAnnotation := "machineconfiguration.openshift.io/state"
	for _, node := range nodes {
		// Verify kubelet versions match. In EUS upgrades we may end up in an
		// intermediate state where all of the nodes are "updated" as far as
		// MCO is concerned, but are actually on different versions of OCP.
		// In those cases, we do not consider the upgrade complete because not
		// all nodes are ready for migration.
		if kubeletVersion == "" {
			kubeletVersion = node.Status.NodeInfo.KubeletVersion
		}
		if kubeletVersion != node.Status.NodeInfo.KubeletVersion {
			return true, nil
		}
		if node.Annotations[stateAnnotation] != "Done" {
			return true, nil
		}
	}
	return false, nil
}

func GetIngressConfig(kubeClient *KubeClient, vips []string) (IngressConfig, error) {
	var machineNetwork string
	var ingressConfig IngressConfig

	nodes, err := kubeClient.ListNodes("")
	if err != nil {
		return ingressConfig, err
	}

	if len(vips) == 0 {
		// This is not necessarily an error path because in handleBootstrapStopKeepalived we do
		// call this function without providing any VIPs. Because of this, we only want to mark
		// this scenario and avoid trying to calculate the machine networks.
		log.Infof("Requested GetIngressConfig for empty VIP list.")
	} else {
		// As it is not possible to get cluster's Machine Network directly, we are using a workaround
		// by detecting which of the local interfaces belongs to the same subnet as requested VIP.
		// This interface can be used to detect what was the original machine network as it contains
		// the subnet mask that we need.
		machineNetwork, err = utils.GetLocalCIDRByIP(vips[0])
		if err != nil {
			return ingressConfig, err
		}
	}

	for _, node := range nodes {
		addr, err := getNodeIpForRequestedIpStack(node, vips, machineNetwork)
		if err != nil {
			log.WithFields(logrus.Fields{
				"err": err,
			}).Warnf("For node %s could not retrieve node's IP. Ignoring", node.ObjectMeta.Name)
		} else {
			ingressConfig.Peers = append(ingressConfig.Peers, addr)
		}
	}

	return ingressConfig, nil
}

func getNodeIpForRequestedIpStack(node *v1.Node, filterIps []string, machineNetwork string) (string, error) {
	log.Infof("Searching for Node IP of %s. Using '%s' as machine network. Filtering out VIPs '%s'.", node.Name, machineNetwork, filterIps)

	if len(filterIps) == 0 {
		return "", fmt.Errorf("for node %s requested NodeIP detection with empty filterIP list. Cannot detect IP stack", node.Name)
	}

	isFilterV4 := utils.IsIPv4(net.ParseIP(filterIps[0]))
	isFilterV6 := utils.IsIPv6(net.ParseIP(filterIps[0]))

	if !isFilterV4 && !isFilterV6 {
		return "", fmt.Errorf("for node %s IPs are neither IPv4 nor IPv6", node.Name)
	}

	// We need to collect IP address of a matching IP stack for every node that is part of the
	// cluster. We need to account for a scenario where Node.Status.Addresses list is incomplete
	// and use different source of the address.
	//
	// We will use here the following sources:
	//   1) Node.Status.Addresses list
	//   2) Node annotation "k8s.ovn.org/host-addresses" in combination with Machine Networks
	//
	// If none of those returns a conclusive result, we don't return an IP for this node. This is
	// not a desired outcome, but can be extended in the future if desired.

	var addr string
	for _, address := range node.Status.Addresses {
		if address.Type == v1.NodeInternalIP {
			if (utils.IsIPv4(net.ParseIP(address.Address)) && isFilterV4) || (utils.IsIPv6(net.ParseIP(address.Address)) && isFilterV6) {
				addr = address.Address
				log.Infof("For node %s selected peer address %s using NodeInternalIP", node.Name, addr)
			}
		}
	}
	if addr == "" {
		log.Infof("For node %s can't find address using NodeInternalIP. Fallback to OVN annotation.", node.Name)

		var ovnHostAddresses []string
		if err := json.Unmarshal([]byte(node.Annotations["k8s.ovn.org/host-addresses"]), &ovnHostAddresses); err != nil {
			log.WithFields(logrus.Fields{
				"err": err,
			}).Warnf("Couldn't unmarshall OVN annotations: '%s'. Skipping.", node.Annotations["k8s.ovn.org/host-addresses"])
		}

		// Here we need to guarantee that local Node IP (i.e. NonVirtualIP) is present somewhere
		// in the IngressConfig.Peers list. This makes "participateInIngressVRPP" to evaluate
		// correctly on the local node where keepalived-monitor runs.
		//
		// We don't care about remote peers so use of GetVRRPConfig() is a natural choice as this
		// function is used earlier to calculate NonVirtualIP - this guarantees selection of the
		// same IP.
		//
		// We are checking if NonVirtualIP is present in the list of OVN annotations. If yes, we
		// use it as a hint and simply pick this IP address.

		_, nonVipAddr, err := GetVRRPConfig(net.ParseIP(filterIps[0]), nil)
		if err != nil {
			return "", err
		}
		suggestedIp := nonVipAddr.IP.String()
		if suggestedIp != "" {
			for _, hostAddr := range ovnHostAddresses {
				if suggestedIp == hostAddr {
					log.Infof("For node %s selected peer address %s using OVN annotations and suggestion.", node.Name, suggestedIp)
					return suggestedIp, nil
				}
			}
		}

	AddrList:
		for _, hostAddr := range ovnHostAddresses {
			for _, filterIp := range filterIps {
				if hostAddr == filterIp {
					log.Infof("Address %s is VIP. Skipping.", hostAddr)
					continue AddrList
				}
			}

			if (utils.IsIPv4(net.ParseIP(hostAddr)) && !isFilterV4) || (utils.IsIPv6(net.ParseIP(hostAddr)) && !isFilterV6) {
				log.Infof("Address %s doesn't match requested IP stack. Skipping.", hostAddr)
				continue
			}

			match, err := utils.IpInCidr(hostAddr, machineNetwork)
			if err != nil {
				log.Infof("Address '%s' and subnet '%s' couldn't be parsed. Skipping.", hostAddr, machineNetwork)
				continue
			}
			if match {
				addr = hostAddr
				log.Infof("For node %s selected peer address %s using OVN annotations.", node.Name, addr)
				break AddrList
			}
		}
	}
	return addr, nil
}

// Returns a Node object populated with the configuration specified by the parameters
// to the function.
// kubeconfigPath: The path to a kubeconfig that can be used to read cluster status
// from the k8s api.
// clusterConfigPath: The path to cluster-config.yaml. This is only available on the
// bootstrap node so it is optional. If the file is not available, set this to "".
// resolvConfPath: The path to resolv.conf. Typically either /etc/resolv.conf or
// /var/run/NetworkManager/resolv.conf.
// apiVips and ingressVips: Lists of VIPs for API and Ingress, respectively.
// apiPort: The port on which the k8s api listens. Should be 6443.
// lbPort: The port on which haproxy listens.
// statPort: The port on which the haproxy stats endpoint listens.
func GetConfig(kubeconfigPath, clusterConfigPath, resolvConfPath string, apiVips, ingressVips []net.IP, apiPort, lbPort, statPort uint16) (node Node, err error) {
	vipCount := 0
	if len(apiVips) > len(ingressVips) {
		vipCount = len(apiVips)
	} else {
		vipCount = len(ingressVips)
	}
	nodes := []Node{}
	var apiVip, ingressVip net.IP
	for i := 0; i < vipCount; i++ {
		if i < len(apiVips) {
			apiVip = apiVips[i]
		} else {
			apiVip = nil
		}
		if i < len(ingressVips) {
			ingressVip = ingressVips[i]
		} else {
			ingressVip = nil
		}
		newNode, err := getNodeConfig(kubeconfigPath, clusterConfigPath, resolvConfPath, apiVip, ingressVip, apiPort, lbPort, statPort)
		if err != nil {
			return Node{}, err
		}
		nodes = append(nodes, newNode)
	}
	nodes[0].Configs = &nodes
	return nodes[0], nil
}

func getNodeConfig(kubeconfigPath, clusterConfigPath, resolvConfPath string, apiVip net.IP, ingressVip net.IP, apiPort, lbPort, statPort uint16) (node Node, err error) {
	clusterName, clusterDomain, err := GetClusterNameAndDomain(kubeconfigPath, clusterConfigPath)
	if err != nil {
		return node, err
	}

	node.Cluster.Name = clusterName
	node.Cluster.Domain = clusterDomain

	node.Cluster.PopulateVRIDs()

	if clusterConfigPath != "" {
		masterAmount, err := getClusterConfigMasterAmount(clusterConfigPath)
		if err != nil {
			return node, err
		}

		node.Cluster.MasterAmount = *masterAmount
	}

	// Node
	node.ShortHostname, err = utils.ShortHostname()
	if err != nil {
		return node, err
	}

	node.Cluster.APIVIPRecordType = "A"
	node.Cluster.APIVIPEmptyType = "AAAA"
	if apiVip != nil {
		node.Cluster.APIVIP = apiVip.String()
		if apiVip.To4() == nil {
			node.Cluster.APIVIPRecordType = "AAAA"
			node.Cluster.APIVIPEmptyType = "A"
		}
	}
	node.Cluster.IngressVIPRecordType = "A"
	node.Cluster.IngressVIPEmptyType = "AAAA"
	if ingressVip != nil {
		node.Cluster.IngressVIP = ingressVip.String()
		if ingressVip.To4() == nil {
			node.Cluster.IngressVIPRecordType = "AAAA"
			node.Cluster.IngressVIPEmptyType = "A"
		}
	}
	vipIface, nonVipAddr, err := GetVRRPConfig(apiVip, ingressVip)
	if err != nil {
		return node, err
	}
	node.NonVirtualIP = nonVipAddr.IP.String()

	node.EnableUnicast = false
	if os.Getenv("ENABLE_UNICAST") == "yes" {
		node.EnableUnicast = true
	}

	resolvConfUpstreams, err := getDNSUpstreams(resolvConfPath)
	if err != nil {
		return node, err
	}
	// Filter out our potential CoreDNS addresses from upstream servers
	node.DNSUpstreams = make([]string, 0)
	for _, upstream := range resolvConfUpstreams {
		if upstream != node.NonVirtualIP && upstream != "127.0.0.1" && upstream != "::1" {
			node.DNSUpstreams = append(node.DNSUpstreams, upstream)
		}
	}
	// If we end up with no upstream DNS servers we'll generate an invalid
	// coredns config. Error out so the init container retries.
	if len(node.DNSUpstreams) < 1 {
		return node, errors.New("No upstream DNS servers found")
	}

	if apiVip.To4() == nil {
		node.Cluster.VIPNetmask = 128
	} else {
		node.Cluster.VIPNetmask = 32
	}
	node.VRRPInterface = vipIface.Name

	// We can't populate this with GetLBConfig because in many cases the
	// backends won't be available yet.
	node.LBConfig = ApiLBConfig{
		ApiPort:  apiPort,
		LbPort:   lbPort,
		StatPort: statPort,
	}

	return node, err
}

// getSortedBackends builds config to communicate with kube-api based on kubeconfigPath parameter value, if kubeconfigPath is not empty it will build the
// config based on that content else config will point to localhost.
func getSortedBackends(kubeClient *KubeClient, vips []net.IP) (backends []Backend, err error) {
	nodes, err := kubeClient.ListNodes("node-role.kubernetes.io/master=")
	if err != nil {
		log.WithFields(logrus.Fields{
			"err": err,
		}).Info("Failed to get master Nodes list")
		return []Backend{}, err
	}
	if len(vips) == 0 {
		return []Backend{}, fmt.Errorf("Trying to build config using empty VIPs")
	}

	// As it is not possible to get cluster's Machine Network directly, we are using a workaround
	// by detecting which of the local interfaces belongs to the same subnet as requested VIP.
	// This interface can be used to detect what was the original machine network as it contains
	// the subnet mask that we need.
	machineNetwork, err := utils.GetLocalCIDRByIP(vips[0].String())
	if err != nil {
		log.WithFields(logrus.Fields{
			"err": err,
		}).Errorf("Could not retrieve subnet for IP %s", vips[0].String())
		return []Backend{}, err
	}

	for _, node := range nodes {
		masterIp, err := getNodeIpForRequestedIpStack(node, utils.ConvertIpsToStrings(vips), machineNetwork)
		if err != nil {
			log.WithFields(logrus.Fields{
				"err": err,
			}).Warnf("Could not retrieve node's IP for %s. Ignoring", node.ObjectMeta.Name)
		} else {
			backends = append(backends, Backend{Host: node.ObjectMeta.Name, Address: masterIp})
		}
	}

	sort.Slice(backends, func(i, j int) bool {
		return backends[i].Address < backends[j].Address
	})
	return backends, err
}

func GetLBConfig(kubeClient, localKubeClient *KubeClient, apiPort, lbPort, statPort uint16, vips []net.IP) (ApiLBConfig, error) {
	config := ApiLBConfig{
		ApiPort:  apiPort,
		LbPort:   lbPort,
		StatPort: statPort,
	}

	if len(vips) == 0 {
		return config, fmt.Errorf("Trying to generate loadbalancer config using empty VIPs")
	}

	// LB frontend address: IPv6 '::' , IPv4 ''
	if utils.IsIPv6(vips[0]) {
		config.FrontendAddr = "::"
	}
	// Try reading master nodes details first from api-vip:kube-apiserver and failover to localhost:kube-apiserver
	backends, err := getSortedBackends(kubeClient, vips)
	if err != nil {
		log.Infof("An error occurred while trying to read master nodes details from api-vip:kube-apiserver: %v", err)
		log.Infof("Trying to read master nodes details from localhost:kube-apiserver")
		backends, err = getSortedBackends(localKubeClient, vips)
		if err != nil {
			log.Error("Failed to retrieve API members information")
			return config, err
		}
	}
	// The backends port is the Etcd one, but we need to loadbalance the API one
	for i := 0; i < len(backends); i++ {
		backends[i].Port = apiPort
	}
	config.Backends = backends
	log.WithFields(logrus.Fields{
		"config": config,
	}).Debug("Config for LB configuration retrieved")
	return config, nil
}

func GetClusterNameAndDomain(kubeconfigPath, clusterConfigPath string) (clusterName string, clusterDomain string, err error) {
	// Try cluster-config.yml first
	clusterName, clusterDomain, err = getClusterConfigClusterNameAndDomain(clusterConfigPath)
	if err != nil {
		// We are using kubeconfig as a fallback for this
		clusterName, clusterDomain, err = GetKubeconfigClusterNameAndDomain(kubeconfigPath)
	}

	return
}

func PopulateNodeAddresses(kubeClient *KubeClient, node *Node) {
	nodes, err := kubeClient.ListNodes("")
	if err != nil {
		log.Errorf("Failed to get node list: %s", err)
		return
	}
	var nodeAddresses []net.IP
	for _, n := range nodes {
		name := ""
		nodeAddresses = nil
		for _, a := range n.Status.Addresses {
			if a.Type == v1.NodeHostName {
				// We only want the shortname
				name = strings.Split(a.Address, ".")[0]
			} else if a.Type == v1.NodeInternalIP {
				nodeAddresses = append(nodeAddresses, net.ParseIP(a.Address))
			}
		}
		if name == "" || (nodeAddresses == nil) {
			log.Warningf("Could not handle node: %v", node)
			continue
		}
		// TODO(bnemec): The ipv6 flag isn't currently used in the templates,
		// but at some point it probably should be so we provide RFC-compliant
		// ipv6 behavior.
		for _, addr := range nodeAddresses {
			ipv6 := true
			check := addr.To4()
			if check != nil {
				ipv6 = false
			}
			node.Cluster.NodeAddresses = append(node.Cluster.NodeAddresses, NodeAddress{Address: addr.String(), Name: name, Ipv6: ipv6})
		}
	}
}
