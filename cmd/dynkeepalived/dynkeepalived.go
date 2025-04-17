package main

import (
	"net"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/openshift/baremetal-runtimecfg/pkg/monitor"
)

var log = logrus.New()

func main() {
	var rootCmd = &cobra.Command{
		Use:          "dynkeepalived path_to_kubeconfig path_to_keepalived_cfg_template path_to_config",
		Short:        "Monitors runtime external interface for keepalived and reloads if it changes",
		SilenceUsage: true,
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
			apiPort, err := cmd.Flags().GetUint16("api-port")
			if err != nil {
				return err
			}
			lbPort, err := cmd.Flags().GetUint16("lb-port")
			if err != nil {
				return err
			}

			checkInterval, err := cmd.Flags().GetDuration("check-interval")
			if err != nil {
				return err
			}
			clusterConfigPath, err := cmd.Flags().GetString("cluster-config")
			if err != nil {
				return err
			}
			platformType, err := cmd.Flags().GetString("platform")
			if err != nil {
				platformType = ""
			}

			return monitor.KeepalivedWatch(args[0], clusterConfigPath, args[1], args[2], apiVips, ingressVips, apiPort, lbPort, checkInterval, platformType)
		},
	}
	rootCmd.PersistentFlags().StringP("cluster-config", "c", "", "Path to cluster-config ConfigMap to retrieve ControlPlane info")
	rootCmd.Flags().Duration("check-interval", time.Second*10, "Time between keepalived watch checks")
	rootCmd.Flags().IP("api-vip", nil, "DEPRECATED: Virtual IP Address to reach the OpenShift API")
	rootCmd.Flags().IPSlice("api-vips", nil, "Virtual IP Addresses to reach the OpenShift API")
	rootCmd.Flags().IP("ingress-vip", nil, "DEPRECATED: Virtual IP Address to reach the OpenShift Ingress Routers")
	rootCmd.Flags().IPSlice("ingress-vips", nil, "Virtual IP Addresses to reach the OpenShift Ingress Routers")
	rootCmd.PersistentFlags().IP("dns-vip", nil, "DEPRECATED: Virtual IP Address to reach an OpenShift node resolving DNS server")
	rootCmd.Flags().Uint16("api-port", 6443, "Port where the OpenShift API listens")
	rootCmd.Flags().Uint16("lb-port", 9445, "Port where the API HAProxy LB will listen")
	rootCmd.Flags().StringP("platform", "p", "", "Cluster Platform")
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Failed due to %s", err)
	}
}
