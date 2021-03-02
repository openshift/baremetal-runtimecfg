package monitor

import (
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/render"
	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
	"github.com/sirupsen/logrus"
)

const resolvConfFilepath string = "/var/run/NetworkManager/resolv.conf"

func CorednsWatch(kubeconfigPath, clusterConfigPath, templatePath, cfgPath string, apiVip, ingressVip net.IP, interval time.Duration) error {
	var prevMD5 string

	signals := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	signal.Notify(signals, syscall.SIGTERM)
	signal.Notify(signals, syscall.SIGINT)
	go func() {
		<-signals
		done <- true
	}()

	prevMD5, err := utils.GetFileMd5(resolvConfFilepath)
	if err != nil {
		return err
	}

	for {
		select {
		case <-done:
			return nil
		default:
			curMD5, err := utils.GetFileMd5(resolvConfFilepath)
			if err != nil {
				return err
			}
			if curMD5 != prevMD5 {
				newConfig, err := config.GetConfig(kubeconfigPath, clusterConfigPath, resolvConfFilepath, apiVip, ingressVip, 0, 0, 0)
				if err != nil {
					return err
				}
				log.WithFields(logrus.Fields{
					"DNS upstreams": newConfig.DNSUpstreams,
				}).Info("Resolv.conf change detected, rendering Corefile")

				err = render.RenderFile(cfgPath, templatePath, newConfig)
				if err != nil {
					log.WithFields(logrus.Fields{
						"config": newConfig,
					}).Error("Failed to render coredns Corefile")
					return err
				}
				prevMD5 = curMD5
			}
			time.Sleep(interval)
		}
	}
}
