package main

import (
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/monitor"
)

var log = logrus.New()

func main() {
	var rootCmd = &cobra.Command{
		Use:   "monitor path_to_kubeconfig path_to_haproxy_cfg_template path_to_config",
		Short: "Monitors master membership and updates HAProxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 3 {
				cmd.Help()
				return nil
			}
			clusterName, clusterDomain, err := config.GetClusterNameAndDomainFromAPIServer(args[0])
			if err != nil {
				return err
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

			checkInterval, err := cmd.Flags().GetDuration("check-interval")
			if err != nil {
				return err
			}

			apiVip, err := cmd.Flags().GetIP("api-vip")
			if err != nil {
				return err
			}
			return monitor.Monitor(args[0], clusterName, clusterDomain, args[1], args[2], apiVip.String(), apiPort, lbPort, statPort, checkInterval)
		},
	}
	rootCmd.Flags().Uint16("api-port", 6443, "Port where the OpenShift API listens")
	rootCmd.Flags().Uint16("lb-port", 9445, "Port where the API HAProxy LB will listen")
	rootCmd.Flags().Uint16("stat-port", 30000, "Port where the HAProxy stats API will listen")
	rootCmd.Flags().Duration("check-interval", time.Second*6, "Time between monitor checks")
	rootCmd.Flags().IP("api-vip", nil, "Virtual IP Address to reach the OpenShift API")
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Failed due to %s", err)
	}
}
