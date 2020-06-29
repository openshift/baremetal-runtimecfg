package monitor

import (
	"net"
	"strconv"
	"strings"

	"github.com/coreos/go-iptables/iptables"
	"github.com/sirupsen/logrus"
)

const (
	table = "nat"
	chain = "PREROUTING"
)

func getHAProxyRuleSpec(apiVip string, apiPort, lbPort uint16) (ruleSpec []string, err error) {
	apiPortStr := strconv.Itoa(int(apiPort))
	lbPortStr := strconv.Itoa(int(lbPort))
	ruleSpec = []string{"--dst", apiVip, "-p", "tcp", "--dport", apiPortStr, "-j", "REDIRECT", "--to-ports", lbPortStr, "-m", "comment", "--comment", "OCP_API_LB_REDIRECT"}
	return ruleSpec, err
}

func getProtocolbyIp(ipStr string) iptables.Protocol {
	net_ipStr := net.ParseIP(ipStr)
	if net_ipStr.To4() != nil {
		return iptables.ProtocolIPv4
	}
	return iptables.ProtocolIPv6
}

func cleanHAProxyPreRoutingRule(apiVip string, apiPort, lbPort uint16) error {
	ipt, err := iptables.NewWithProtocol(getProtocolbyIp(apiVip))
	if err != nil {
		return err
	}

	ruleSpec, err := getHAProxyRuleSpec(apiVip, apiPort, lbPort)
	if err != nil {
		return err
	}

	if exists, _ := ipt.Exists(table, chain, ruleSpec...); exists {
		log.WithFields(logrus.Fields{
			"spec": strings.Join(ruleSpec, " "),
		}).Info("Removing existing nat PREROUTING rule")
		return ipt.Delete(table, chain, ruleSpec...)
	}
	return nil
}

func ensureHAProxyPreRoutingRule(apiVip string, apiPort, lbPort uint16) error {
	ipt, err := iptables.NewWithProtocol(getProtocolbyIp(apiVip))
	if err != nil {
		return err
	}

	ruleSpec, err := getHAProxyRuleSpec(apiVip, apiPort, lbPort)
	if err != nil {
		return err
	}
	if exists, _ := ipt.Exists(table, chain, ruleSpec...); exists {
		return nil
	} else {
		log.WithFields(logrus.Fields{
			"spec": strings.Join(ruleSpec, " "),
		}).Info("Inserting nat PREROUTING rule")
		return ipt.Insert(table, chain, 1, ruleSpec...)
	}
}

func checkHAProxyPreRoutingRule(apiVip string, apiPort, lbPort uint16) (bool, error) {
	ipt, err := iptables.NewWithProtocol(getProtocolbyIp(apiVip))
	if err != nil {
		return false, err
	}

	ruleSpec, err := getHAProxyRuleSpec(apiVip, apiPort, lbPort)
	if err != nil {
		return false, err
	}

	exists, _ := ipt.Exists(table, chain, ruleSpec...)
	return exists, nil
}
