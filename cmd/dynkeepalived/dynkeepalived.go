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

			return monitor.KeepalivedWatch(args[0], clusterConfigPath, args[1], args[2], apiVip, ingressVip, apiPort, lbPort, checkInterval)
		},
	}
	rootCmd.PersistentFlags().StringP("cluster-config", "c", "", "Path to cluster-config ConfigMap to retrieve ControlPlane info")
	rootCmd.Flags().Duration("check-interval", time.Second*10, "Time between keepalived watch checks")
	rootCmd.Flags().IP("api-vip", nil, "Virtual IP Address to reach the OpenShift API")
	rootCmd.PersistentFlags().IP("ingress-vip", nil, "Virtual IP Address to reach the OpenShift Ingress Routers")
	rootCmd.PersistentFlags().IP("dns-vip", nil, "Virtual IP Address to reach an OpenShift node resolving DNS server")
	rootCmd.Flags().Uint16("api-port", 6443, "Port where the OpenShift API listens")
	rootCmd.Flags().Uint16("lb-port", 9445, "Port where the API HAProxy LB will listen")
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Failed due to %s", err)
	}
}
