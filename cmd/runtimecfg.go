package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"text/template"

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
		},
	}

	var renderCmd = &cobra.Command{
		Use:   "render [path to kubeconfig] [files to render]...",
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
			config, err := config.GetConfig(kubeCfgPath, apiVip, ingressVip, dnsVip)
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

			ext := ".tmpl"
			extLen := len(ext)
			for _, templatePath := range args[1:] {
				if path.Ext(templatePath) != ext {
					return fmt.Errorf("Template %s does not have the right extension. Must be '%s'", templatePath, ext)
				}

				tmpl, err := template.ParseFiles(templatePath)
				if err != nil {
					return err
				}

				baseName := path.Base(templatePath)
				outPath := path.Join(outDir, baseName[:len(baseName)-extLen])
				outFile, err := os.Create(outPath)
				if err != nil {
					return err
				}
				defer outFile.Close()

				fmt.Printf("Rendering %s\n", outPath)
				err = tmpl.Execute(outFile, config)
				if err != nil {
					return err
				}
			}
			return err
		},
	}
	renderCmd.Flags().StringP("out-dir", "o", "", "Directory where the templates will be rendered")

	rootCmd.PersistentFlags().Bool("verbose", false, "Display extra information about the rendering")
	rootCmd.PersistentFlags().IP("api-vip", nil, "Virtual IP Address to reach the OpenShift API")
	rootCmd.PersistentFlags().IP("ingress-vip", nil, "Virtual IP Address to reach the OpenShift Ingress Routers")
	rootCmd.PersistentFlags().IP("dns-vip", nil, "Virtual IP Address to reach an OpenShift node resolving DNS server")

	rootCmd.AddCommand(displayCmd)
	rootCmd.AddCommand(renderCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
