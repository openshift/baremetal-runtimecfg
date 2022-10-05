package monitor

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"crypto/tls"

	"gopkg.in/yaml.v2"

	"github.com/google/go-cmp/cmp"
	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/render"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	keepalivedControlSock                       = "/var/run/keepalived/keepalived.sock"
	cfgKeepalivedChangeThreshold  uint8         = 3
	dummyPortNum                  uint16        = 123
	unicastPatternInCfgFile                     = "unicast_peer"
	modeUpdateFilepath                          = "/etc/keepalived/monitor.conf"
	userModeUpdateFilepath                      = "/etc/keepalived/monitor-user.conf"
	modeUpdateIntervalInSec       time.Duration = 600
	processingTimeInSec           uint16        = 30
	iptablesFilePath                            = "/var/run/keepalived/iptables-rule-exists"
	haproxyFilePath                             = "/var/run/keepalived/haproxy-healthy"
	haproxyCheckInterval                        = time.Second * 2
	bigRise = 30
	smallRise = 3
	bootstrapApiFailuresThreshold int           = 4
)

type APIState uint8

const (
	stopped APIState = iota
	started APIState = iota
)

type haproxyCheckStatus struct {
	fall int
	rise int
	current bool
	failCount int
	passCount int
}

func getActualMode(cfgPath string) (error, bool) {
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

func updateUnicastConfig(kubeconfigPath string, newConfig *config.Node) {
	var err error

	if !newConfig.EnableUnicast {
		return
	}
	newConfig.IngressConfig, err = config.GetIngressConfig(kubeconfigPath, newConfig.Cluster.APIVIP)
	if err != nil {
		log.Warnf("Could not retrieve ingress config: %v", err)
	}

	newConfig.LBConfig, err = config.GetLBConfig(kubeconfigPath, dummyPortNum, dummyPortNum, dummyPortNum, net.ParseIP(newConfig.Cluster.APIVIP))
	if err != nil {
		log.Warnf("Could not retrieve LB config: %v", err)
	}

	for i, c := range *newConfig.Configs {
		// Must do this by index instead of using c because c is local to this loop
		(*newConfig.Configs)[i].IngressConfig, err = config.GetIngressConfig(kubeconfigPath, c.Cluster.APIVIP)
		if err != nil {
			log.Warnf("Could not retrieve ingress config: %v", err)
		}
		(*newConfig.Configs)[i].LBConfig, err = config.GetLBConfig(kubeconfigPath, dummyPortNum, dummyPortNum, dummyPortNum, net.ParseIP(c.Cluster.APIVIP))
		if err != nil {
			log.Warnf("Could not retrieve LB config: %v", err)
		}
	}
}

func doesConfigChanged(curConfig, appliedConfig *config.Node) bool {
	validConfig := true
	cfgChanged := appliedConfig == nil || !cmp.Equal(*appliedConfig, *curConfig)
	// In unicast mode etcd is used for sync purpose between bootstrap and the masters nodes,
	// we want to apply new config to master nodes only after nodes appears in etcd, with this
	// approach we should avoid asymetric configuration
	if curConfig.EnableUnicast {
		if os.Getenv("IS_BOOTSTRAP") == "no" && len(curConfig.LBConfig.Backends) < 2 {
			validConfig = false
		}
	}
	return cfgChanged && validConfig
}

type modeUpdateInfo struct {
	Mode string
	Time time.Time
}

func isModeUpdateNeeded(cfgPath string) (bool, modeUpdateInfo) {
	enableUnicast := false
	updateRequired := false
	desiredModeInfo := modeUpdateInfo{}
	filePath := userModeUpdateFilepath

	// userModeUpdateFilepath has higher priority than modeUpdateFilepath
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		filePath = modeUpdateFilepath
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			return updateRequired, desiredModeInfo
		}
	}

	yamlFile, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Warnf("Could not ReadFile %s", filePath)
		return updateRequired, desiredModeInfo
	}
	if err = yaml.Unmarshal(yamlFile, &desiredModeInfo); err != nil {
		log.Warnf("Could not parse file content %s", yamlFile)
		return updateRequired, desiredModeInfo
	}
	if desiredModeInfo.Mode == "unicast" {
		enableUnicast = true
	}
	err, curEnableUnicast := getActualMode(cfgPath)
	if err == nil && curEnableUnicast != enableUnicast {
		updateRequired = true
	}
	return updateRequired, desiredModeInfo
}

