package config

import (
	"net"
	"net/url"
	"strings"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
)

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
}

type Node struct {
	Cluster       Cluster
	NonVirtualIP  string
	ShortHostname string
	VRRPInterface string
}

func GetConfig(kubeconfigPath string, apiVip net.IP, ingressVip net.IP, dnsVip net.IP) (node Node, err error) {
	kubeCfg, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return node, err
	}

	ctxt := kubeCfg.Contexts[kubeCfg.CurrentContext]
	cluster := kubeCfg.Clusters[ctxt.Cluster]
	serverUrl, err := url.Parse(cluster.Server)
	if err != nil {
		return node, err
	}

	apiHostname := serverUrl.Hostname()
	apiHostnameSlices := strings.SplitN(apiHostname, ".", 3)
	node.Cluster.Name = apiHostnameSlices[1]
	node.Cluster.Domain = apiHostnameSlices[2]

	node.Cluster.APIVirtualRouterID = utils.FletcherChecksum8(node.Cluster.Name + "-api")
	node.Cluster.DNSVirtualRouterID = utils.FletcherChecksum8(node.Cluster.Name + "-dns")
	node.Cluster.IngressVirtualRouterID = utils.FletcherChecksum8(node.Cluster.Name + "-ingress")

	// Node
	node.ShortHostname, err = utils.ShortHostname()
	if err != nil {
		return node, err
	}

	vips := make([]net.IP, 0)
	if apiVip != nil {
		vips = append(vips, apiVip)
		node.Cluster.APIVIP = apiVip.String()
	}
	if ingressVip != nil {
		vips = append(vips, ingressVip)
		node.Cluster.IngressVIP = ingressVip.String()
	}
	if dnsVip != nil {
		vips = append(vips, dnsVip)
		node.Cluster.DNSVIP = dnsVip.String()
	}
	vipIface, nonVipAddr, err := getInterfaceAndNonVIPAddr(vips)
	if err != nil {
		return node, err
	}
	node.NonVirtualIP = nonVipAddr.IP.String()
	prefix, _ := nonVipAddr.Mask.Size()
	node.Cluster.VIPNetmask = prefix
	node.VRRPInterface = vipIface.Name

	return node, err
}
