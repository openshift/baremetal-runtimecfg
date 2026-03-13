//go:build linux

package monitor

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"github.com/google/nftables/userdata"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

const (
	tableName       = "ocp_nat"
	chainPrerouting = "ocp_prerouting"
	chainOutput     = "ocp_output"
	ruleComment     = "OCP_API_LB_REDIRECT"
)

// getFamily returns (v4, v6 or 0 [invalid ip - error])
func getFamily(ipStr string) (nftables.TableFamily, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return 0, fmt.Errorf("invalid IP address: %s", ipStr)
	}
	if ip.To4() != nil {
		return nftables.TableFamilyIPv4, nil
	}
	return nftables.TableFamilyIPv6, nil
}

func getOrCreateTable(conn *nftables.Conn, family nftables.TableFamily) (*nftables.Table, error) {
	tables, err := conn.ListTablesOfFamily(family)
	if err != nil {
		return nil, fmt.Errorf("failed to get tables: %v", err)
	}
	for _, table := range tables {
		if table.Name == tableName {
			return table, nil
		}
	}
	table := &nftables.Table{
		Family: family,
		Name:   tableName,
	}
	table = conn.AddTable(table)
	return table, nil
}

func getOrCreateChain(conn *nftables.Conn, table *nftables.Table, name string, hook *nftables.ChainHook) (*nftables.Chain, error) {
	chains, err := conn.ListChainsOfTableFamily(table.Family)
	if err != nil {
		return nil, fmt.Errorf("failed to get chains: %v", err)
	}
	for _, chain := range chains {
		if chain.Table.Name == table.Name && chain.Name == name {
			return chain, nil
		}
	}
	priority := nftables.ChainPriorityNATDest // -100
	chainType := nftables.ChainTypeNAT
	chain := &nftables.Chain{
		Name:     name,
		Table:    table,
		Type:     chainType,
		Hooknum:  hook,
		Priority: priority,
	}
	chain = conn.AddChain(chain)
	return chain, nil
}

func exprMatchDestIP(apiVip string, family nftables.TableFamily) ([]expr.Any, error) {
	ip := net.ParseIP(apiVip)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", apiVip)
	}

	var ipBytes []byte
	var offset uint32
	var length uint32

	if family == nftables.TableFamilyIPv4 {
		ipBytes = ip.To4()
		offset = 16 // IPv4 destination address offset
		length = 4  // IPv4 address length
	} else {
		ipBytes = ip.To16()
		offset = 24 // IPv6 destination address offset
		length = 16 // IPv6 address length
	}

	return []expr.Any{
		&expr.Payload{
			DestRegister: 1,
			Base:         expr.PayloadBaseNetworkHeader,
			Offset:       offset,
			Len:          length,
		},
		&expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     ipBytes,
		},
	}, nil
}

func exprMatchTCP() []expr.Any {
	return []expr.Any{
		&expr.Meta{
			Key:      expr.MetaKeyL4PROTO,
			Register: 1,
		},
		&expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     []byte{unix.IPPROTO_TCP},
		},
	}
}

func exprMatchDestPort(port uint16) []expr.Any {
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, port)

	return []expr.Any{
		&expr.Payload{
			DestRegister: 1,
			Base:         expr.PayloadBaseTransportHeader,
			Offset:       2, // TCP destination port offset
			Len:          2,
		},
		&expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     portBytes,
		},
	}
}

func exprMatchLoopback() []expr.Any {
	return []expr.Any{
		&expr.Meta{
			Key:      expr.MetaKeyOIFNAME,
			Register: 1,
		},
		&expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     []byte("lo\x00"), // null-terminated interface name
		},
	}
}

