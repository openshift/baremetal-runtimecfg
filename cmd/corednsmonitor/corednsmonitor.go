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

			return monitor.CorednsWatch(args[0], clusterConfigPath, args[1], args[2], apiVips, ingressVips, checkInterval)
		},
	}
	rootCmd.PersistentFlags().StringP("cluster-config", "c", "", "Path to cluster-config ConfigMap to retrieve ControlPlane info")
	rootCmd.Flags().Duration("check-interval", time.Second*30, "Time between coredns watch checks")
	rootCmd.Flags().IP("api-vip", nil, "DEPRECATED: Virtual IP Address to reach the OpenShift API")
	rootCmd.Flags().IPSlice("api-vips", nil, "Virtual IP Addresses to reach the OpenShift API")
	rootCmd.Flags().IP("ingress-vip", nil, "DEPRECATED: Virtual IP Address to reach the OpenShift Ingress Routers")
	rootCmd.Flags().IPSlice("ingress-vips", nil, "Virtual IP Addresses to reach the OpenShift Ingress Routers")
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Failed due to %s", err)
	}
}
