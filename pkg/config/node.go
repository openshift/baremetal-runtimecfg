package config

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/ghodss/yaml"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
	"github.com/openshift/installer/pkg/types"
)

const (
	localhostKubeApiServerUrl string = "https://localhost:6443"
	// labelNodeRolePrefix is a label prefix for node roles
	labelNodeRolePrefix = "node-role.kubernetes.io/"
	// highlyAvailableArbiterMode is the control plane topology when installing TNA
	highlyAvailableArbiterMode string = "HighlyAvailableArbiter"
	// dualReplicaTopologyMode is the control plane topology when installing TNF
	dualReplicaTopologyMode string = "DualReplica"
)

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
	APILBIPs               []string
	APIIntLBIPs            []string
	IngressLBIPs           []string
	CloudLBRecordType      string
	CloudLBEmptyType       string
	PlatformType           string
	ControlPlaneTopology   string
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

type ClusterLBConfig struct {
	ApiLBIPs     []net.IP
	ApiIntLBIPs  []net.IP
	IngressLBIPs []net.IP
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

func isOnPremPlatform(configPath string, platformType string) (bool, error) {
	if configPath == "" && platformType == "" {
		// Default to on-prem
		return true, nil
	}

	icPlatform := ""
	// Read contents of install-config.yaml, if present
	ic, err := getClusterConfigMapInstallConfig(configPath)
	if err != nil {
		if platformType == "" {
			// Again, default to on-prem
			return true, err
		}
	}

	switch {
	case ic.Platform.BareMetal != nil:
		icPlatform = string(configv1.BareMetalPlatformType)
	case ic.Platform.VSphere != nil:
		icPlatform = string(configv1.VSpherePlatformType)
	case ic.Platform.OpenStack != nil:
		icPlatform = string(configv1.OpenStackPlatformType)
	case ic.Platform.Ovirt != nil:
		icPlatform = string(configv1.OvirtPlatformType)
	case ic.Platform.Nutanix != nil:
		icPlatform = string(configv1.NutanixPlatformType)
	case ic.Platform.GCP != nil:
		icPlatform = string(configv1.GCPPlatformType)
	case ic.Platform.AWS != nil:
		icPlatform = string(configv1.AWSPlatformType)
	case ic.Platform.Azure != nil:
		icPlatform = string(configv1.AzurePlatformType)
	}

	// Both "platform" parameter and install-config are present
	if platformType != "" && icPlatform != "" {
		if platformType != icPlatform {
			return false, fmt.Errorf("Platforms specified in install-config and platform parameter do not match")
		}
	}

	// One of the 2 parameters are present
	platform := icPlatform
	if platformType != "" {
		platform = platformType
	}
	switch platform {
	case string(configv1.GCPPlatformType), string(configv1.AWSPlatformType), string(configv1.AzurePlatformType):
		return false, nil
	case string(configv1.BareMetalPlatformType), string(configv1.VSpherePlatformType), string(configv1.OpenStackPlatformType), string(configv1.OvirtPlatformType), string(configv1.NutanixPlatformType):
		return true, nil
	}

	return false, nil
}

func getClusterConfigMapInstallConfig(configPath string) (installConfig types.InstallConfig, err error) {
	yamlFile, err := os.ReadFile(configPath)
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

// GetNodes will return a list of all nodes in the cluster
//
// Args:
//   - kubeconfigPath as string
//
// Returns:
//   - v1.NodeList or error
func GetNodes(kubeconfigPath string) (*v1.NodeList, error) {
	config, err := utils.GetClientConfig("", kubeconfigPath)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

// IsUpgradeStillRunning check if the upgrade is still running by looking at
// the nodes' machineconfiguration state and kubelet version. Once all of the
// machineconfigurations are Done and all kubelet versions match we know it
// is safe to trigger the unicast migration.
//
// Args:
//   - kubeconfigPath as string
//
// Returns:
//   - true (upgrade still running), false (upgrade complete) or error
func IsUpgradeStillRunning(kubeconfigPath string) (bool, error) {
	nodes, err := GetNodes(kubeconfigPath)
	if err != nil {
		return false, err
	}

	kubeletVersion := ""
	stateAnnotation := "machineconfiguration.openshift.io/state"
	for _, node := range nodes.Items {
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

func GetIngressConfig(kubeconfigPath string, vips []string) (IngressConfig, error) {
	var machineNetwork string
	var ingressConfig IngressConfig

	config, err := utils.GetClientConfig("", kubeconfigPath)
	if err != nil {
		return ingressConfig, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return ingressConfig, err
	}

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
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
		//
		// In case there is no subnet containing a VIP on any of the available NICs we are counterintuitively
		// selecting just a Node IP with the matching IP stack. This is a weird case in e.g. vSphere
		// where VIPs do not belong to the L2 of the node, yet they work properly.
		machineNetwork, err = utils.GetLocalCIDRByIP(vips[0])

		if err == nil {
			for _, node := range nodes.Items {
				addr, err := getNodeIpForRequestedIpStack(node, vips, machineNetwork)
				if err != nil {
					log.WithFields(logrus.Fields{
						"err": err,
					}).Warnf("For node %s could not retrieve node's IP. Ignoring", node.ObjectMeta.Name)
				} else {
					ingressConfig.Peers = append(ingressConfig.Peers, addr)
				}
			}
		} else {
			log.WithFields(logrus.Fields{
				"err": err,
			}).Errorf("Could not retrieve subnet for IP %s. Falling back to an IP of the matching IP stack", vips[0])

			for _, node := range nodes.Items {
				addr := ""
				for _, address := range node.Status.Addresses {
					if address.Type == v1.NodeInternalIP && utils.IsIPv6(net.ParseIP(address.Address)) == utils.IsIPv6(net.ParseIP(vips[0])) {
						addr = address.Address
						break
					}
				}
				if addr != "" {
					ingressConfig.Peers = append(ingressConfig.Peers, addr)
				} else {
					log.WithFields(logrus.Fields{
						"err": err,
					}).Warnf("Could not retrieve node's IP for %s. Ignoring", node.ObjectMeta.Name)
				}
			}
		}
	}

	return ingressConfig, nil
}

func getNodeIpForRequestedIpStack(node v1.Node, filterIps []string, machineNetwork string) (string, error) {
	log.Debugf("Searching for Node IP of %s. Using '%s' as machine network. Filtering out VIPs '%s'.", node.Name, machineNetwork, filterIps)

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
	//   2) Node annotation "k8s.ovn.org/host-cidrs" in combination with Machine Networks
	//   3) Deprecated node annotation "k8s.ovn.org/host-addresses" in combination with Machine Networks
	//
	// If none of those returns a conclusive result, we don't return an IP for this node. This is
	// not a desired outcome, but can be extended in the future if desired.

	var addr string
	for _, address := range node.Status.Addresses {
		if address.Type == v1.NodeInternalIP {
			if (utils.IsIPv4(net.ParseIP(address.Address)) && isFilterV4) || (utils.IsIPv6(net.ParseIP(address.Address)) && isFilterV6) {
				addr = address.Address
				log.Debugf("For node %s selected peer address %s using NodeInternalIP", node.Name, addr)
			}
		}
	}
	if addr == "" {
		log.Debugf("For node %s can't find address using NodeInternalIP. Fallback to OVN annotation.", node.Name)

		var ovnHostAddresses []string
		var tmp []string

		err := json.Unmarshal([]byte(node.Annotations["k8s.ovn.org/host-cidrs"]), &tmp)
		if err == nil {
			for _, cidr := range tmp {
				ip := strings.Split(cidr, "/")[0]
				ovnHostAddresses = append(ovnHostAddresses, ip)
			}
		} else {
			log.WithFields(logrus.Fields{
				"err": err,
			}).Warnf("Couldn't unmarshall OVN HostCidrs annotations: '%s'. Trying HostAddresses.", node.Annotations["k8s.ovn.org/host-cidrs"])

			if err := json.Unmarshal([]byte(node.Annotations["k8s.ovn.org/host-addresses"]), &ovnHostAddresses); err != nil {
				log.WithFields(logrus.Fields{
					"err": err,
				}).Warnf("Couldn't unmarshall OVN HostAddresses annotations: '%s'. Skipping.", node.Annotations["k8s.ovn.org/host-addresses"])
			}
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
					log.Debugf("For node %s selected peer address %s using OVN annotations and suggestion.", node.Name, suggestedIp)
					return suggestedIp, nil
				}
			}
		}

	AddrList:
		for _, hostAddr := range ovnHostAddresses {
			for _, filterIp := range filterIps {
				if hostAddr == filterIp {
					log.Debugf("Address %s is VIP. Skipping.", hostAddr)
					continue AddrList
				}
			}

			if (utils.IsIPv4(net.ParseIP(hostAddr)) && !isFilterV4) || (utils.IsIPv6(net.ParseIP(hostAddr)) && !isFilterV6) {
				log.Debugf("Address %s doesn't match requested IP stack. Skipping.", hostAddr)
				continue
			}

			match, err := utils.IpInCidr(hostAddr, machineNetwork)
			if err != nil {
				log.Warnf("Address '%s' and subnet '%s' couldn't be parsed. Skipping.", hostAddr, machineNetwork)
				continue
			}
			if match {
				addr = hostAddr
				log.Debugf("For node %s selected peer address %s using OVN annotations.", node.Name, addr)
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
// clusterLBConfig: A struct containing IPs for API, API-Int and Ingress LBs
// platformType: Name of the platform.
func GetConfig(kubeconfigPath, clusterConfigPath, resolvConfPath string, apiVips, ingressVips []net.IP, apiPort, lbPort, statPort uint16, clusterLBConfig ClusterLBConfig, platformType string, controlPlaneTopology string) (node Node, err error) {
	if onPremPlatform, _ := isOnPremPlatform(clusterConfigPath, platformType); !onPremPlatform {
		// Cloud Platforms with cloud LBs but no Cloud DNS
		return getNodeConfigWithCloudLBIPs(kubeconfigPath, clusterConfigPath, resolvConfPath, clusterLBConfig, platformType, controlPlaneTopology)
	}
	// On-prem platforms
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
		newNode, err := getNodeConfig(kubeconfigPath, clusterConfigPath, resolvConfPath, apiVip, ingressVip, apiPort, lbPort, statPort, platformType, controlPlaneTopology)
		if err != nil {
			return Node{}, err
		}
		nodes = append(nodes, newNode)
	}
	nodes[0].Configs = &nodes
	return nodes[0], nil
}

func getNodeConfig(kubeconfigPath, clusterConfigPath, resolvConfPath string, apiVip net.IP, ingressVip net.IP, apiPort, lbPort, statPort uint16, platformType string, controlPlaneTopology string) (node Node, err error) {
	clusterName, clusterDomain, err := GetClusterNameAndDomain(kubeconfigPath, clusterConfigPath)
	if err != nil {
		return node, err
	}

	node.Cluster.Name = clusterName
	node.Cluster.Domain = clusterDomain
	node.Cluster.PlatformType = platformType
	node.Cluster.ControlPlaneTopology = controlPlaneTopology

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
	// Rest of the Node config will not be available on Cloud platforms.
	if onPremPlatform, err := isOnPremPlatform(clusterConfigPath, platformType); !onPremPlatform {
		return node, err
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
func getSortedBackends(kubeconfigPath string, readFromLocalAPI bool, vips []net.IP, controlPlaneTopology string) (backends []Backend, err error) {
	kubeApiServerUrl := ""
	if readFromLocalAPI {
		kubeApiServerUrl = localhostKubeApiServerUrl
	}
	config, err := utils.GetClientConfig(kubeApiServerUrl, kubeconfigPath)
	if err != nil {
		return []Backend{}, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.WithFields(logrus.Fields{
			"err": err,
		}).Info("Failed to get client")
		return []Backend{}, err
	}
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: "node-role.kubernetes.io/master=",
	})
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
	// In case there is no subnet containing a VIP on any of the available NICs we are counterintuitively
	// selecting just a Node IP with the matching IP stack. This is a weird case in e.g. vSphere
	// where VIPs do not belong to the L2 of the node, yet they work properly.
	machineNetwork, err := utils.GetLocalCIDRByIP(vips[0].String())
	if err == nil {
		for _, node := range nodes.Items {
			masterIp, err := getNodeIpForRequestedIpStack(node, utils.ConvertIpsToStrings(vips), machineNetwork)
			if err != nil {
				log.WithFields(logrus.Fields{
					"err": err,
				}).Warnf("Could not retrieve node's IP for %s. Ignoring", node.ObjectMeta.Name)
			} else {
				backends = append(backends, Backend{Host: node.ObjectMeta.Name, Address: masterIp})
			}
		}
	} else {
		log.WithFields(logrus.Fields{
			"err": err,
		}).Errorf("Could not retrieve subnet for IP %s. Falling back to an IP of the matching IP stack", vips[0].String())

		for _, node := range nodes.Items {
			masterIp := ""
			for _, address := range node.Status.Addresses {
				if address.Type == v1.NodeInternalIP && utils.IsIPv6(net.ParseIP(address.Address)) == utils.IsIPv6(vips[0]) {
					masterIp = address.Address
					break
				}
			}
			if masterIp != "" {
				backends = append(backends, Backend{Host: node.ObjectMeta.Name, Address: masterIp})
			} else {
				log.WithFields(logrus.Fields{
					"err": err,
				}).Warnf("Could not retrieve node's IP for %s. Ignoring", node.ObjectMeta.Name)
			}
		}
	}

	// When installing TNA/TNF clusters using assisted service, one of the master nodes acts as the bootstrap.
	// So during the installation there will only be one master node, but we need two in order to configure keepalived.
	// We cannot wait until the bootstrap finishes and becomes a master, because then no node will have the API vip.
	// To circumvent that we will temporarily add a dummy ip to the list of nodes.
	// After the bootstrap becomes a master node, it's ip will replace the dummy ip in the list.
	if len(nodes.Items) == 1 &&
		(controlPlaneTopology == highlyAvailableArbiterMode || controlPlaneTopology == dualReplicaTopologyMode) {
		backends = append(backends, Backend{Host: "dummy", Address: "0.0.0.0"})
	}

	sort.Slice(backends, func(i, j int) bool {
		return backends[i].Address < backends[j].Address
	})
	return backends, nil
}

func GetLBConfig(kubeconfigPath string, apiPort, lbPort, statPort uint16, vips []net.IP, controlPlaneTopology string) (ApiLBConfig, error) {
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
	backends, err := getSortedBackends(kubeconfigPath, false, vips, controlPlaneTopology)
	if err != nil {
		log.Infof("An error occurred while trying to read master nodes details from api-vip:kube-apiserver: %v", err)
		log.Infof("Trying to read master nodes details from localhost:kube-apiserver")
		backends, err = getSortedBackends(kubeconfigPath, true, vips, controlPlaneTopology)
		if err != nil {
			log.WithFields(logrus.Fields{
				"kubeconfigPath": kubeconfigPath,
			}).Error("Failed to retrieve API members information")
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

func PopulateNodeAddresses(kubeconfigPath string, node *Node) {
	// Get node list
	config, err := utils.GetClientConfig("", kubeconfigPath)
	if err != nil {
		log.Errorf("Failed to build client config: %s", err)
		return
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Errorf("Failed to create client: %s", err)
		return
	}
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Errorf("Failed to get node list: %s", err)
		return
	}
	var nodeAddresses []net.IP
	for _, n := range nodes.Items {
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

func getNodeConfigWithCloudLBIPs(kubeconfigPath, clusterConfigPath, resolvConfPath string, clusterLBConfig ClusterLBConfig, platformType string, controlPlaneTopology string) (node Node, err error) {
	var apiLBIP, apiIntLBIP, ingressIP net.IP
	nodes := []Node{}

	// The number of LB IPs provided in ClusterLBConfig's ApiIntLBIPs, ApiLBIPs and IngressLBIPs
	// could all be different. In private clusters, there would be no API LB IPs provided. So,to
	// account for the varying lengths we determine the longest between the list of Ingress LB
	// IPs and API-Int LB IPs. When present, len(ApiLBIPs) is equal to len(ApiIntLBIPs).
	ipCount := 0
	if len(clusterLBConfig.ApiIntLBIPs) > len(clusterLBConfig.IngressLBIPs) {
		ipCount = len(clusterLBConfig.ApiIntLBIPs)
	} else {
		ipCount = len(clusterLBConfig.IngressLBIPs)
	}

	// Iterate through the longest list of LB IPs and provide an API, API-Int and Ingress
	// LB IP to each newly created Node. When a list has no more LB IPs, update Node object
	// with a nil IP address.
	for i := 0; i < ipCount; i++ {
		if i < len(clusterLBConfig.ApiIntLBIPs) {
			apiIntLBIP = clusterLBConfig.ApiIntLBIPs[i]
		} else {
			apiIntLBIP = nil
		}

		// For public clusters. Private clusters will not have External
		// LBs so apiLBIPs could be empty.
		if len(clusterLBConfig.ApiLBIPs) != 0 && i < len(clusterLBConfig.ApiLBIPs) {
			apiLBIP = clusterLBConfig.ApiLBIPs[i]
		} else {
			apiLBIP = nil
		}
		if len(clusterLBConfig.IngressLBIPs) != 0 && i < len(clusterLBConfig.IngressLBIPs) {
			ingressIP = clusterLBConfig.IngressLBIPs[i]
		} else {
			ingressIP = nil
		}
		newNode, err := getNodeConfig(kubeconfigPath, clusterConfigPath, resolvConfPath, nil, nil, 0, 0, 0, platformType, controlPlaneTopology)
		if err != nil {
			return Node{}, err
		}
		newNode, err = updateNodewithCloudInfo(apiLBIP, apiIntLBIP, ingressIP, resolvConfPath, newNode)
		if err != nil {
			return Node{}, err
		}
		nodes = append(nodes, newNode)
	}
	nodes[0].Configs = &nodes
	return nodes[0], nil
}

func updateNodewithCloudInfo(apiLBIP, apiIntLBIP, ingressIP net.IP, resolvConfPath string, node Node) (updatedNode Node, err error) {
	var validLBIP net.IP
	if apiIntLBIP != nil {
		validLBIP = apiIntLBIP
		node.Cluster.APIIntLBIPs = append(node.Cluster.APIIntLBIPs, apiIntLBIP.String())
	}
	if apiLBIP != nil {
		node.Cluster.APILBIPs = append(node.Cluster.APILBIPs, apiLBIP.String())
	}
	if ingressIP != nil {
		node.Cluster.IngressLBIPs = append(node.Cluster.IngressLBIPs, ingressIP.String())
		if validLBIP == nil {
			validLBIP = ingressIP
		}
	}
	node.Cluster.CloudLBRecordType = "A"
	node.Cluster.CloudLBEmptyType = "AAAA"
	if validLBIP.To4() == nil {
		node.Cluster.CloudLBRecordType = "AAAA"
		node.Cluster.CloudLBEmptyType = "A"
	}
	resolvConfUpstreams, err := getDNSUpstreams(resolvConfPath)
	if err != nil {
		return node, err
	}
	// Extract only useful upstream addresses
	node.DNSUpstreams = make([]string, 0)
	for _, upstream := range resolvConfUpstreams {
		if upstream != "127.0.0.1" && upstream != "::1" {
			log.Infof("Adding %s as DNS Upstream", upstream)
			node.DNSUpstreams = append(node.DNSUpstreams, upstream)
		}
	}
	// Having no DNS Upstream servers is invalid. Return error so init
	// container can retry.
	if len(node.DNSUpstreams) < 1 {
		return node, errors.New("No upstream DNS servers found")
	}
	return node, nil
}

func PopulateCloudLBIPAddresses(clusterLBConfig ClusterLBConfig, node Node) (updatedNode Node, err error) {
	for _, ip := range clusterLBConfig.ApiIntLBIPs {
		node.Cluster.APIIntLBIPs = append(node.Cluster.APIIntLBIPs, ip.String())
	}
	for _, ip := range clusterLBConfig.ApiLBIPs {
		node.Cluster.APILBIPs = append(node.Cluster.APILBIPs, ip.String())
	}
	for _, ip := range clusterLBConfig.IngressLBIPs {
		node.Cluster.IngressLBIPs = append(node.Cluster.IngressLBIPs, ip.String())
	}
	node.Cluster.CloudLBRecordType = "A"
	node.Cluster.CloudLBEmptyType = "AAAA"
	if len(clusterLBConfig.ApiIntLBIPs) > 0 && clusterLBConfig.ApiIntLBIPs[0].To4() == nil {
		node.Cluster.CloudLBRecordType = "AAAA"
		node.Cluster.CloudLBEmptyType = "A"
	}
	return node, nil
}
