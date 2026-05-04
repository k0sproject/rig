// Package os provides remote OS release information detection
package os

import (
	"errors"
	"fmt"
)

// ErrArchNotDetected is returned by Arch when no architecture was detected during OS resolution.
var ErrArchNotDetected = errors.New("architecture not detected")

// ErrUnrecognizedArch is returned by Arch when the raw architecture string is not a known value.
var ErrUnrecognizedArch = errors.New("unrecognized architecture")

const (
	archAmd64 = "amd64"
	archArm64 = "arm64"
	archArm   = "arm"
	arch386   = "386"
)

// archNormalize maps raw uname -m / PROCESSOR_ARCHITECTURE values to GOARCH
// strings matching the architecture tokens used in k0s release binaries.
var archNormalize = map[string]string{
	// Linux / macOS uname -m outputs
	"x86_64":   archAmd64,
	"aarch64":  archArm64,
	"arm64":    archArm64, // macOS Apple Silicon
	"armv8l":   archArm,
	"armv7l":   archArm,
	"armv6l":   archArm,
	"armv5tel": archArm,
	"aarch32":  archArm,
	"arm32":    archArm,
	"armhfp":   archArm,
	"arm-32":   archArm,
	"i386":     arch386,
	"i686":     arch386,
	// Windows PROCESSOR_ARCHITECTURE values
	"AMD64":   archAmd64,
	"X86_64":  archAmd64,
	"ARM64":   archArm64,
	"AARCH64": archArm64,
	"x86":     arch386,
	"X86":     arch386,
	"I386":    arch386,
}

// Release describes host operating system version information.
type Release struct {
	ID          string            `kv:"ID"`
	IDLike      []string          `kv:"ID_LIKE,delim: "`
	Name        string            `kv:"NAME"`
	Version     string            `kv:"VERSION_ID"`
	ExtraFields map[string]string `kv:"*"`
	arch        string
}

// Arch returns the host CPU architecture as a normalized GOARCH string
// (amd64, arm64, arm, 386). Returns ErrArchNotDetected if the architecture
// was not detected during OS resolution, or ErrUnrecognizedArch if the raw
// value is not a known architecture.
func (o *Release) Arch() (string, error) {
	if o.arch == "" {
		return "", ErrArchNotDetected
	}
	if goarch, ok := archNormalize[o.arch]; ok {
		return goarch, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnrecognizedArch, o.arch)
}

// String returns a human readable representation of the release information.
func (o *Release) String() string {
	if o.Name != "" {
		return o.Name
	}
	return o.ID + " " + o.Version
}
