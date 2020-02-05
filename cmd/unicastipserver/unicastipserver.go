package main

import (
	"fmt"
	"os"

	"github.com/openshift/baremetal-runtimecfg/pkg/monitor"
	"github.com/spf13/cobra"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "unicastipserver [path to kubeconfig] [api-vip] [dns-vip] [ingress-vip] [api-provisioning-vip]",
		Short: "baremetal-runtimecfg discovers OpenShift cluster and node configuration and renders Go templates",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			unicastipServerPort, err := cmd.Flags().GetUint16("unicastip-server-port")
			if err != nil {
				return err
			}

			return monitor.UnicastIPServer(apiVip, ingressVip, dnsVip, unicastipServerPort)
		},
	}

	rootCmd.Flags().IP("api-vip", nil, "Virtual IP Address to reach the OpenShift API")
	rootCmd.Flags().IP("ingress-vip", nil, "Virtual IP Address to reach the OpenShift Ingress Routers")
	rootCmd.Flags().IP("dns-vip", nil, "Virtual IP Address to reach an OpenShift node resolving DNS server")
	rootCmd.Flags().Uint16("unicastip-server-port", 64444, "Port where the OpenShift API listens at")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
