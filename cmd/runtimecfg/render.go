package main

import (
	"io/ioutil"
	"net"
	"os"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/render"
	"github.com/spf13/cobra"
)

var (
	renderCmd = &cobra.Command{
		Use: `render [path to kubeconfig] [paths to render]...
		        If there is one single path and it is a directory, it renders the .tmpl files in it`,
		Short: "Renders go templates with the runtime configuration",
		RunE:  runRender,
	}
)

func init() {
	renderCmd.Flags().StringP("out-dir", "o", "", "Directory where the templates will be rendered")

	renderCmd.Flags().StringP("cluster-config", "c", "", "Path to cluster-config ConfigMap to retrieve ControlPlane info")
	renderCmd.Flags().Bool("verbose", false, "Display extra information about the rendering")
	renderCmd.Flags().IP("api-vip", nil, "DEPRECATED: Virtual IP Address to reach the OpenShift API")
	renderCmd.Flags().IPSlice("api-vips", nil, "Virtual IP Addresses to reach the OpenShift API")
	renderCmd.Flags().IP("ingress-vip", nil, "DEPRECATED: Virtual IP Address to reach the OpenShift Ingress Routers")
	renderCmd.Flags().IPSlice("ingress-vips", nil, "Virtual IP Addresses to reach the OpenShift Ingress Routers")
	renderCmd.Flags().IP("dns-vip", nil, "DEPRECATED: Virtual IP Address to reach an OpenShift node resolving DNS server")
	renderCmd.Flags().Uint16("api-port", 6443, "Port where the OpenShift API listens at")
	renderCmd.Flags().Uint16("lb-port", 9445, "Port where the API HAProxy LB will listen at")
	renderCmd.Flags().Uint16("stat-port", 29445, "Port where the HAProxy stats API will listen at")
	renderCmd.Flags().StringP("resolvconf-path", "r", "/etc/resolv.conf", "Optional path to a resolv.conf file to use to get upstream DNS servers")
	renderCmd.Flags().IPSlice("cloud-ext-lb-ips", nil, "IP Addresses of Cloud External Load Balancers for OpenShift API")
	renderCmd.Flags().IPSlice("cloud-int-lb-ips", nil, "IP Addresses of Cloud Internal Load Balancers for OpenShift Internal API")
	renderCmd.Flags().IPSlice("cloud-ingress-lb-ips", nil, "IP Addresses of Cloud Ingress Load Balancers")
	renderCmd.Flags().StringP("platform", "p", "", "Cluster Platform")
	rootCmd.AddCommand(renderCmd)
}

func runRender(cmd *cobra.Command, args []string) error {
	kubeCfgPath := "./kubeconfig"
	if len(args) > 0 {
		kubeCfgPath = args[0]
	}

	apiVip, err := cmd.Flags().GetIP("api-vip")
	if err != nil {
		apiVip = nil
	}
	apiVips, err := cmd.Flags().GetIPSlice("api-vips")
	if err != nil {
		apiVips = []net.IP{}
	}
	// If we were passed a VIP using the old interface, coerce it into the list
	// format that the rest of the code now expects.
	if len(apiVips) < 1 && apiVip != nil {
		apiVips = []net.IP{apiVip}
	}
	ingressVip, err := cmd.Flags().GetIP("ingress-vip")
	if err != nil {
		ingressVip = nil
	}
	ingressVips, err := cmd.Flags().GetIPSlice("ingress-vips")
	if err != nil {
		ingressVips = []net.IP{}
	}
	// If we were passed a VIP using the old interface, coerce it into the list
	// format that the rest of the code now expects.
	if len(ingressVips) < 1 && ingressVip != nil {
		ingressVips = []net.IP{ingressVip}
	}
	apiPort, err := cmd.Flags().GetUint16("api-port")
	if err != nil {
		return err
	}
	lbPort, err := cmd.Flags().GetUint16("lb-port")
	if err != nil {
		return err
	}
	statPort, err := cmd.Flags().GetUint16("stat-port")
	if err != nil {
		return err
	}
	clusterConfigPath, err := cmd.Flags().GetString("cluster-config")
	if err != nil {
		return err
	}
	resolveConfPath, err := cmd.Flags().GetString("resolvconf-path")
	if err != nil {
		return err
	}

	apiLBIPs, err := cmd.Flags().GetIPSlice("cloud-ext-lb-ips")
	if err != nil {
		apiLBIPs = []net.IP{}
	}
	apiIntLBIPs, err := cmd.Flags().GetIPSlice("cloud-int-lb-ips")
	if err != nil {
		apiIntLBIPs = []net.IP{}
	}
	ingressLBIPs, err := cmd.Flags().GetIPSlice("cloud-ingress-lb-ips")
	if err != nil {
		ingressLBIPs = []net.IP{}
	}
	platformType, err := cmd.Flags().GetString("platform")
	if err != nil {
		platformType = ""
	}

	clusterLBConfig := config.ClusterLBConfig{ApiLBIPs: apiLBIPs, ApiIntLBIPs: apiIntLBIPs, IngressLBIPs: ingressLBIPs}
	config, err := config.GetConfig(kubeCfgPath, clusterConfigPath, resolveConfPath, apiVips, ingressVips, apiPort, lbPort, statPort, clusterLBConfig, platformType, "")
	if err != nil {
		return err
	}

	outDir, err := cmd.Flags().GetString("out-dir")
	if outDir == "" {
		outDir, err = ioutil.TempDir("", "runtimecfg")
		if err != nil {
			return err
		}
	}
	err = os.MkdirAll(outDir, os.ModePerm)
	if err != nil {
		return err
	}

	return render.Render(outDir, args[1:], config)
}
