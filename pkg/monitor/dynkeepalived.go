package monitor

import (
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/render"
	"github.com/sirupsen/logrus"
)

const keepalivedControlSock = "/var/run/keepalived/keepalived.sock"
const cfgKeepalivedChangeThreshold uint8 = 3

func KeepalivedWatch(kubeconfigPath, clusterConfigPath, templatePath, cfgPath string, apiVip, ingressVip, dnsVip net.IP, interval time.Duration) error {
	var appliedConfig, curConfig, prevConfig *config.Node
	var configChangeCtr uint8 = 0
	var bootstrapIP string

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

			if newConfig.EnableUnicast {
				if bootstrapIP == "" {
					bootstrapIP, err = config.GetBootstrapIP(apiVip.String())
					if err != nil {
						log.Warnf("Could not retrieve bootstrap IP: %v", err)
					}
				}
				if newConfig.BootstrapIP == "" {
					newConfig.BootstrapIP = bootstrapIP
				}

				newConfig.IngressConfig, err = config.GetIngressConfig(kubeconfigPath)
				if err != nil {
					log.Warnf("Could not retrieve ingress config: %v", err)
				}
			}

			curConfig = &newConfig
			if appliedConfig == nil || !cmp.Equal(*appliedConfig, *curConfig) {
				if prevConfig == nil || cmp.Equal(*prevConfig, *curConfig) {
					configChangeCtr++
				} else {
					configChangeCtr = 1
				}
				log.WithFields(logrus.Fields{
					"new config": newConfig,
				}).Info("Config change detected")
				if configChangeCtr >= cfgKeepalivedChangeThreshold {
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
			time.Sleep(interval)
		}
	}
}
