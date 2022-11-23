// Package registry is a registry of OS support modules
package registry

import (
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/errstring"
)

// ErrOSModuleNotFound is returned when no suitable OS support module is found
var ErrOSModuleNotFound = errstring.New("os support module not found")

type (
	buildFunc = func() interface{}
	matchFunc = func(rig.OSVersion) bool
)

type osFactory struct {
	MatchFunc matchFunc
	BuildFunc buildFunc
}

var osModules []*osFactory

// RegisterOSModule registers a OS support module into rig's registry
func RegisterOSModule(mf matchFunc, bf buildFunc) {
	// Inserting to beginning to match the most latest added
	osModules = append([]*osFactory{{MatchFunc: mf, BuildFunc: bf}}, osModules...)
}

// GetOSModuleBuilder returns a suitable OS support module from rig's registry
func GetOSModuleBuilder(osv rig.OSVersion) (buildFunc, error) {
	for _, of := range osModules {
		if of.MatchFunc(osv) {
			return of.BuildFunc, nil
		}
	}

	return nil, ErrOSModuleNotFound
}
