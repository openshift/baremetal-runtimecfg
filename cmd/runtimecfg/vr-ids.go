package main

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/spf13/cobra"
)

var (
	vrIdsCmd = &cobra.Command{
		Use: `vr-ids [cluster name]
			It prints the virtual router ID information for the chosen cluster name`,
		Short: "Prints Virtual Router ID information",
		Args:  cobra.ExactArgs(1),
		RunE:  runVRIds,
	}
)

func init() {
	rootCmd.AddCommand(vrIdsCmd)
}

func runVRIds(cmd *cobra.Command, args []string) error {
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
}
