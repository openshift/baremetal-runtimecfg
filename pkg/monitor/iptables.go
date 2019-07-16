package monitor

import (
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
	ruleSpec = []string{"--src", "0/0", "--dst", apiVip, "-p", "tcp", "--dport", apiPortStr, "-j", "REDIRECT", "--to-ports", lbPortStr, "-m", "comment", "--comment", "OCP_API_LB_REDIRECT"}
	return ruleSpec, err
}

func cleanHAProxyPreRoutingRule(apiVip string, apiPort, lbPort uint16) error {
	ipt, err := iptables.New()
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
	ipt, err := iptables.New()
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