func exprDNAT(apiVip string, lbPort uint16, family nftables.TableFamily) ([]expr.Any, error) {
	ip := net.ParseIP(apiVip)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", apiVip)
	}

	var ipBytes []byte
	var nfproto uint32

	if family == nftables.TableFamilyIPv4 {
		ipBytes = ip.To4()
		nfproto = unix.NFPROTO_IPV4
	} else {
		ipBytes = ip.To16()
		nfproto = unix.NFPROTO_IPV6
	}

	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, lbPort)

	return []expr.Any{
		&expr.Immediate{
			Register: 1,
			Data:     ipBytes,
		},
		&expr.Immediate{
			Register: 2,
			Data:     portBytes,
		},
		&expr.NAT{
			Type:        expr.NATTypeDestNAT,
			Family:      nfproto,
			RegAddrMin:  1,
			RegProtoMin: 2,
		},
	}, nil
}

func exprRedirect(lbPort uint16) []expr.Any {
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, lbPort)

	return []expr.Any{
		&expr.Immediate{
			Register: 1,
			Data:     portBytes,
		},
		&expr.Redir{
			RegisterProtoMin: 1,
			RegisterProtoMax: 1,
		},
	}
}

func buildPreroutingRule(table *nftables.Table, chain *nftables.Chain, apiVip string, apiPort, lbPort uint16, family nftables.TableFamily) (*nftables.Rule, error) {
	var exprs []expr.Any

	ipExprs, err := exprMatchDestIP(apiVip, family)
	if err != nil {
		return nil, err
	}
	exprs = append(exprs, ipExprs...)

	exprs = append(exprs, exprMatchTCP()...)

	exprs = append(exprs, exprMatchDestPort(apiPort)...)

	dnatExprs, err := exprDNAT(apiVip, lbPort, family)
	if err != nil {
		return nil, err
	}
	exprs = append(exprs, dnatExprs...)

	rule := &nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: exprs,
	}
	rule.UserData = userdata.AppendString(nil, userdata.TypeComment, ruleComment)

	return rule, nil
}

func buildOutputRule(table *nftables.Table, chain *nftables.Chain, apiVip string, apiPort, lbPort uint16, family nftables.TableFamily) (*nftables.Rule, error) {
	var exprs []expr.Any

	ipExprs, err := exprMatchDestIP(apiVip, family)
	if err != nil {
		return nil, err
	}
	exprs = append(exprs, ipExprs...)

	exprs = append(exprs, exprMatchTCP()...)

	exprs = append(exprs, exprMatchDestPort(apiPort)...)

	exprs = append(exprs, exprMatchLoopback()...)

	exprs = append(exprs, exprRedirect(lbPort)...)

	rule := &nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: exprs,
	}
	rule.UserData = userdata.AppendString(nil, userdata.TypeComment, ruleComment)

	return rule, nil
}

func findRuleByComment(conn *nftables.Conn, chain *nftables.Chain, comment string) (*nftables.Rule, error) {
	rules, err := conn.GetRules(chain.Table, chain)
	if err != nil {
		return nil, fmt.Errorf("failed to get rules: %v", err)
	}

	for _, rule := range rules {
		if ruleComment, ok := userdata.GetString(rule.UserData, userdata.TypeComment); ok {
			if ruleComment == comment {
				return rule, nil
			}
		}
	}

	return nil, nil
}