func handleBootstrapStopKeepalived(kubeconfigPath string, bootstrapStopKeepalived chan APIState) {
	consecutiveErr := 0

	/* It could take up to ~20 seconds for the local kube-apiserver to start running on the bootstrap node,
	so before checking if kube-apiserver is not operational we should verify (with a timeout of 30 seconds)
	first that it's operational. */
	log.Info("handleBootstrapStopKeepalived: verify first that local kube-apiserver is operational")
	for start := time.Now(); time.Since(start) < time.Second*30; {
		if _, err := config.GetIngressConfig(kubeconfigPath, ""); err == nil {
			log.Info("handleBootstrapStopKeepalived: local kube-apiserver is operational")
			break
		}
		log.Info("handleBootstrapStopKeepalived: local kube-apiserver still not operational")
		time.Sleep(3 * time.Second)
	}

	for {
		if _, err := config.GetIngressConfig(kubeconfigPath, ""); err != nil {
			// We have started to talk to Ironic through the API VIP as well,
			// so if Ironic is still up then we need to keep the VIP, even if
			// the apiserver has gone down.
			if _, err = http.Get("http://localhost:6385/v1"); err != nil {
				consecutiveErr++
				log.WithFields(logrus.Fields{
					"consecutiveErr": consecutiveErr,
				}).Info("handleBootstrapStopKeepalived: detect failure on API and Ironic")
			}
		} else {
			if consecutiveErr > bootstrapApiFailuresThreshold { // Means it was stopped
				bootstrapStopKeepalived <- started
			}
			consecutiveErr = 0
		}
		if consecutiveErr > bootstrapApiFailuresThreshold {
			log.WithFields(logrus.Fields{
				"consecutiveErr":                consecutiveErr,
				"bootstrapApiFailuresThreshold": bootstrapApiFailuresThreshold,
			}).Info("handleBootstrapStopKeepalived: Num of failures exceeds threshold")
			bootstrapStopKeepalived <- stopped
		}
		time.Sleep(1 * time.Second)
	}
}

func handleConfigModeUpdate(cfgPath string, kubeconfigPath string, updateModeCh chan modeUpdateInfo) {

	// create Ticker that will run every round modeUpdateIntervalInSec
	nextTickTime := time.Now().Add((modeUpdateIntervalInSec / 2) * time.Second).Round(modeUpdateIntervalInSec * time.Second)
	time.Sleep(time.Until(nextTickTime))
	ticker := time.NewTicker(modeUpdateIntervalInSec * time.Second)
	defer ticker.Stop()

	for {

		select {
		case tickerTime := <-ticker.C:

			updateRequired, desiredModeInfo := isModeUpdateNeeded(cfgPath)
			if !updateRequired {
				continue
			}
			log.WithFields(logrus.Fields{
				"desiredModeInfo.Mode": desiredModeInfo.Mode,
				"tickerTime":           tickerTime,
			}).Info("Update Mode request detected, verify that upgrade process completed")

			// before applying mode update we should verify that upgrade process completed.
			upgradeRunning, err := config.IsUpgradeStillRunning(kubeconfigPath)
			if err != nil || upgradeRunning {
				log.WithFields(logrus.Fields{
					"err":            err,
					"upgradeRunning": upgradeRunning,
				}).Info("Failed to retrieve upgrade status or Upgrade still running")
				continue
			}
			// Ticker being called every round 10Min (e.g: 14:50, 15:00), the calculated time for mode update is: next round 5 minutes.
			// so, for 14:50, we'd do it at 14:55 and for 15:00 we'd do it at 15:05
			desiredModeInfo.Time = time.Now().Add((modeUpdateIntervalInSec / 2) * time.Second).Round((modeUpdateIntervalInSec / 2) * time.Second)
			log.WithFields(logrus.Fields{
				"desiredModeInfo.Time": desiredModeInfo.Time,
			}).Info("Planned time for Mode update")

			timeoutInSec := time.Duration((time.Until(desiredModeInfo.Time).Seconds() - (float64)(processingTimeInSec)))
			// sleep until processingTimeInSec seconds before planned time
			time.Sleep(timeoutInSec * time.Second)
			updateModeCh <- desiredModeInfo
		}
	}
}

