package util

import (
	"net"
)

// IsValidAddress checks whether the given IP address is a valid one
func IsValidAddress(address string) bool {
	return net.ParseIP(address) != nil
}
