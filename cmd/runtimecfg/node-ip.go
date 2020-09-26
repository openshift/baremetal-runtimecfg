package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	// Enable sha256 in container image references
	_ "crypto/sha256"

	"github.com/spf13/cobra"

	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
)

const (
	kubeletSvcOverridePath = "/etc/systemd/system/kubelet.service.d/20-nodenet.conf"
	crioSvcOverridePath    = "/etc/systemd/system/crio.service.d/20-nodenet.conf"
)

var retry bool
var nodeIPCmd = &cobra.Command{
	Use:                   "node-ip",
	DisableFlagsInUseLine: true,
	Short:                 "Node IP tools",
	Long:                  "Node IP has tools that aid in the configuration of nodes in platforms that use Virtual IPs",
}

var nodeIPShowCmd = &cobra.Command{
	Use:                   "show [Virtual IP]",
	DisableFlagsInUseLine: true,
	Short:                 "Show a configured IP address that directly routes to the given Virtual IPs",
	Args:                  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		err := show(cmd, args)
		if err != nil {
			log.Fatalf("error in node-ip show: %v\n", err)
		}
	},
}

var nodeIPSetCmd = &cobra.Command{
	Use:                   "set [Virtual IP]",
	DisableFlagsInUseLine: true,
	Short:                 "Sets container runtime services to bind to a configured IP address that directly routes to the given virtual IPs",
	Args:                  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		err := set(cmd, args)
		if err != nil {
			log.Fatalf("error in node-ip set: %v\n", err)
		}
	},
}

// init executes upon import
func init() {
	nodeIPCmd.AddCommand(nodeIPShowCmd)
	nodeIPCmd.AddCommand(nodeIPSetCmd)
	nodeIPCmd.PersistentFlags().BoolVarP(&retry, "retry-on-failure", "r", false, "Keep retrying until it finds a suitable IP address. System errors will still abort")
	rootCmd.AddCommand(nodeIPCmd)
}

func show(cmd *cobra.Command, args []string) error {
	vips, err := parseIPs(args)
	if err != nil {
		return err
	}

	chosenAddress, err := getSuitableIP(retry, vips)
	if err != nil {
		return err
	}
	log.Infof("Chosen Node IP %s", chosenAddress)

	fmt.Println(chosenAddress)
	return nil
}

func set(cmd *cobra.Command, args []string) error {
	vips, err := parseIPs(args)
	if err != nil {
		return err
	}

	chosenAddress, err := getSuitableIP(retry, vips)
	if err != nil {
		return err
	}
	log.Infof("Chosen Node IP %s", chosenAddress)

	// Kubelet
	kubeletOverrideDir := filepath.Dir(kubeletSvcOverridePath)
	err = os.MkdirAll(kubeletOverrideDir, 0755)
	if err != nil {
		return err
	}
	log.Infof("Opening Kubelet service override path %s", kubeletSvcOverridePath)
	kOverride, err := os.Create(kubeletSvcOverridePath)
	if err != nil {
		return err
	}
	defer kOverride.Close()

	kOverrideContent := fmt.Sprintf("[Service]\nEnvironment=\"KUBELET_NODE_IP=%s\"\n", chosenAddress)
	log.Infof("Writing Kubelet service override with content %s", kOverrideContent)
	_, err = kOverride.WriteString(kOverrideContent)
	if err != nil {
		return err
	}

	// CRI-O
	crioOverrideDir := filepath.Dir(crioSvcOverridePath)
	err = os.MkdirAll(crioOverrideDir, 0755)
	if err != nil {
		return err
	}
	log.Infof("Opening CRI-O service override path %s", crioSvcOverridePath)
	cOverride, err := os.Create(crioSvcOverridePath)
	if err != nil {
		return err
	}
	defer cOverride.Close()

	cOverrideContent := fmt.Sprintf("[Service]\nEnvironment=\"CONTAINER_STREAM_ADDRESS=%s\"\n", chosenAddress)
	log.Infof("Writing CRI-O service override with content %s", cOverrideContent)
	_, err = cOverride.WriteString(cOverrideContent)
	if err != nil {
		return err
	}
	return nil
}

func getSuitableIP(retry bool, vips []net.IP) (chosen net.IP, err error) {
	for {
		nodeAddrs, err := utils.AddressesRouting(vips, utils.NonDeprecatedAddress)
		if err != nil {
			return nil, err
		}

		if len(nodeAddrs) > 0 {
			chosen = nodeAddrs[0]
			break
		}
		if !retry {
			return nil, fmt.Errorf("Failed to find node IP")
		}

		log.Errorf("Failed to find a suitable node IP")
		time.Sleep(time.Second)
	}
	return
}

func parseIPs(args []string) ([]net.IP, error) {
	ips := make([]net.IP, len(args))
	for i, arg := range args {
		ips[i] = net.ParseIP(arg)
		if ips[i] == nil {
			return nil, fmt.Errorf("Failed to parse IP address %s", arg)
		}
		log.Infof("Parsed Virtual IP %s", ips[i])
	}
	return ips, nil
}
