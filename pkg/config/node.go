package config

import (
	"bufio"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
	"github.com/openshift/installer/pkg/types"
)

const localhostKubeApiServerUrl string = "https://localhost:6443"

var log = logrus.New()

type Cluster struct {
	Name                   string
	Domain                 string
	APIVIP                 string
	APIVirtualRouterID     uint8
	APIVIPRecordType       string
	APIVIPEmptyType        string
	DNSVIP                 string
	DNSVirtualRouterID     uint8
	IngressVIP             string
	IngressVirtualRouterID uint8
	IngressVIPRecordType   string
	IngressVIPEmptyType    string
	VIPNetmask             int
	MasterAmount           int64
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
	Cluster           Cluster
	LBConfig          ApiLBConfig
	NonVirtualIP      string
	ShortHostname     string
	EtcdShortHostname string
	VRRPInterface     string
	DNSUpstreams      []string
	IngressConfig     IngressConfig
	EnableUnicast     bool
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
	c.DNSVirtualRouterID = utils.FletcherChecksum8(c.Name+"-dns") + 1
	if c.DNSVirtualRouterID == c.APIVirtualRouterID {
		c.DNSVirtualRouterID++
	}
	c.IngressVirtualRouterID = utils.FletcherChecksum8(c.Name+"-ingress") + 1
	for c.IngressVirtualRouterID == c.DNSVirtualRouterID || c.IngressVirtualRouterID == c.APIVirtualRouterID {
		c.IngressVirtualRouterID++
	}
	return nil
}

func GetVRRPConfig(apiVip, ingressVip, dnsVip net.IP) (vipIface net.Interface, nonVipAddr *net.IPNet, err error) {
	vips := make([]net.IP, 0)
	if apiVip != nil {
		vips = append(vips, apiVip)
	}
	if ingressVip != nil {
		vips = append(vips, ingressVip)
	}
	if dnsVip != nil {
		vips = append(vips, dnsVip)
	}
	return getInterfaceAndNonVIPAddr(vips)
}

func IsUpgradeStillRunning(kubeconfigPath string) (error, bool) {

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return err, true
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err, true
	}

	nodes, err := clientset.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return err, true
	}

	for _, node := range nodes.Items {
		if node.Annotations["machineconfiguration.openshift.io/desiredConfig"] != node.Annotations["machineconfiguration.openshift.io/currentConfig"] ||
			node.Annotations["machineconfiguration.openshift.io/desiredConfig"] != nodes.Items[0].Annotations["machineconfiguration.openshift.io/desiredConfig"] {

			return nil, true
		}
	}

	return nil, false
}

func GetIngressConfig(kubeconfigPath string) (ingressConfig IngressConfig, err error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return ingressConfig, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return ingressConfig, err
	}

	nodes, err := clientset.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return ingressConfig, err
	}

	for _, node := range nodes.Items {
		for _, address := range node.Status.Addresses {
			if address.Type == v1.NodeInternalIP {
				ingressConfig.Peers = append(ingressConfig.Peers, address.Address)
			}
		}
	}

	return ingressConfig, nil
}

func GetConfig(kubeconfigPath, clusterConfigPath, resolvConfPath string, apiVip net.IP, ingressVip net.IP, dnsVip net.IP, apiPort, lbPort, statPort uint16) (node Node, err error) {
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

	node.EtcdShortHostname, err = utils.EtcdShortHostname()
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
	if dnsVip != nil {
		node.Cluster.DNSVIP = dnsVip.String()
	}
	vipIface, nonVipAddr, err := GetVRRPConfig(apiVip, ingressVip, dnsVip)
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
		if upstream != node.NonVirtualIP && upstream != node.Cluster.DNSVIP && upstream != "127.0.0.1" && upstream != "::1" {
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
func getSortedBackends(kubeconfigPath string, readFromLocalAPI bool) (backends []Backend, err error) {

	kubeApiServerUrl := ""
	if readFromLocalAPI {
		kubeApiServerUrl = localhostKubeApiServerUrl
	}
	config, err := clientcmd.BuildConfigFromFlags(kubeApiServerUrl, kubeconfigPath)
	if err != nil {
		log.WithFields(logrus.Fields{
			"err": err,
		}).Info("Failed to get client config")
		return []Backend{}, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.WithFields(logrus.Fields{
			"err": err,
		}).Info("Failed to get client")
		return []Backend{}, err
	}
	nodes, err := clientset.CoreV1().Nodes().List(metav1.ListOptions{
		LabelSelector: "node-role.kubernetes.io/master=",
	})
	if err != nil {
		log.WithFields(logrus.Fields{
			"err": err,
		}).Info("Failed to get master Nodes list")
		return []Backend{}, err
	}
	for _, node := range nodes.Items {
		masterIp := ""
		for _, address := range node.Status.Addresses {
			if address.Type == v1.NodeInternalIP {
				masterIp = address.Address
				break
			}
		}
		if masterIp != "" {
			backends = append(backends, Backend{Host: node.ObjectMeta.Name, Address: masterIp})
		} else {
			log.Warnf("Could not retrieve node's IP for %s", node.ObjectMeta.Name)
		}
	}

	sort.Slice(backends, func(i, j int) bool {
		return backends[i].Address < backends[j].Address
	})
	return backends, err
}

func GetLBConfig(kubeconfigPath string, apiPort, lbPort, statPort uint16, apiVip net.IP) (ApiLBConfig, error) {
	config := ApiLBConfig{
		ApiPort:  apiPort,
		LbPort:   lbPort,
		StatPort: statPort,
	}

	// LB frontend address: IPv6 '::' , IPv4 ''
	if apiVip.To4() == nil {
		config.FrontendAddr = "::"
	}
	// Try reading master nodes details first from api-vip:kube-apiserver and failover to localhost:kube-apiserver
	backends, err := getSortedBackends(kubeconfigPath, false)
	if err != nil {
		log.Infof("An error occurred while trying to read master nodes details from api-vip:kube-apiserver: %v", err)
		log.Infof("Trying to read master nodes details from localhost:kube-apiserver")
		backends, err = getSortedBackends(kubeconfigPath, true)
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
