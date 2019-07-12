package main

import (
	"fmt"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/spf13/cobra"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "runtimecfg",
		Short: "baremetal-runtimecfg discovers OpenShift cluster and node configuration and renders Go templates",
		Long: `A small utitily that reads KubeConfig and checks the current system for rendering OpenShift baremetal networking configuration.
                Complete documentation is available at http://github.com/openshift/baremetal-runtimecfg`,
	}
	var displayCmd = &cobra.Command{
		Use: "display [path to kubeconfig]",
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeCfgPath := "./kubeconfig"
			if len(args) > 0 {
				kubeCfgPath = args[0]
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
			config, err := config.GetConfig(kubeCfgPath, apiVip, ingressVip, dnsVip)
			if err != nil {
				return err
			}

			spew.Dump(config)
			return err

			//t := template.Must(template.New("baremetal-tmpl").ParseGlob("*.tmpl"))
			//err = t.Execute(os.Stdout, config)
			//if err != nil {
			//panic(err)
			//}
		},
	}

	//var renderCmd =

	rootCmd.PersistentFlags().Bool("verbose", false, "Display extra information about the rendering")
	rootCmd.PersistentFlags().IP("api-vip", nil, "Virtual IP Address to reach the OpenShift API")
	rootCmd.PersistentFlags().IP("ingress-vip", nil, "Virtual IP Address to reach the OpenShift Ingress Routers")
	rootCmd.PersistentFlags().IP("dns-vip", nil, "Virtual IP Address to reach an OpenShift node resolving DNS server")

	rootCmd.AddCommand(displayCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