func ensureHAProxyFirewallRules(apiVip string, apiPort, lbPort uint16) error {
	conn, err := nftables.New()
	if err != nil {
		return fmt.Errorf("failed to create nftables connection: %v", err)
	}
	defer conn.CloseLasting()

	family, err := getFamily(apiVip)
	if err != nil {
		return err
	}

	table, err := getOrCreateTable(conn, family)
	if err != nil {
		return err
	}

	preroutingChain, err := getOrCreateChain(conn, table, chainPrerouting, nftables.ChainHookPrerouting)
	if err != nil {
		return err
	}

	existingRule, err := findRuleByComment(conn, preroutingChain, ruleComment)
	if err != nil {
		return err
	}

	if existingRule == nil {
		rule, err := buildPreroutingRule(table, preroutingChain, apiVip, apiPort, lbPort, family)
		if err != nil {
			return err
		}

		conn.InsertRule(rule)

		log.WithFields(logrus.Fields{
			"chain":  chainPrerouting,
			"table":  table.Name,
			"family": family,
		}).Info("Inserting nftables PREROUTING rule")
	}

	outputChain, err := getOrCreateChain(conn, table, chainOutput, nftables.ChainHookOutput)
	if err != nil {
		return err
	}

	existingRule, err = findRuleByComment(conn, outputChain, ruleComment)
	if err != nil {
		return err
	}

	if existingRule == nil {
		rule, err := buildOutputRule(table, outputChain, apiVip, apiPort, lbPort, family)
		if err != nil {
			return err
		}

		conn.InsertRule(rule)

		log.WithFields(logrus.Fields{
			"chain":  chainOutput,
			"table":  table.Name,
			"family": family,
		}).Info("Inserting nftables OUTPUT rule")
	}

	if err := conn.Flush(); err != nil {
		return fmt.Errorf("failed to flush nftables changes: %v", err)
	}

	return nil
}

func cleanHAProxyFirewallRules(apiVip string, apiPort, lbPort uint16) error {
	conn, err := nftables.New()
	if err != nil {
		return fmt.Errorf("failed to create nftables connection: %v", err)
	}
	defer conn.CloseLasting()

	family, err := getFamily(apiVip)
	if err != nil {
		return err
	}

	table, err := getOrCreateTable(conn, family)
	if err != nil {
		return err
	}

	preroutingChain, err := getOrCreateChain(conn, table, chainPrerouting, nftables.ChainHookPrerouting)
	if err != nil {
		return err
	}

	rule, err := findRuleByComment(conn, preroutingChain, ruleComment)
	if err != nil {
		return err
	}

	if rule != nil {
		log.WithFields(logrus.Fields{
			"chain":  chainPrerouting,
			"table":  table.Name,
			"family": family,
		}).Info("Removing nftables PREROUTING rule")

		if err := conn.DelRule(rule); err != nil {
			return fmt.Errorf("failed to delete PREROUTING rule: %v", err)
		}
	}

	outputChain, err := getOrCreateChain(conn, table, chainOutput, nftables.ChainHookOutput)
	if err != nil {
		return err
	}

	rule, err = findRuleByComment(conn, outputChain, ruleComment)
	if err != nil {
		return err
	}

	if rule != nil {
		log.WithFields(logrus.Fields{
			"chain":  chainOutput,
			"table":  table.Name,
			"family": family,
		}).Info("Removing nftables OUTPUT rule")

		if err := conn.DelRule(rule); err != nil {
			return fmt.Errorf("failed to delete OUTPUT rule: %v", err)
		}
	}

	if err := conn.Flush(); err != nil {
		return fmt.Errorf("failed to flush nftables changes: %v", err)
	}

	return nil
}

// checkHAProxyFirewallRules checks if both HAProxy firewall rules exist
// (true if both exist, false if one or both are missing)
func checkHAProxyFirewallRules(apiVip string, apiPort, lbPort uint16) (bool, error) {
	conn, err := nftables.New()
	if err != nil {
		return false, fmt.Errorf("failed to create nftables connection: %v", err)
	}
	defer conn.CloseLasting()

	family, err := getFamily(apiVip)
	if err != nil {
		return false, err
	}

	table, err := getOrCreateTable(conn, family)
	if err != nil {
		return false, err
	}

	preroutingChain, err := getOrCreateChain(conn, table, chainPrerouting, nftables.ChainHookPrerouting)
	if err != nil {
		return false, err
	}

	preroutingRule, err := findRuleByComment(conn, preroutingChain, ruleComment)
	if err != nil {
		return false, err
	}

	outputChain, err := getOrCreateChain(conn, table, chainOutput, nftables.ChainHookOutput)
	if err != nil {
		return false, err
	}

	outputRule, err := findRuleByComment(conn, outputChain, ruleComment)
	if err != nil {
		return false, err
	}

	return (preroutingRule != nil && outputRule != nil), nil
}