func handleLeasing(cfgPath string, apiVips, ingressVips []net.IP) error {
	vips, err := getVipsToLease(cfgPath)

	if err != nil {
		return err
	}

	if vips == nil {
		return nil
	}

	if len(apiVips) != len(vips.APIVips) {
		return fmt.Errorf("Mismatched number of API VIPs. Expected: %d Actual: %d", len(apiVips), len(vips.APIVips))
	}
	if len(ingressVips) != len(vips.IngressVips) {
		return fmt.Errorf("Mismatched number of Ingress VIPs. Expected: %d Actual: %d", len(ingressVips), len(vips.IngressVips))
	}

	for i, vip := range vips.APIVips {
		if vip.IpAddress != apiVips[i].String() {
			return fmt.Errorf("Mismatched ip for api. Expected: %s Actual: %s", apiVips[i].String(), vips.APIVip.IpAddress)
		}
	}

	for i, vip := range vips.IngressVips {
		if vip.IpAddress != ingressVips[i].String() {
			return fmt.Errorf("Mismatched ip for ingress. Expected: %s Actual: %s", ingressVips[i].String(), vips.IngressVip.IpAddress)
		}
	}

	for i := 0; i < len(apiVips); i++ {
		vipIface, _, err := config.GetVRRPConfig(apiVips[i], ingressVips[i])
		if err != nil {
			return err
		}

		if err = LeaseVIPs(log, cfgPath, vipIface.Name, []vip{vips.APIVips[i], vips.IngressVips[i]}); err != nil {
			log.WithFields(logrus.Fields{
				"cfgPath":        cfgPath,
				"vipMasterIface": vipIface.Name,
				"vips":           []vip{vips.APIVips[i], vips.IngressVips[i]},
			}).WithError(err).Error("Failed to lease VIPS")
			return err
		}
	}

	log.WithFields(logrus.Fields{
		"cfgPath": cfgPath,
	}).Info("Leased VIPS successfully")

	return nil
}

func haproxyHealthCheck(lbPort uint16) {
	haproxyStatus := haproxyCheckStatus{fall: 3, rise: smallRise, current: true, failCount: 0, passCount: 3}
	for {
		// Signal to keepalived whether haproxy on this node seems healthy
		// This is being done here instead of in a check script so we can
		// have more sophisticated logic than a check script allows
		haproxyHealthy := checkHAProxyHealth(lbPort, &haproxyStatus)
		// TODO: Factor out the file creation logic that is shared with the
		// firewall check
		_, err := os.Stat(haproxyFilePath)
		fileExists := !os.IsNotExist(err)
		if haproxyHealthy {
			if !fileExists {
				_, err := os.Create(haproxyFilePath)
				if err != nil {
					log.WithFields(logrus.Fields{"path": haproxyFilePath}).Error("Failed to create file")
				}
			}
		} else {
			if fileExists {
				err := os.Remove(haproxyFilePath)
				if err != nil {
					log.WithFields(logrus.Fields{"path": haproxyFilePath}).Error("Failed to remove file")
				}
			}
		}
		time.Sleep(haproxyCheckInterval)
	}
}

