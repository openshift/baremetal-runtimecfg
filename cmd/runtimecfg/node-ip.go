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

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
)

const (
	kubeletSvcOverridePath = "/etc/systemd/system/kubelet.service.d/20-nodenet.conf"
	nodeIpFile             = "/run/nodeip-configuration/primary-ip"
	nodeIpIpV6File         = config.NodeIpIpV6File
	nodeIpIpV4File         = config.NodeIpIpV4File
	crioSvcOverridePath    = "/etc/systemd/system/crio.service.d/20-nodenet.conf"
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
	// If primary ip address was already created, it means that nodeip-configuration has run already and no need to
	// choose new ip, we should leave same configuration as we already set
	if ip, err := config.GetIpFromFile(nodeIpFile); err == nil {
		log.Infof("Found ip %s in %s, no need for new configuration, exiting", ip, nodeIpFile)
		return nil
	}

	vips, err := parseIPs(args)
	if err != nil {
		return err
	}

	chosenAddresses, err := getSuitableIPs(retry, vips, preferIPv6)
	if err != nil {
		return err
	}
	log.Infof("Chosen Node IPs: %v", chosenAddresses)

	nodeIP := chosenAddresses[0].String()
	nodeIPs := nodeIP
	if len(chosenAddresses) > 1 {
		nodeIPs += "," + chosenAddresses[1].String()
	}
	// Kubelet
	kOverrideContent := fmt.Sprintf("[Service]\nEnvironment=\"KUBELET_NODE_IP=%s\" \"KUBELET_NODE_IPS=%s\"\n", nodeIP, nodeIPs)
	log.Infof("Writing Kubelet service override with content %s", kOverrideContent)
	err = writeToFile(kubeletSvcOverridePath, kOverrideContent)
	if err != nil {
		return err
	}

	// CRI-O
	cOverrideContent := fmt.Sprintf("[Service]\nEnvironment=\"CONTAINER_STREAM_ADDRESS=%s\"\n", nodeIP)
	log.Infof("Writing CRIO service override with content %s", cOverrideContent)
	err = writeToFile(crioSvcOverridePath, cOverrideContent)
	if err != nil {
		return err
	}

	// node ip hint for all other services
	err = writeToFile(nodeIpFile, nodeIP)
	if err != nil {
		return err
	}

	ipv6Created, ipv4Created := false, false
	for i := 0; i < len(chosenAddresses) && i < 2; i++ {
		if utils.IsIPv6(chosenAddresses[i]) && !ipv6Created {
			err = writeToFile(nodeIpIpV6File, chosenAddresses[i].String())
			if err != nil {
				return err
			}
			ipv6Created = true
		} else if !utils.IsIPv6(chosenAddresses[i]) && !ipv4Created {
			err = writeToFile(nodeIpIpV4File, chosenAddresses[i].String())
			if err != nil {
				return err
			}
			ipv4Created = true
		}
	}

	return nil
}

func writeToFile(path string, data string) error {
	dir := filepath.Dir(path)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}
	log.Infof("Opening path %s", path)
	fileToCreate, err := os.Create(path)
	if err != nil {
		return err
	}
	defer fileToCreate.Close()

	log.Infof("Writing path %s with content %s", path, data)
	_, err = fileToCreate.WriteString(data)
	if err != nil {
		return err
	}
	return nil
}

func checkAddressUsable(chosen []net.IP) (err error) {
	// If using IPv6, verify that the choosen address isn't tentative
	// i.e. we can actually bind to it
	if len(chosen) > 0 && net.IPv6len == len(chosen[0]) {
		_, err = net.Listen("tcp", "["+chosen[0].String()+"]:")
		if err != nil {
			log.Errorf("Chosen node IP is not usable")
			return err
		}
	}
	return err
}

func getSuitableIPs(retry bool, vips []net.IP, preferIPv6 bool) (chosen []net.IP, err error) {
	// Enable debug logging in utils package
	utils.SetDebugLogLevel()
	for {
		if len(vips) > 0 {
			chosen, err = utils.AddressesRouting(vips, utils.ValidNodeAddress)
			if len(chosen) > 0 || err != nil {
				if err == nil {
					err = checkAddressUsable(chosen)
				}
				if err != nil {
					if !retry {
						return nil, fmt.Errorf("Failed to find node IP")
					}
					time.Sleep(time.Second)
					continue
				}
				return chosen, err
			}
		}
		if len(chosen) == 0 {
			chosen, err = utils.AddressesDefault(preferIPv6, utils.ValidNodeAddress)
			if len(chosen) > 0 || err != nil {
				if err == nil {
					err = checkAddressUsable(chosen)
				}
				if err != nil {
					if !retry {
						return nil, fmt.Errorf("Failed to find node IP")
					}
					chosen = []net.IP{}
					time.Sleep(time.Second)
					continue
				}
				return chosen, err
			}
		}
		if !retry {
			return nil, fmt.Errorf("Failed to find node IP")
		}

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
