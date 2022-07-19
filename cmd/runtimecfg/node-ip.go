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
	interfaceFile          = "/run/nodeip-configuration/interface"
	primaryFile            = "/run/nodeip-configuration/primary-ip"
)

var retry, preferIPv6 bool
var nodeIPCmd = &cobra.Command{
	Use:                   "node-ip",
	DisableFlagsInUseLine: true,
	Short:                 "Node IP tools",
	Long:                  "Node IP has tools that aid in the configuration of the default node IP",
}

var nodeIPShowCmd = &cobra.Command{
	Use:                   "show [Virtual IP...]",
	DisableFlagsInUseLine: true,
	Short:                 "Show a configured IP address that directly routes to the given Virtual IPs. If no Virtual IPs are provided or if the node isn't attached to the VIP subnet, it will pick an IP associated with the default route.",
	Args:                  cobra.MinimumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		err := show(cmd, args)
		if err != nil {
			log.Fatalf("error in node-ip show: %v\n", err)
		}
	},
}

var nodeIPSetCmd = &cobra.Command{
	Use:                   "set [Virtual IP...]",
	DisableFlagsInUseLine: true,
	Short:                 "Sets container runtime services to bind to a configured IP address that directly routes to the given virtual IPs. If no Virtual IPs are provided or if the node isn't attached to the VIP subnet, it will pick an IP associated with the default route.",
	Args:                  cobra.MinimumNArgs(0),
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
	nodeIPCmd.PersistentFlags().BoolVarP(&preferIPv6, "prefer-ipv6", "6", false, "Prefer IPv6 addresses to IPv4")
	rootCmd.AddCommand(nodeIPCmd)
}

func show(cmd *cobra.Command, args []string) error {
	vips, err := parseIPs(args)
	if err != nil {
		return err
	}

	chosenAddresses, err := getSuitableIPs(retry, vips, preferIPv6)
	if err != nil {
		return err
	}
	log.Infof("Chosen Node IPs: %v", chosenAddresses)

	fmt.Println(chosenAddresses[0])
	return nil
}

func set(cmd *cobra.Command, args []string) error {
	vips, err := parseIPs(args)
	if err != nil {
		return err
	}

	chosenAddresses, err := getSuitableIPs(retry, vips, preferIPv6)
	if err != nil {
		return err
	}
	log.Infof("Chosen Node IPs: %v", chosenAddresses)

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

	nodeIP := chosenAddresses[0].Address.String()
	nodeIPs := nodeIP
	if len(chosenAddresses) > 1 {
		nodeIPs += "," + chosenAddresses[1].Address.String()
	}
	kOverrideContent := fmt.Sprintf("[Service]\nEnvironment=\"KUBELET_NODE_IP=%s\" \"KUBELET_NODE_IPS=%s\"\n", nodeIP, nodeIPs)
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

	cOverrideContent := fmt.Sprintf("[Service]\nEnvironment=\"CONTAINER_STREAM_ADDRESS=%s\"\n", chosenAddresses[0].Address)
	log.Infof("Writing CRI-O service override with content %s", cOverrideContent)
	_, err = cOverride.WriteString(cOverrideContent)
	if err != nil {
		return err
	}

	// Set interface in case it is not br-ex that means that it was already configured and node is rebooting
	// in that case don't change the previous written interface
	if chosenAddresses[0].LinkName != "br-ex" {
		interfaceDir := filepath.Dir(interfaceFile)
		err = os.MkdirAll(interfaceDir, 0755)
		if err != nil {
			return err
		}
		log.Infof("Opening interface file %s", interfaceFile)
		iFile, err := os.Create(interfaceFile)
		if err != nil {
			return err
		}
		defer iFile.Close()
		interfaceContent := fmt.Sprintf("NODE_IP_INTERFACE=%s\"\n", chosenAddresses[0].LinkName)
		log.Infof("Writing interface file content %s", interfaceContent)
		_, err = iFile.WriteString(interfaceContent)
		if err != nil {
			return err
		}
	} else {
		log.Infof("Skipping set interface file as bridge was already configured on the expected interface")
	}

	// set node ip for those who wants to use it
	primaryIpDir := filepath.Dir(primaryFile)
	err = os.MkdirAll(primaryIpDir, 0755)
	if err != nil {
		return err
	}
	log.Infof("Opening primary ip file %s", primaryFile)
	primaryIPFile, err := os.Create(primaryFile)
	if err != nil {
		return err
	}
	defer primaryIPFile.Close()
	primaryIPContent := fmt.Sprintf("NODE_IP_PRIMARY_IP=%s\"\n", nodeIP)
	log.Infof("Writing node ip primary file with content %s", primaryIPContent)
	_, err = primaryIPFile.WriteString(primaryIPContent)
	if err != nil {
		return err
	}

	return nil
}

func getSuitableIPs(retry bool, vips []net.IP, preferIPv6 bool) (chosen []utils.FoundAddress, err error) {
	// Enable debug logging in utils package
	utils.SetDebugLogLevel()
	for {
		if len(vips) > 0 {
			chosen, err = utils.AddressesRouting(vips, utils.ValidNodeAddress)
			if len(chosen) > 0 || err != nil {
				// If using IPv6, verify that the choosen address isn't tentative
				// i.e. we can actually bind to it
				if len(chosen) > 0 && net.IPv6len == len(chosen[0].Address) {
					_, err := net.Listen("tcp", "["+chosen[0].Address.String()+"]:")
					if err != nil {
						log.Errorf("Chosen node IP is not usable")
						if !retry {
							return nil, fmt.Errorf("Failed to find node IP")
						}
						time.Sleep(time.Second)
						continue
					}
				}
				return chosen, err
			}
		}
		if len(chosen) == 0 {
			chosen, err = utils.AddressesDefault(preferIPv6, utils.ValidNodeAddress)
			if len(chosen) > 0 || err != nil {
				return chosen, err
			}
		}
		if !retry {
			return nil, fmt.Errorf("Failed to find node IP")
		}

		log.Errorf("Failed to find a suitable node IP")
		time.Sleep(time.Second)
	}
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
