package main

import (
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/openshift/baremetal-runtimecfg/pkg/monitor"
)

var log = logrus.New()

func main() {
	var rootCmd = &cobra.Command{
		Use:   "dynkeepalived path_to_kubeconfig path_to_keepalived_cfg_template path_to_config",
		Short: "Monitors runtime external interface for keepalived and reloads if it changes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 3 {
				cmd.Help()
				return nil
			}
			apiVip, err := cmd.Flags().GetIP("api-vip")
			if err != nil {
				apiVip = nil
			}
			ingressVip, err := cmd.Flags().GetIP("ingress-vip")
			if err != nil {
				ingressVip = nil
			}
			dnsVip, err := cmd.Flags().GetIP("dns-vip")
			if err != nil {
				dnsVip = nil
			}
			apiProvisioningVip, err := cmd.Flags().GetIP("api-provisioning-vip")
			if err != nil {
				apiProvisioningVip = nil
			}

			checkInterval, err := cmd.Flags().GetDuration("check-interval")
			if err != nil {
				return err
			}
			clusterConfigPath, err := cmd.Flags().GetString("cluster-config")
			if err != nil {
				return err
			}

			return monitor.KeepalivedWatch(args[0], clusterConfigPath, args[1], args[2], apiVip, ingressVip, dnsVip, apiProvisioningVip, checkInterval)
		},
	}
	rootCmd.PersistentFlags().StringP("cluster-config", "c", "", "Path to cluster-config ConfigMap to retrieve ControlPlane info")
	rootCmd.Flags().Duration("check-interval", time.Second*10, "Time between keepalived watch checks")
	rootCmd.Flags().IP("api-vip", nil, "Virtual IP Address to reach the OpenShift API")
	rootCmd.PersistentFlags().IP("ingress-vip", nil, "Virtual IP Address to reach the OpenShift Ingress Routers")
	rootCmd.PersistentFlags().IP("dns-vip", nil, "Virtual IP Address to reach an OpenShift node resolving DNS server")
	rootCmd.Flags().IP("api-provisioning-vip", nil, "Virtual IP Address to reach Ignition")
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Failed due to %s", err)
	}
}
