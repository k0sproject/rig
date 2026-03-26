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

// archNormalize maps raw uname -m / PROCESSOR_ARCHITECTURE values to GOARCH
// strings matching the architecture tokens used in k0s release binaries.
var archNormalize = map[string]string{
	// Linux / macOS uname -m outputs
	"x86_64":   "amd64",
	"aarch64":  "arm64",
	"arm64":    "arm64", // macOS Apple Silicon
	"armv8l":   "arm",
	"armv7l":   "arm",
	"armv6l":   "arm",
	"armv5tel": "arm",
	"aarch32":  "arm",
	"arm32":    "arm",
	"armhfp":   "arm",
	"arm-32":   "arm",
	"i386":     "386",
	"i686":     "386",
	// Windows PROCESSOR_ARCHITECTURE values
	"AMD64":   "amd64",
	"X86_64":  "amd64",
	"ARM64":   "arm64",
	"AARCH64": "arm64",
	"x86":     "386",
	"X86":     "386",
	"I386":    "386",
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
