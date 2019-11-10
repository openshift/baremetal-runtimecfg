package monitor

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/sirupsen/logrus"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/render"
	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
)

const haproxyMasterSock = "/var/run/haproxy/haproxy-master.sock"
const cfgChangeThreshold uint8 = 3
const k8sHealthThresholdOn uint8 = 3
const k8sHealthThresholdOff uint8 = 2

var log = logrus.New()

type RuntimeConfig struct {
	LBConfig *config.ApiLBConfig
}

func Monitor(clusterName, clusterDomain, templatePath, cfgPath, apiVip string, apiPort, lbPort, statPort uint16, interval time.Duration) error {
	var appliedConfig, curConfig, prevConfig *config.ApiLBConfig
	var K8sHealthSts bool = false
	var oldK8sHealthSts bool
	var k8sHealthChangeCtr uint8 = 0
	var configChangeCtr uint8 = 0

	signals := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	signal.Notify(signals, syscall.SIGTERM)
	signal.Notify(signals, syscall.SIGINT)
	go func() {
		<-signals
		done <- true
	}()

	conn, err := net.Dial("unix", haproxyMasterSock)
	if err != nil {
		return err
	}
	defer conn.Close()

	domain := fmt.Sprintf("%s.%s", clusterName, clusterDomain)
	log.Info("API is not reachable through HAProxy")
	for {
		select {
		case <-done:
			cleanHAProxyPreRoutingRule(apiVip, apiPort, lbPort)
			return nil
		default:
			config, err := config.GetLBConfig(domain, apiPort, lbPort, statPort, net.ParseIP(apiVip))
			if err != nil {
				return err
			}
			curConfig = &config
			if appliedConfig == nil || !cmp.Equal(*appliedConfig, *curConfig) {
				if prevConfig == nil || cmp.Equal(*prevConfig, *curConfig) {
					configChangeCtr++
				} else {
					configChangeCtr = 1
				}
				log.WithFields(logrus.Fields{
					"curConfig":       *curConfig,
					"configChangeCtr": configChangeCtr,
				}).Info("Config change detected")
				if configChangeCtr >= cfgChangeThreshold {
					log.WithFields(logrus.Fields{
						"curConfig": *curConfig,
					}).Info("Apply config change")
					err = render.RenderFile(cfgPath, templatePath, RuntimeConfig{LBConfig: curConfig})
					if err != nil {
						log.WithFields(logrus.Fields{
							"config": *curConfig,
						}).Error("Failed to render HAProxy configuration")
						return err
					}
					_, err = conn.Write([]byte("reload\n"))
					if err != nil {
						log.WithFields(logrus.Fields{
							"socket": haproxyMasterSock,
						}).Error("Failed to write reload to HAProxy master socket")
						return err
					}
					configChangeCtr = 0
					appliedConfig = curConfig
				}
			} else {
				configChangeCtr = 0
			}
			prevConfig = &config

			curK8sHealthSts, err := utils.IsKubernetesHealthy(lbPort)
			if err != nil {
				curK8sHealthSts = false
			}
			oldK8sHealthSts = K8sHealthSts
			K8sHealthSts, k8sHealthChangeCtr = utils.AlarmStabilization(K8sHealthSts, curK8sHealthSts, k8sHealthChangeCtr, k8sHealthThresholdOn, k8sHealthThresholdOff)
			if K8sHealthSts {
				if oldK8sHealthSts != K8sHealthSts {
					log.Info("API is reachable through HAProxy")
				}
				err := ensureHAProxyPreRoutingRule(apiVip, apiPort, lbPort)
				if err != nil {
					log.WithFields(logrus.Fields{"err": err}).Error("Failed to ensure HAProxy PREROUTING rule to direct traffic to the LB")
				}
			} else {
				if oldK8sHealthSts != K8sHealthSts {
					log.Info("API is not reachable through HAProxy")
				}
				cleanHAProxyPreRoutingRule(apiVip, apiPort, lbPort)
			}
			time.Sleep(interval)
		}
	}
}
