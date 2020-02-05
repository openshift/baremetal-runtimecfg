package config

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
	"github.com/openshift/installer/pkg/types"
)

var log = logrus.New()

type Cluster struct {
	Name                   string
	Domain                 string
	APIVIP                 string
	APIVirtualRouterID     uint8
	DNSVIP                 string
	DNSVirtualRouterID     uint8
	IngressVIP             string
	IngressVirtualRouterID uint8
	VIPNetmask             int
	MasterAmount           int64
	EtcdBackends           string
}

type Backend struct {
	Host               string
	CanonicalHost      string
	CanonicalShortName string
	Address            string
	Port               uint16
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
	BootstrapIP       string
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

func GetBootstrapIP(apiVip string) (bootstrapIP string, err error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(apiVip, "64444"), 10*time.Second)
	if err != nil {
		log.Infof("An error occurred on dial: %v", err)
		return "", err
	}
	defer conn.Close()

	bootstrapIP, err = bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		log.Infof("An error occurred on read: %v", err)
		return "", err
	}

	bootstrapIP = strings.TrimSpace(bootstrapIP)

	log.Infof("Got bootstrap IP %v", bootstrapIP)

	return bootstrapIP, err
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
	// Try cluster-config.yml first
	clusterName, clusterDomain, err := getClusterConfigClusterNameAndDomain(clusterConfigPath)
	if err != nil {
		// We are using kubeconfig as a fallback for this
		clusterName, clusterDomain, err = GetKubeconfigClusterNameAndDomain(kubeconfigPath)
		if err != nil {
			return node, err
		}
	}
	node.Cluster.Name = clusterName
	node.Cluster.Domain = clusterDomain

	// Add one to the fletcher8 result because 0 is an invalid vrid in
	// keepalived. This is safe because fletcher8 can never return 255 due to
	// the modulo arithmetic that happens. The largest value it can return is
	// 238 (0xEE).
	node.Cluster.APIVirtualRouterID = utils.FletcherChecksum8(node.Cluster.Name+"-api") + 1
	node.Cluster.DNSVirtualRouterID = utils.FletcherChecksum8(node.Cluster.Name+"-dns") + 1
	if node.Cluster.DNSVirtualRouterID == node.Cluster.APIVirtualRouterID {
		node.Cluster.DNSVirtualRouterID++
	}
	node.Cluster.IngressVirtualRouterID = utils.FletcherChecksum8(node.Cluster.Name+"-ingress") + 1
	for node.Cluster.IngressVirtualRouterID == node.Cluster.DNSVirtualRouterID || node.Cluster.IngressVirtualRouterID == node.Cluster.APIVirtualRouterID {
		node.Cluster.IngressVirtualRouterID++
	}

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

	if apiVip != nil {
		node.Cluster.APIVIP = apiVip.String()
	}
	if ingressVip != nil {
		node.Cluster.IngressVIP = ingressVip.String()
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

	prefix, _ := nonVipAddr.Mask.Size()
	node.Cluster.VIPNetmask = prefix
	node.VRRPInterface = vipIface.Name

	domain := fmt.Sprintf("%s.%s", clusterName, clusterDomain)
	node.LBConfig, err = GetLBConfig(domain, apiPort, lbPort, statPort, apiVip)
	if err != nil {
		return node, err
	}
	etcdBackends, err := getSortedBackends(domain)
	if err != nil {
		return node, err
	}
	for _, backend := range etcdBackends {
		if len(node.Cluster.EtcdBackends) > 0 {
			node.Cluster.EtcdBackends += ","
		}
		node.Cluster.EtcdBackends += fmt.Sprintf("https://%s:2379", strings.TrimRight(backend.Host, "."))
	}

	return node, err
}

func getSortedBackends(domain string) (backends []Backend, err error) {
	srvs, err := utils.GetEtcdSRVMembers(domain)
	if err != nil {
		log.WithFields(logrus.Fields{
			"err": err,
		}).Info("Failed to get Etcd SRV members")
		srvs = []*net.SRV{}
		err = nil
	}

	for _, srv := range srvs {
		addr, err := utils.GetFirstAddr(srv.Target)
		if err != nil {
			log.WithFields(logrus.Fields{
				"member": srv.Target,
			}).Error("Failed to get address for member")
			continue
		}

		// Do a reverse lookup to get the canonical hostname as well
		canonicalHost, err := utils.GetFirstHost(addr)
		canonicalShortName := utils.GetShortHostname(canonicalHost)
		backends = append(backends, Backend{Host: srv.Target, Address: addr, Port: srv.Port, CanonicalHost: canonicalHost, CanonicalShortName: canonicalShortName})
	}
	sort.Slice(backends, func(i, j int) bool {
		return backends[i].Address < backends[j].Address
	})
	return backends, err
}

func GetLBConfig(domain string, apiPort, lbPort, statPort uint16, apiVip net.IP) (ApiLBConfig, error) {
	config := ApiLBConfig{
		ApiPort:  apiPort,
		LbPort:   lbPort,
		StatPort: statPort,
	}
	// LB frontend address: IPv6 '::' , IPv4 ''
	if apiVip.To4() == nil {
		config.FrontendAddr = "::"
	}

	backends, err := getSortedBackends(domain)
	if err != nil {
		log.WithFields(logrus.Fields{
			"domain": domain,
		}).Error("Failed to retrieve API member information")
		return config, err
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
