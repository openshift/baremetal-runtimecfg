package monitor

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/sirupsen/logrus"

	"github.com/openshift/baremetal-runtimecfg/pkg/render"
	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
)

const haproxyMasterSock = "/var/run/haproxy/haproxy-master.sock"

var log = logrus.New()

type Backend struct {
	Host    string
	Address string
	Port    uint16
}
type ApiLBConfig struct {
	ApiPort  uint16
	LbPort   uint16
	StatPort uint16
	Backends []Backend
}

func getSortedBackends(domain string) (backends []Backend, err error) {
	srvs, err := utils.GetEtcdSRVMembers(domain)
	if err != nil {
		return backends, err
	}

	backends = make([]Backend, len(srvs))
	for i, srv := range srvs {
		addr, err := utils.GetFirstAddr(srv.Target)
		if err != nil {
			log.WithFields(logrus.Fields{
				"member": srv.Target,
			}).Error("Failed to get address for member")
			continue
		}
		backends[i].Host = srv.Target
		backends[i].Address = addr
		backends[i].Port = srv.Port
	}
	sort.Slice(backends, func(i, j int) bool {
		return backends[i].Address < backends[j].Address
	})
	return backends, err
}

func Monitor(clusterName, clusterDomain, templatePath, cfgPath, apiVip string, apiPort, lbPort, statPort uint16, interval time.Duration) error {
	var oldConfig, newConfig *ApiLBConfig
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
	for {
		select {
		case <-done:
			return nil
		default:
			newConfig = &ApiLBConfig{
				ApiPort:  apiPort,
				LbPort:   lbPort,
				StatPort: statPort,
			}

			backends, err := getSortedBackends(domain)

			// The backends port is the Etcd one, but we need the API one
			for i := 0; i < len(backends); i++ {
				backends[i].Port = apiPort
			}
			if err != nil {
				log.WithFields(logrus.Fields{
					"domain": domain,
				}).Error("Failed to get API member information")
				return err
			}
			newConfig.Backends = backends
			log.WithFields(logrus.Fields{
				"config": *newConfig,
			}).Debug("Config for LB configuration retrieved")

			if oldConfig == nil || !cmp.Equal(*oldConfig, *newConfig) {
				log.WithFields(logrus.Fields{
					"newConfig": *newConfig,
				}).Info("Config change detected")
				err = render.RenderFile(cfgPath, templatePath, newConfig)
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
				log.Info("API reachable through HAProxy")
				ensureHAProxyPreRoutingRule(apiVip, apiPort, lbPort)
			} else {
				log.Info("API still not reachable through HAProxy")
			}

			log.Infof("Waiting %.0f seconds to check again", interval.Seconds())
			time.Sleep(interval)
		}
	}
}
