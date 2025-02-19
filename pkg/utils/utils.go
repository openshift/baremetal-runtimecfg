package utils

import (
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"net/http"
	"os"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/sirupsen/logrus"
)

const kubeClientTimeout = 30 * time.Second
const haproxyHealthPort = 9454

func FletcherChecksum8(inp string) uint8 {
	var ckA, ckB uint8
	for i := 0; i < len(inp); i++ {
		ckA = (ckA + inp[i]) % 0xf
		ckB = (ckB + ckA) % 0xf
	}
	return (ckB << 4) | ckA
}

func ShortHostname() (shortName string, err error) {
	var hostname string

	if filePath, ok := os.LookupEnv("RUNTIMECFG_HOSTNAME_PATH"); ok {
		dat, err := ioutil.ReadFile(filePath)
		if err != nil {
			log.WithFields(logrus.Fields{
				"filePath": filePath,
			}).Error("Failed to read file")
			return "", err
		}
		hostname = strings.TrimSuffix(string(dat), "\n")
		log.WithFields(logrus.Fields{
			"hostname": hostname,
			"file":     filePath,
		}).Debug("Hostname retrieved from a file")
	} else {
		hostname, err = os.Hostname()
		if err != nil {
			panic(err)
		}
		log.WithFields(logrus.Fields{
			"hostname": hostname,
		}).Debug("Hostname retrieved from OS")
	}
	splitHostname := strings.SplitN(hostname, ".", 2)
	shortName = splitHostname[0]
	return shortName, err
}

func IsKubernetesHealthy(port uint16) (bool, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: transport}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/haproxy_monitor", haproxyHealthPort))
	if err != nil {
		return false, err
	}
	defer client.CloseIdleConnections()
	defer resp.Body.Close()

	return resp.StatusCode == 200, nil
}

func AlarmStabilization(cur_alrm bool, cur_defect bool, consecutive_ctr uint8, on_threshold uint8, off_threshold uint8) (bool, uint8) {
	var new_alrm bool = cur_alrm
	var threshold uint8

	if cur_alrm != cur_defect {
		consecutive_ctr++
		if cur_alrm {
			threshold = off_threshold
		} else {
			threshold = on_threshold
		}
		if consecutive_ctr >= threshold {
			new_alrm = !cur_alrm
			consecutive_ctr = 0
		}
	} else {
		consecutive_ctr = 0
	}
	return new_alrm, consecutive_ctr
}

func GetFileMd5(filePath string) (string, error) {
	var returnMD5String string
	file, err := os.Open(filePath)
	if err != nil {
		return returnMD5String, err
	}
	defer file.Close()
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return returnMD5String, err
	}
	hashInBytes := hash.Sum(nil)[:16]
	returnMD5String = hex.EncodeToString(hashInBytes)
	return returnMD5String, nil
}

// getClientConfig returns a Kubernetes client Config.
func GetClientConfig(kubeApiServerUrl, kubeconfigPath string) (*rest.Config, error) {
	config, err := clientcmd.BuildConfigFromFlags(kubeApiServerUrl, kubeconfigPath)
	if err != nil {
		log.WithFields(logrus.Fields{
			"err": err,
		}).Info("Failed to get client config")
		return nil, err
	}
	// Kubeapi can be not stable on installation process
	// and we should free connection in case it was stuck
	config.Timeout = kubeClientTimeout
	return config, err
}

func Mapper[T, U any](data []T, f func(T) U) []U {
	res := make([]U, 0, len(data))
	for _, e := range data {
		res = append(res, f(e))
	}
	return res
}

// GetNodeIPDebugStatus checks if NodeIP detection debug mode is enabled in the configmap.
// Explicitly ignore errors, as if there is no configmap, no custom config to be applied.
// Function is designed to work in the following way
//   - in boostrap node debug logging should be ENABLED
//   - inside installed cluster
//     -- if config map does not exist, debug logging DISABLED
//     -- if config map exists without "enable-nodeip-debug" key, debug logging DISABLED
//     -- if config map returns error, debug logging
func GetNodeIPDebugStatus(clientset *kubernetes.Clientset) bool {
	if os.Getenv("IS_BOOTSTRAP") == "yes" {
		return true
	}

	cm, err := clientset.CoreV1().ConfigMaps(os.Getenv("POD_NAMESPACE")).Get(context.TODO(), "logging", metav1.GetOptions{})
	if err != nil {
		if strings.HasSuffix(err.Error(), "not found") {
			return false
		}
		log.WithFields(logrus.Fields{"err": err}).Warn("Failed to get logging configuration")
		return true
	}
	if value, ok := cm.Data["enable-nodeip-debug"]; ok {
		if value == "true" {
			return true
		}
	}

	return false
}
