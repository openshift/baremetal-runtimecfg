//go:build !linux

package monitor

import "fmt"

// ensureHAProxyFirewallRules returns an error on non-linux because it is not supported on non-Linux platforms
func ensureHAProxyFirewallRules(apiVip string, apiPort, lbPort uint16) error {
	return fmt.Errorf("nftables firewall rules are only supported on Linux")
}

// cleanHAProxyFirewallRules returns an error on non-linux because it is not supported on non-Linux platforms
func cleanHAProxyFirewallRules(apiVip string, apiPort, lbPort uint16) error {
	return fmt.Errorf("nftables firewall rules are only supported on Linux")
}

// checkHAProxyFirewallRules returns an error on non-linux because it is not supported on non-Linux platforms
func checkHAProxyFirewallRules(apiVip string, apiPort, lbPort uint16) (bool, error) {
	return false, fmt.Errorf("nftables firewall rules are only supported on Linux")
}

// ensureCorednsFirewallRules returns an error on non-linux because it is not supported on non-Linux platforms
func ensureCorednsFirewallRules(port uint16) error {
	return fmt.Errorf("nftables firewall rules are only supported on Linux")
}

// cleanCorednsFirewallRules returns an error on non-linux because it is not supported on non-Linux platforms
func cleanCorednsFirewallRules(port uint16) error {
	return fmt.Errorf("nftables firewall rules are only supported on Linux")
}

// checkCorednsFirewallRules returns an error on non-linux because it is not supported on non-Linux platforms
func checkCorednsFirewallRules(port uint16) (bool, error) {
	return false, fmt.Errorf("nftables firewall rules are only supported on Linux")
}
