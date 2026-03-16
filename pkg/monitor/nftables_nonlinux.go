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
