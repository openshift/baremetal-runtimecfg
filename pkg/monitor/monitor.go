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

var log = logrus.New()

type RuntimeConfig struct {
	LBConfig *config.ApiLBConfig
}

func Monitor(clusterName, clusterDomain, templatePath, cfgPath, apiVip string, apiPort, lbPort, statPort uint16, interval time.Duration) error {
	var oldConfig, newConfig *config.ApiLBConfig
	var k8sIsHealthy bool = false
	signals := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	signal.Notify(signals, syscall.SIGTERM)
	signal.Notify(signals, syscall.SIGINT)
	go func() {
		<-signals
		cleanHAProxyPreRoutingRule(apiVip, apiPort, lbPort)
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
			return nil
		default:
			config, err := config.GetLBConfig(domain, apiPort, lbPort, statPort)
			if err != nil {
				return err
			}
			newConfig = &config
			if oldConfig == nil || !cmp.Equal(*oldConfig, *newConfig) {
				log.WithFields(logrus.Fields{
					"newConfig": *newConfig,
				}).Info("Config change detected")
				err = render.RenderFile(cfgPath, templatePath, RuntimeConfig{LBConfig: newConfig})
				if err != nil {
					log.WithFields(logrus.Fields{
						"config": *newConfig,
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
			}
			oldConfig = newConfig

			ok, err := utils.IsKubernetesHealthy(lbPort)
			if err == nil && ok {
				if !k8sIsHealthy {
					log.Info("API is reachable through HAProxy")
					k8sIsHealthy = true
				}
				err := ensureHAProxyPreRoutingRule(apiVip, apiPort, lbPort)
				if err != nil {
					log.WithFields(logrus.Fields{"err": err}).Error("Failed to ensure HAProxy PREROUTING rule to direct traffic to the LB")
				}
			} else {
				cleanHAProxyPreRoutingRule(apiVip, apiPort, lbPort)
				if k8sIsHealthy {
					log.Info("API is not reachable through HAProxy")
					k8sIsHealthy = false
				}
			}
			time.Sleep(interval)
		}
	}
}
