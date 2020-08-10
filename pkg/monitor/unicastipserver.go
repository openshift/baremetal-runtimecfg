package monitor

import (
	"net"
	"strconv"
	"strings"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
)

// UnicastIPServer starts a server
func UnicastIPServer(apiVip, ingressVip, dnsVip net.IP, unicastIPServerPort uint16, remoteBackendCh chan string) error {
	_, nonVirtualIP, err := config.GetVRRPConfig(apiVip, ingressVip, dnsVip)

	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", net.JoinHostPort("::", strconv.Itoa(int(unicastIPServerPort))))
	if err != nil {
		return err
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Print(err)
			continue
		}
		log.Infof("Connection from %v accepted", conn.RemoteAddr().String())
		clientIpAddr := strings.Split(conn.RemoteAddr().String(), ":")[0]
		log.Infof("Adding %s to remoteBackendCh", clientIpAddr)
		remoteBackendCh <- clientIpAddr

		go handleConn(conn, nonVirtualIP.IP.String())
	}
}

func handleConn(conn net.Conn, nonVirtualIP string) {
	log.Infof("Writing %v to socket", nonVirtualIP)
	conn.Write([]byte(nonVirtualIP + "\n"))
	conn.Close()
}
