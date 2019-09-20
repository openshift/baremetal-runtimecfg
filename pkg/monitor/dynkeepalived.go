package monitor

import (
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/render"
	"github.com/sirupsen/logrus"
)

const keepalivedControlSock = "/var/run/keepalived/keepalived.sock"

func KeepalivedWatch(kubeconfigPath, clusterConfigPath, templatePath, cfgPath string, apiVip, ingressVip, dnsVip net.IP, interval time.Duration) error {
	var prevConfig *config.Node

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
			newConfig, err := config.GetConfig(kubeconfigPath, clusterConfigPath, apiVip, ingressVip, dnsVip, 0, 0, 0)
			if err != nil {
				return err
			}
			if prevConfig == nil || prevConfig.VRRPInterface != newConfig.VRRPInterface {
				log.WithFields(logrus.Fields{
					"new config": newConfig,
				}).Info("Config change detected")
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
			}
			prevConfig = &newConfig
			time.Sleep(interval)
		}
	}
}
