package utils

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
)

func FletcherChecksum8(inp string) uint8 {
	var ckA, ckB uint8
	for i := 0; i < len(inp); i++ {
		ckA = (ckA + inp[i]) % 0xf
		ckB = (ckB + ckA) % 0xf
	}
	return (ckB << 4) | ckA
}

func ShortHostname() (shortName string, err error) {
	hostname, err := os.Hostname()
	if err != nil {
		panic(err)
	}
	splitHostname := strings.SplitN(hostname, ".", 2)
	shortName = splitHostname[0]
	return shortName, err
}

func EtcdShortHostname() (shortName string, err error) {
	shortHostname, err := ShortHostname()
	if err != nil {
		panic(err)
	}
	if !strings.Contains(shortHostname, "master") {
		return "", err
	}
	etcdHostname := strings.Replace(shortHostname, "master", "etcd", 1)
	return etcdHostname, err
}

func GetEtcdSRVMembers(domain string) (srvs []*net.SRV, err error) {
	_, srvs, err = net.LookupSRV("etcd-server-ssl", "tcp", domain)
	if err != nil {
		return srvs, err
	}
	return srvs, err
}

func GetFirstAddr(host string) (string, error) {
	addrs, err := net.LookupHost(host)
	if err != nil {
		return "", err
	}
	return addrs[0], nil
}

func IsKubernetesHealthy(port uint16) (bool, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: transport}
	resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/healthz", port))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	return string(body) == "ok", nil
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