func checkHAProxyHealth(port uint16, status *haproxyCheckStatus) bool {
	current := false
	code := -1
	message := ""

	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
    client := &http.Client{Transport: tr}
	response, err := client.Get(fmt.Sprintf("https://localhost:%v/readyz", port))
	if err == nil {
		defer response.Body.Close()
		defer client.CloseIdleConnections()
		code = response.StatusCode
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			current = true
		}
	} else {
		message = err.Error()
	}
	// This can probably be rolled into the if above
	if current {
		status.passCount++
		status.failCount = 0
	} else {
		status.failCount++
		status.passCount = 0
	}
	// Adjust fall value based on whether we have the VIP
	if haveAPIVIP() {
		// We didn't have it previously, log a message
		if status.rise == bigRise {
			log.Info("Node now holding API VIP")
		}
		status.rise = smallRise
	} else {
		if status.rise == smallRise {
			log.Info("Node lost API VIP")
		}
		status.rise = bigRise
	}
	if !current {
		if status.failCount >= status.fall {
			status.current = false
		}
		log.WithFields(logrus.Fields{
			"code": code,
			"failCount": status.failCount,
			"fall": status.fall,
			"rise": status.rise,
			"current": status.current,
			"message": message,
		}).Info("HAProxy Healthcheck Failed")
	} else  {
		if status.passCount >= status.rise {
			status.current = true
		}
		// Only log passes while status is rising so we don't spam the logs under
		// ordinary circumstances
		if (status.passCount < status.rise && !status.current) || status.passCount == status.rise {
			log.WithFields(logrus.Fields{
				"code": code,
				"passCount": status.passCount,
				"fall": status.fall,
				"rise": status.rise,
				"current": status.current,
			}).Info("HAProxy Healthcheck Passed")
		}
	}
	return status.current
}

// TODO: Pass in API VIP and interface
func haveAPIVIP() bool {
	nlHandle, err := netlink.NewHandle(unix.NETLINK_ROUTE)
	if err != nil {
		log.WithFields(logrus.Fields{
				"err": err,
			}).Error("haveAPIVIP: Failed to get handle")
		return false
	}
	defer nlHandle.Delete()

	link, err := nlHandle.LinkByName("enp2s0")
	if err != nil {
		log.WithFields(logrus.Fields{
				"err": err,
			}).Error("haveAPIVIP: Failed to get link")
		return false
	}
	addresses, err := nlHandle.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		log.WithFields(logrus.Fields{
			"err": err,
		}).Error("haveAPIVIP: Failed to list addresses")
		return false
	}
	nlVIP, err := netlink.ParseAddr("192.168.111.5/32")
	if err != nil {
		log.WithFields(logrus.Fields{
			"err": err,
		}).Error("haveAPIVIP: Failed to parse VIP")
		return false
	}
	for _, address := range addresses {
		if address.Equal(*nlVIP) {
			return true
		}
	}
	return false
}

