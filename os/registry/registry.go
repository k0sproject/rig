package registry

import (
	"fmt"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
)

type buildFunc = func(os.Host) interface{}
type matchFunc = func(rig.OSVersion) bool

type osFactory struct {
	MatchFunc matchFunc
	BuildFunc func() interface{}
}

type hostsetter interface {
	SetHost(os.Host)
}

var osModules []*osFactory

// RegisterOSModule registers a OS support module into rig's registry
func RegisterOSModule(mf matchFunc, bf func() interface{}) {
	// Inserting to beginning to match the most latest added
	osModules = append([]*osFactory{{MatchFunc: mf, BuildFunc: bf}}, osModules...)
}

// GetOSModuleBuilder returns a suitable OS support module from rig's registry
func GetOSModuleBuilder(osv rig.OSVersion) (buildFunc, error) {
	for _, of := range osModules {
		if of.MatchFunc(osv) {
			bf := func(h os.Host) interface{} {
				obj := of.BuildFunc()
				if setter, ok := obj.(hostsetter); ok {
					setter.SetHost(h)
				}
				return obj
			}
			return bf, nil
		}
	}

	return nil, fmt.Errorf("os support module not found")
}
