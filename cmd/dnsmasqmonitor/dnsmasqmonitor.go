package main

import (
	"time"

	"github.com/openshift/baremetal-runtimecfg/pkg/monitor"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var log = logrus.New()

func main() {
	var rootCmd = &cobra.Command{
		Use:   "dnsmasqmonitor path_to_kubeconfig path_to_host_file_cfg_template path_to_config",
		Short: "Monitors dnsmasq host configmap",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 3 {
				cmd.Help()
				return nil
			}
			apiVip, err := cmd.Flags().GetIP("api-vip")
			if err != nil {
				apiVip = nil
			}

			checkInterval, err := cmd.Flags().GetDuration("check-interval")
			if err != nil {
				return err
			}

			return monitor.DnsmasqWatch(args[0], args[1], args[2], apiVip, checkInterval)
		},
	}
	rootCmd.Flags().Duration("check-interval", time.Second*30, "Time between coredns watch checks")
	rootCmd.Flags().IP("api-vip", nil, "Virtual IP Address to reach the OpenShift API")
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Failed due to %s", err)
	}
}
