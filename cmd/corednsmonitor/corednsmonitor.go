package main

import (
	"net"
	"time"

	"github.com/openshift/baremetal-runtimecfg/pkg/monitor"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var log = logrus.New()

func main() {
	var rootCmd = &cobra.Command{
		Use:   "corednsmonitor path_to_kubeconfig path_to_keepalived_cfg_template path_to_config",
		Short: "Monitors runtime external interface for Coredns Corefile changes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 3 {
				cmd.Help()
				return nil
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

			checkInterval, err := cmd.Flags().GetDuration("check-interval")
			if err != nil {
				return err
			}
			clusterConfigPath, err := cmd.Flags().GetString("cluster-config")
			if err != nil {
				return err
			}
			cloudExtLBIPs, err := cmd.Flags().GetIPSlice("cloud-ext-lb-ips")
			if err != nil {
				cloudExtLBIPs = []net.IP{}
			}
			cloudIntLBIPs, err := cmd.Flags().GetIPSlice("cloud-int-lb-ips")
			if err != nil {
				cloudIntLBIPs = []net.IP{}
			}
			cloudIngressLBIPs, err := cmd.Flags().GetIPSlice("cloud-ingress-lb-ips")
			if err != nil {
				cloudIngressLBIPs = []net.IP{}
			}
			platformType, err := cmd.Flags().GetString("platform")
			if err != nil {
				platformType = ""
			}

			return monitor.CorednsWatch(args[0], clusterConfigPath, args[1], args[2], apiVips, ingressVips, checkInterval, cloudExtLBIPs, cloudIntLBIPs, cloudIngressLBIPs, platformType)
		},
	}
	rootCmd.PersistentFlags().StringP("cluster-config", "c", "", "Path to cluster-config ConfigMap to retrieve ControlPlane info")
	rootCmd.Flags().Duration("check-interval", time.Second*30, "Time between coredns watch checks")
	rootCmd.Flags().IP("api-vip", nil, "DEPRECATED: Virtual IP Address to reach the OpenShift API")
	rootCmd.Flags().IPSlice("api-vips", nil, "Virtual IP Addresses to reach the OpenShift API")
	rootCmd.Flags().IP("ingress-vip", nil, "DEPRECATED: Virtual IP Address to reach the OpenShift Ingress Routers")
	rootCmd.Flags().IPSlice("ingress-vips", nil, "Virtual IP Addresses to reach the OpenShift Ingress Routers")
	rootCmd.Flags().IPSlice("cloud-ext-lb-ips", nil, "IP Addresses of Cloud External Load Balancers for OpenShift API")
	rootCmd.Flags().IPSlice("cloud-int-lb-ips", nil, "IP Addresses of Cloud Internal Load Balancers for OpenShift Internal API")
	rootCmd.Flags().IPSlice("cloud-ingress-lb-ips", nil, "IP Addresses of Cloud Ingress Load Balancers")
	rootCmd.Flags().StringP("platform", "p", "", "Cluster Platform")

	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Failed due to %s", err)
	}
}
