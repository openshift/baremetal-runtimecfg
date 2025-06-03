package monitor

import (
	"net"
	"strconv"
	"strings"

	"github.com/coreos/go-iptables/iptables"
	"github.com/sirupsen/logrus"
)

const (
	table          = "nat"
	isLoopback     = true
	notLoopback    = false
	redirectTarget = true
	dnatTarget     = false
)

func getHAProxyRuleSpec(apiVip string, apiPort, lbPort uint16, loopback, redirect bool) (ruleSpec []string, err error) {
	apiPortStr := strconv.Itoa(int(apiPort))
	lbPortStr := strconv.Itoa(int(lbPort))
	dstStr := net.JoinHostPort(apiVip, lbPortStr)
	ruleSpec = []string{"--dst", apiVip, "-p", "tcp", "--dport", apiPortStr}
	if loopback {
		ruleSpec = append(ruleSpec, "-o", "lo")
	}
	if redirect {
		ruleSpec = append(ruleSpec, "-j", "REDIRECT", "--to-ports", lbPortStr)
	} else {
		ruleSpec = append(ruleSpec, "-j", "DNAT", "--to-destination", dstStr)
	}
	ruleSpec = append(ruleSpec, "-m", "comment", "--comment", "OCP_API_LB_REDIRECT")
	return ruleSpec, err
}

func getProtocolbyIp(ipStr string) iptables.Protocol {
	net_ipStr := net.ParseIP(ipStr)
	if net_ipStr.To4() != nil {
		return iptables.ProtocolIPv4
	}
	return iptables.ProtocolIPv6
}

func cleanHAProxyFirewallRules(apiVip string, apiPort, lbPort uint16) error {
	ipt, err := iptables.NewWithProtocol(getProtocolbyIp(apiVip))
	if err != nil {
		return err
	}

	ruleSpec, err := getHAProxyRuleSpec(apiVip, apiPort, lbPort, notLoopback, dnatTarget)
	if err != nil {
		return err
	}
	chain := "PREROUTING"
	if exists, _ := ipt.Exists(table, chain, ruleSpec...); exists {
		log.WithFields(logrus.Fields{
			"spec": strings.Join(ruleSpec, " "),
		}).Info("Removing existing nat PREROUTING rule")
		err = ipt.Delete(table, chain, ruleSpec...)
		if err != nil {
			return err
		}
	}

	ruleSpec, err = getHAProxyRuleSpec(apiVip, apiPort, lbPort, isLoopback, redirectTarget)
	if err != nil {
		return err
	}
	chain = "OUTPUT"
	if exists, _ := ipt.Exists(table, chain, ruleSpec...); exists {
		log.WithFields(logrus.Fields{
			"spec": strings.Join(ruleSpec, " "),
		}).Info("Removing existing nat OUTPUT rule")
		return ipt.Delete(table, chain, ruleSpec...)
	}
	return nil
}

func ensureHAProxyFirewallRules(apiVip string, apiPort, lbPort uint16) error {
	ipt, err := iptables.NewWithProtocol(getProtocolbyIp(apiVip))
	if err != nil {
		return err
	}

	ruleSpec, err := getHAProxyRuleSpec(apiVip, apiPort, lbPort, notLoopback, dnatTarget)
	if err != nil {
		return err
	}
	chain := "PREROUTING"
	if exists, _ := ipt.Exists(table, chain, ruleSpec...); exists {
		return nil
	}
	log.WithFields(logrus.Fields{
		"spec": strings.Join(ruleSpec, " "),
	}).Info("Inserting nat PREROUTING rule")
	err = ipt.Insert(table, chain, 1, ruleSpec...)
	if err != nil {
		return err
	}

	ruleSpec, err = getHAProxyRuleSpec(apiVip, apiPort, lbPort, isLoopback, redirectTarget)
	if err != nil {
		return err
	}
	chain = "OUTPUT"
	if exists, _ := ipt.Exists(table, chain, ruleSpec...); exists {
		return nil
	}
	log.WithFields(logrus.Fields{
		"spec": strings.Join(ruleSpec, " "),
	}).Info("Inserting nat OUTPUT rule")
	return ipt.Insert(table, chain, 1, ruleSpec...)
}

func checkHAProxyFirewallRules(apiVip string, apiPort, lbPort uint16) (bool, error) {
	ipt, err := iptables.NewWithProtocol(getProtocolbyIp(apiVip))
	if err != nil {
		return false, err
	}

	ruleSpec, err := getHAProxyRuleSpec(apiVip, apiPort, lbPort, notLoopback, dnatTarget)
	if err != nil {
		return false, err
	}
	preroutingExists, err := ipt.Exists(table, "PREROUTING", ruleSpec...)
	if err != nil {
		return false, err
	}

	ruleSpec, err = getHAProxyRuleSpec(apiVip, apiPort, lbPort, isLoopback, redirectTarget)
	if err != nil {
		return false, err
	}
	outputExists, err := ipt.Exists(table, "OUTPUT", ruleSpec...)
	if err != nil {
		return false, err
	}
	return (preroutingExists && outputExists), nil
}
