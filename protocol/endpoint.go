package protocol

import (
	"fmt"
	"net"
	"strconv"
)

// Endpoint represents a network endpoint.
type Endpoint struct {
	Address string `yaml:"address" validate:"required,hostname_rfc1123|ip"`
	Port    int    `yaml:"port" validate:"gt=0,lte=65535"`
}

// TCPAddr returns the TCP address of the endpoint.
func (e *Endpoint) TCPAddr() (*net.TCPAddr, error) {
	ip, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(e.Address, strconv.Itoa(e.Port)))
	if err != nil {
		return nil, fmt.Errorf("resolve address: %w", err)
	}
	return ip, nil
}

// Validate the endpoint.
func (e *Endpoint) Validate() error {
	if e.Address == "" {
		return fmt.Errorf("%w: address is required", ErrValidationFailed)
	}

	if e.Port <= 0 || e.Port > 65535 {
		return fmt.Errorf("%w: port must be between 1 and 65535", ErrValidationFailed)
	}

	return nil
}
