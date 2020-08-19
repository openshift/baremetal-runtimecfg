package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/spf13/cobra"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/render"
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
			config, err := config.GetConfig(kubeCfgPath, clusterConfigPath, resolveConfPath, apiVip, ingressVip, dnsVip, apiPort, lbPort, statPort)
			if err != nil {
				return err
			}

			spew.Dump(config)
			return err
		},
	}

	var renderCmd = &cobra.Command{
		Use: `render [path to kubeconfig] [paths to render]...
		        If there is one single path and it is a directory, it renders the .tmpl files in it`,
		Short: "Renders go templates with the runtime configuration",
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
			config, err := config.GetConfig(kubeCfgPath, clusterConfigPath, resolveConfPath, apiVip, ingressVip, dnsVip, apiPort, lbPort, statPort)
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
		},
	}

	var vrIdsCmd = &cobra.Command{
		Use: `vr-ids [cluster name]
		        It prints the virtual router ID information for the chosen cluster name`,
		Short: "Prints Virtual Router ID information",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := config.Cluster{Name: args[0]}
			err := c.PopulateVRIDs()
			if err != nil {
				return err
			}

			v := reflect.ValueOf(c)
			clusterType := v.Type()
			for i := 0; i < v.NumField(); i++ {
				fieldName := clusterType.Field(i).Name
				if strings.HasSuffix(fieldName, "RouterID") {
					fmt.Printf("%s: %d\n", fieldName, v.Field(i).Interface())
				}
			}
			return nil
		},
	}
	renderCmd.Flags().StringP("out-dir", "o", "", "Directory where the templates will be rendered")

	rootCmd.PersistentFlags().StringP("cluster-config", "c", "", "Path to cluster-config ConfigMap to retrieve ControlPlane info")
	rootCmd.PersistentFlags().Bool("verbose", false, "Display extra information about the rendering")
	rootCmd.PersistentFlags().IP("api-vip", nil, "Virtual IP Address to reach the OpenShift API")
	rootCmd.PersistentFlags().IP("ingress-vip", nil, "Virtual IP Address to reach the OpenShift Ingress Routers")
	rootCmd.PersistentFlags().IP("dns-vip", nil, "Virtual IP Address to reach an OpenShift node resolving DNS server")
	rootCmd.PersistentFlags().Uint16("api-port", 6443, "Port where the OpenShift API listens at")
	rootCmd.PersistentFlags().Uint16("lb-port", 9445, "Port where the API HAProxy LB will listen at")
	rootCmd.PersistentFlags().Uint16("stat-port", 50000, "Port where the HAProxy stats API will listen at")
	rootCmd.PersistentFlags().StringP("resolvconf-path", "r", "/etc/resolv.conf", "Optional path to a resolv.conf file to use to get upstream DNS servers")

	rootCmd.AddCommand(displayCmd)
	rootCmd.AddCommand(renderCmd)
	rootCmd.AddCommand(vrIdsCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