func KeepalivedWatch(kubeconfigPath, clusterConfigPath, templatePath, cfgPath string, apiVips, ingressVips []net.IP, apiPort, lbPort uint16, interval time.Duration) error {
	var appliedConfig, curConfig, prevConfig *config.Node
	var configChangeCtr uint8 = 0

	if err := handleLeasing(cfgPath, apiVips, ingressVips); err != nil {
		return err
	}

	signals := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	updateModeCh := make(chan modeUpdateInfo, 1)
	bootstrapStopKeepalived := make(chan APIState, 1)

	signal.Notify(signals, syscall.SIGTERM)
	signal.Notify(signals, syscall.SIGINT)
	go func() {
		<-signals
		done <- true
	}()

	go handleConfigModeUpdate(cfgPath, kubeconfigPath, updateModeCh)
	go haproxyHealthCheck(lbPort)

	if os.Getenv("IS_BOOTSTRAP") == "yes" {
		/* When OPENSHIFT_INSTALL_PRESERVE_BOOTSTRAP is set to true the bootstrap node won't be destroyed and
		   Keepalived on the bootstrap continue to run, this behavior might cause problems when unicast keepalived being used,
		   so, Keepalived on bootstrap should stop running when local kube-apiserver isn't operational anymore.
		   handleBootstrapStopKeepalived function is responsible to stop Keepalived when the condition is met. */
		go handleBootstrapStopKeepalived(kubeconfigPath, bootstrapStopKeepalived)
	}

	conn, err := net.Dial("unix", keepalivedControlSock)
	if err != nil {
		return err
	}
	defer conn.Close()
	for {
		select {
		case <-done:
			return nil

		case APIStateChanged := <-bootstrapStopKeepalived:
			//Verify that stop message sent successfully
			for {
				var cmdMsg []byte
				if APIStateChanged == stopped {
					cmdMsg = []byte("stop\n")
				} else {
					cmdMsg = []byte("reload\n")
				}
				_, err := conn.Write(cmdMsg)
				if err == nil {
					log.Infof("Command message successfully sent to Keepalived container control socket: %s", string(cmdMsg[:]))
					break
				}
				log.WithFields(logrus.Fields{
					"socket": keepalivedControlSock,
				}).Error("Failed to write command to Keepalived container control socket")
				time.Sleep(1 * time.Second)
			}
			// Make sure we don't send multiple messages in close succession if the
			// bootstrapStopKeepalived queue has more than one item in it.
			time.Sleep(5 * time.Second)

		case desiredModeInfo := <-updateModeCh:

			newConfig, err := config.GetConfig(kubeconfigPath, clusterConfigPath, "/etc/resolv.conf", apiVips, ingressVips, 0, 0, 0)
			if err != nil {
				return err
			}
			log.WithFields(logrus.Fields{
				"newConfig.EnableUnicast": newConfig.EnableUnicast,
				"desiredModeInfo.Mode":    desiredModeInfo.Mode,
				"desiredModeInfo.Time":    desiredModeInfo.Time,
			}).Info("Update Mode from newConfig.EnableUnicast to desiredModeInfo.Mode")

			if desiredModeInfo.Mode == "unicast" {
				newConfig.EnableUnicast = true
			} else {
				newConfig.EnableUnicast = false
			}
			updateUnicastConfig(kubeconfigPath, &newConfig)

			log.WithFields(logrus.Fields{
				"curConfig": newConfig,
			}).Info("Mode Update config change")

			err = render.RenderFile(cfgPath, templatePath, newConfig)
			if err != nil {
				log.WithFields(logrus.Fields{
					"config": newConfig,
				}).Error("Failed to render Keepalived configuration")
				return err
			}

			time.Sleep(time.Until(desiredModeInfo.Time))
			log.WithFields(logrus.Fields{
				"curTime": time.Now(),
			}).Info("After sleep, before sending reload request ")

			_, err = conn.Write([]byte("reload\n"))
			if err != nil {
				log.WithFields(logrus.Fields{
					"socket": keepalivedControlSock,
				}).Error("Failed to write reload to Keepalived container control socket")
				return err
			}

			curConfig = &newConfig
			configChangeCtr = 0
			appliedConfig = curConfig

		default:
			newConfig, err := config.GetConfig(kubeconfigPath, clusterConfigPath, "/etc/resolv.conf", apiVips, ingressVips, 0, 0, 0)
			if err != nil {
				return err
			}

			//In upgrade flow, we should first continue with the same mode (unicast or multicast) as currently configured in keepalived.conf file
			err, curEnableUnicast := getActualMode(cfgPath)
			if err == nil && newConfig.EnableUnicast != curEnableUnicast {
				log.WithFields(logrus.Fields{
					"newConfig.EnableUnicast": newConfig.EnableUnicast,
					"curEnableUnicast":        curEnableUnicast,
				}).Debug("EnableUnicast != enableUnicast from cfg file, update EnableUnicast value")
				newConfig.EnableUnicast = curEnableUnicast
			}
			updateUnicastConfig(kubeconfigPath, &newConfig)
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
			// The rules are all managed as a single entity, so we should only need
			// to check the first VIP.
			ruleExists, err := checkHAProxyFirewallRules(apiVips[0].String(), apiPort, lbPort)
			if err != nil {
				log.Error("Failed to check for haproxy firewall rule")
			} else {
				_, err := os.Stat(iptablesFilePath)
				fileExists := !os.IsNotExist(err)
				if ruleExists {
					if !fileExists {
						_, err := os.Create(iptablesFilePath)
						if err != nil {
							log.WithFields(logrus.Fields{"path": iptablesFilePath}).Error("Failed to create file")
						}
					}
				} else {
					if fileExists {
						err := os.Remove(iptablesFilePath)
						if err != nil {
							log.WithFields(logrus.Fields{"path": iptablesFilePath}).Error("Failed to remove file")
						}
					}
				}
			}
			time.Sleep(interval)
		}
	}
}
