//go:build !linux

package utils

import (
	"errors"
	"net"
)

// Dummy types for non-Linux platforms (netlink package is Linux-only)
type dummyAddr struct{}

// AddressFilter is a function type to filter addresses (non-functional on non-Linux platforms)
type AddressFilter func(dummyAddr) bool

// RouteFilter is a function type to filter routes (non-functional on non-Linux platforms)
type RouteFilter func(interface{}) bool

// ValidNodeAddress always returns false because it is not supported on non-Linux platforms
func ValidNodeAddress(address dummyAddr) bool {
	return false
}

// ValidOVNNodeAddress always returns false because it is not supported on non-Linux platforms
func ValidOVNNodeAddress(address dummyAddr) bool {
	return false
}

// AddressesRouting returns an error on non-linux because it is not supported on non-Linux platforms
func AddressesRouting(vips []net.IP, af AddressFilter, preferIPv6 bool) ([]net.IP, error) {
	return nil, errors.New("AddressesRouting is only supported on Linux")
}

// AddressesDefault returns an error on non-linux because it is not supported on non-Linux platforms
func AddressesDefault(preferIPv6 bool, af AddressFilter) ([]net.IP, error) {
	return nil, errors.New("AddressesDefault is only supported on Linux")
}

// GetInterfaceWithCidrByIP returns an error on non-linux because it is not supported on non-Linux platforms
func GetInterfaceWithCidrByIP(ip net.IP, strictMatch bool) (*net.Interface, *net.IPNet, error) {
	return nil, nil, errors.New("GetInterfaceWithCidrByIP is only supported on Linux")
}
