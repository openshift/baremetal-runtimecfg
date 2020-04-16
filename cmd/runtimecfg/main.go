package main

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:   "runtimecfg",
		Short: "baremetal-runtimecfg discovers OpenShift cluster and node configuration and renders Go templates",
		Long: `A small utitily that reads KubeConfig and checks the current system for rendering OpenShift baremetal networking configuration.
                Complete documentation is available at http://github.com/openshift/baremetal-runtimecfg`,
	}
	log = logrus.New()
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Error executing runtimecfg: %v", err)
	}
}
