package monitor

import (
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/render"
	"github.com/sirupsen/logrus"
)

const keepalivedControlSock = "/var/run/keepalived/keepalived.sock"
const cfgKeepalivedChangeThreshold uint8 = 3
const dummyPortNum uint16 = 123
const unicastPatternInCfgFile = "unicast_peer"

var (
	g_BootstrapIP string
)

func getEnabledUnicastFromFile(cfgPath string) (error, bool) {
	enableUnicast := false
	_, err := os.Stat(cfgPath)
	if os.IsNotExist(err) {
		return err, enableUnicast
	}

	b, err := ioutil.ReadFile(cfgPath)
	if err != nil {
		return err, enableUnicast
	}
	s := string(b)
	// //check whether conf file contains unicast config pattern
	if strings.Contains(s, unicastPatternInCfgFile) {
		enableUnicast = true
	}
	return nil, enableUnicast
}

func updateUnicastConfig(kubeconfigPath string, newConfig, appliedConfig *config.Node) {
	var err error

	if !newConfig.EnableUnicast {
		return
	}
	retrieveBootstrapIpAddr(newConfig.Cluster.APIVIP)
	newConfig.BootstrapIP = g_BootstrapIP

	newConfig.IngressConfig, err = config.GetIngressConfig(kubeconfigPath)
	if err != nil {
		log.Warnf("Could not retrieve ingress config: %v", err)
	}

	newConfig.LBConfig, err = config.GetLBConfig(kubeconfigPath, dummyPortNum, dummyPortNum, dummyPortNum, net.ParseIP(newConfig.Cluster.APIVIP))
	if err != nil {
		log.Warnf("Could not retrieve LB config: %v", err)
	}
}

func doesConfigChanged(curConfig, appliedConfig *config.Node) bool {
	validConfig := true
	cfgChanged := appliedConfig == nil || !cmp.Equal(*appliedConfig, *curConfig)
	// In unicast mode etcd is used for sync purpose between bootstrap and the masters nodes,
	// we want to apply new config to master nodes only after nodes appears in etcd, with this
	// approach we should avoid asymetric configuration
	if curConfig.EnableUnicast {
		if os.Getenv("IS_BOOTSTRAP") == "no" && len(curConfig.LBConfig.Backends) == 0 {
			validConfig = false
		}
	}
	return cfgChanged && validConfig
}

func retrieveBootstrapIpAddr(apiVip string) {
	var err error

	if g_BootstrapIP != "" {
		return
	}
	// we don't need to read the bootstrap IP address for bootstrap node
	if os.Getenv("IS_BOOTSTRAP") == "yes" {
		g_BootstrapIP = ""
		return
	}
	g_BootstrapIP, err = config.GetBootstrapIP(apiVip)
	if err != nil {
		log.Warnf("Could not retrieve bootstrap IP: %v", err)
	}
}

func KeepalivedWatch(kubeconfigPath, clusterConfigPath, templatePath, cfgPath string, apiVip, ingressVip, dnsVip net.IP, apiPort, lbPort uint16, interval time.Duration) error {
	var appliedConfig, curConfig, prevConfig *config.Node
	var configChangeCtr uint8 = 0

	signals := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	signal.Notify(signals, syscall.SIGTERM)
	signal.Notify(signals, syscall.SIGINT)
	go func() {
		<-signals
		done <- true
	}()

	conn, err := net.Dial("unix", keepalivedControlSock)
	if err != nil {
		return err
	}
	defer conn.Close()
	for {
		select {
		case <-done:
			return nil
		default:
			newConfig, err := config.GetConfig(kubeconfigPath, clusterConfigPath, "/etc/resolv.conf", apiVip, ingressVip, dnsVip, 0, 0, 0)
			if err != nil {
				return err
			}
			//In upgrade flow, we should first continue with the same mode (unicast or multicast) as currently configured in keepalived.conf file
			err, enableUnicastFromFile := getEnabledUnicastFromFile(cfgPath)
			if err == nil && newConfig.EnableUnicast != enableUnicastFromFile {
				log.WithFields(logrus.Fields{
					"newConfig.EnableUnicast": newConfig.EnableUnicast,
					"enableUnicastFromFile":   enableUnicastFromFile,
				}).Info("EnableUnicast != enableUnicast from cfg file, update EnableUnicast value")
				newConfig.EnableUnicast = enableUnicastFromFile
			}
			updateUnicastConfig(kubeconfigPath, &newConfig, appliedConfig)
			curConfig = &newConfig
			if doesConfigChanged(curConfig, appliedConfig) {
				if prevConfig == nil || cmp.Equal(*prevConfig, *curConfig) {
					configChangeCtr++
				} else {
					configChangeCtr = 1
				}
				log.WithFields(logrus.Fields{
					"current config":  *curConfig,
					"configChangeCtr": configChangeCtr,
				}).Info("Config change detected")

				if configChangeCtr >= cfgKeepalivedChangeThreshold {

					log.WithFields(logrus.Fields{
						"curConfig": *curConfig,
					}).Info("Apply config change")

					err = render.RenderFile(cfgPath, templatePath, newConfig)
					if err != nil {
						log.WithFields(logrus.Fields{
							"config": newConfig,
						}).Error("Failed to render Keepalived configuration")
						return err
					}

					_, err = conn.Write([]byte("reload\n"))
					if err != nil {
						log.WithFields(logrus.Fields{
							"socket": keepalivedControlSock,
						}).Error("Failed to write reload to Keepalived container control socket")
						return err
					}
					configChangeCtr = 0
					appliedConfig = curConfig
				}
			} else {
				configChangeCtr = 0
			}
			prevConfig = &newConfig

			// Signal to keepalived whether the haproxy firewall rule is in place
			ruleExists, err := checkHAProxyPreRoutingRule(apiVip.String(), apiPort, lbPort)
			if err != nil {
				log.Error("Failed to check for haproxy firewall rule")
			} else {
				filePath := "/var/run/keepalived/iptables-rule-exists"
				_, err := os.Stat(filePath)
				fileExists := !os.IsNotExist(err)
				if ruleExists {
					if !fileExists {
						_, err := os.Create(filePath)
						if err != nil {
							log.WithFields(logrus.Fields{"path": filePath}).Error("Failed to create file")
						}
					}
				} else {
					if fileExists {
						err := os.Remove(filePath)
						if err != nil {
							log.WithFields(logrus.Fields{"path": filePath}).Error("Failed to remove file")
						}
					}
				}
			}
			time.Sleep(interval)
		}
	}
}
