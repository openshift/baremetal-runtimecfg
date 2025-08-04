package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	// Enable sha256 in container image references
	_ "crypto/sha256"

	"github.com/spf13/cobra"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
)

const (
	kubeletSvcOverridePath      = "/etc/systemd/system/kubelet.service.d/20-nodenet.conf"
	nodeIpFile                  = "/run/nodeip-configuration/primary-ip"
	nodeIpIpV6File              = config.NodeIpIpV6File
	nodeIpIpV4File              = config.NodeIpIpV4File
	nodeIpNotMatchesVipsFile    = "/run/nodeip-configuration/remote-worker"
	crioSvcOverridePath         = "/etc/systemd/system/crio.service.d/20-nodenet.conf"
	remoteWorkerLabel           = "node.openshift.io/remote-worker"
	ovn                         = "OVNKubernetes"
	maxSecondsToSuitableIPsLoop = 300 // 5 minutes
	addSecondsToSuitableIPsLoop = 2
)

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

var params struct {
	retry         bool
	preferIPv6    bool
	userManagedLB bool
	networkType   string
	platform      string
}

// init executes upon import
func init() {
	nodeIPCmd.AddCommand(nodeIPShowCmd)
	nodeIPCmd.AddCommand(nodeIPSetCmd)
	nodeIPCmd.PersistentFlags().BoolVarP(&params.retry, "retry-on-failure", "r", false, "Keep retrying until it finds a suitable IP address. System errors will still abort")
	nodeIPCmd.PersistentFlags().BoolVarP(&params.preferIPv6, "prefer-ipv6", "6", false, "Prefer IPv6 addresses to IPv4")
	nodeIPCmd.PersistentFlags().StringVarP(&params.networkType, "network-type", "n", ovn, "CNI network type")
	nodeIPCmd.PersistentFlags().BoolVarP(&params.userManagedLB, "user-managed-lb", "l", false, "User managed load balancer")
	nodeIPCmd.PersistentFlags().StringVarP(&params.platform, "platform", "p", "", "Cluster platform")
	rootCmd.AddCommand(nodeIPCmd)
}

func show(cmd *cobra.Command, args []string) error {
	vips, err := parseIPs(args)
	if err != nil {
		return err
	}

	chosenAddresses, _, err := getSuitableIPs(params.retry, vips, params.preferIPv6, params.networkType)
	if err != nil {
		return err
	}
	log.Infof("Chosen Node IPs: %v", chosenAddresses)

	fmt.Println(chosenAddresses[0])
	return nil
}

func set(cmd *cobra.Command, args []string) error {
	log.Infof("NodeIp started with params: %+v", params)

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

	chosenAddresses, matchesVips, err := getSuitableIPs(params.retry, vips, params.preferIPv6, params.networkType)
	if err != nil {
		return err
	}
	log.Infof("Chosen Node IPs: %v", chosenAddresses)

	nodeIP := chosenAddresses[0].String()
	nodeIPs := nodeIP
	if len(chosenAddresses) > 1 {
		nodeIPs += "," + chosenAddresses[1].String()
	}
	remoteWorker := isRemoteWorker(vips, matchesVips, params.userManagedLB, params.platform)
	// if chosen ip doesn't match vips, we need create a file that
	// will be used by keepalived container to verify if it should run or not
	// We want to disable keepalived in case this host is remote worker
	// This is skipped in case of user managed load balancer as the VIPs are not managed by keepalived.
	if remoteWorker {
		err = writeToFile(nodeIpNotMatchesVipsFile, "node ip doesn't match any vip, don't run keepalived")
		if err != nil {
			return err
		}
	}

	// Kubelet
	kOverrideContent := fmt.Sprintf("[Service]\nEnvironment=\"KUBELET_NODE_IP=%s\" \"KUBELET_NODE_IPS=%s\"\n", nodeIP, nodeIPs)
	// in case chosen ip doesn't match vip (remote worker) set kubelet label
	if remoteWorker {
		kOverrideContent += fmt.Sprintf("Environment=\"CUSTOM_KUBELET_LABELS=%s\"\n", remoteWorkerLabel)
	}

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
	log.Debugf("Opening path %s", path)
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

func getSuitableIPs(retry bool, vips []net.IP, preferIPv6 bool, networkType string) (chosen []net.IP, matchesVips bool, err error) {
	// timerLoop will hold a time in Seconds to be used with time.Sleep() before going
	// for the next loop interation.
	timerLoop := 1

	// Enable debug logging in utils package if requested
	if os.Getenv("ENABLE_NODEIP_DEBUG") == "true" {
		utils.SetDebugLogLevel()
	}

	ipFilterFunc := utils.ValidNodeAddress
	for {
		timerLoop = timerLoop * addSecondsToSuitableIPsLoop
		if len(vips) > 0 {
			chosen, err = utils.AddressesRouting(vips, ipFilterFunc, preferIPv6)
			if len(chosen) > 0 || err != nil {
				if err == nil {
					err = checkAddressUsable(chosen)
				}
				if err != nil {
					if !retry {
						return nil, false, fmt.Errorf("Failed to find node IP")
					}
					time.Sleep(time.Second)
					continue
				}
				return chosen, true, err
			}
		}
		if len(chosen) == 0 {
			// we should check ovn specific in case vips were not set
			if networkType == ovn {
				ipFilterFunc = utils.ValidOVNNodeAddress
			}
			chosen, err = utils.AddressesDefault(preferIPv6, ipFilterFunc)
			if len(chosen) > 0 || err != nil {
				if err == nil {
					err = checkAddressUsable(chosen)
				}
				if err != nil {
					if !retry {
						return nil, false, fmt.Errorf("Failed to find node IP")
					}
					chosen = []net.IP{}
					time.Sleep(time.Second)
					continue
				}
				return chosen, false, err
			}
		}
		if !retry {
			return nil, false, fmt.Errorf("Failed to find node IP")
		}

		log.Errorf("Failed to find a suitable node IP")
		if timerLoop >= maxSecondsToSuitableIPsLoop {
			// we reached the max seconds to suitable IPs, to avoid spam logs
			// keep sleeping maxSecondsToSuitableIPsLoop before the next try.
			timerLoop = maxSecondsToSuitableIPsLoop
		}
		time.Sleep(time.Second * time.Duration(timerLoop))
	}
}

func parseIPs(args []string) ([]net.IP, error) {
	ips := make([]net.IP, len(args))
	for i, arg := range args {
		ips[i] = net.ParseIP(arg)
		if ips[i] == nil {
			return nil, fmt.Errorf("Failed to parse IP address %s", arg)
		}
		log.Debugf("Parsed Virtual IP %s", ips[i])
	}
	return ips, nil
}

// currently we allow setting remote worker only in case of baremetal platform without external lb
func isRemoteWorker(vips []net.IP, matchesVips, userManagedLB bool, platform string) bool {
	return len(vips) > 0 && !matchesVips && !userManagedLB && strings.ToLower(platform) == "baremetal"
}
