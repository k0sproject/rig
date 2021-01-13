package registry

import (
	"fmt"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
)

type buildFunc = func(os.Host) interface{}
type matchFunc = func(*rig.Os) bool

type osFactory struct {
	MatchFunc matchFunc
	BuildFunc buildFunc
}

var osModules []*osFactory

func RegisterOSModule(mf matchFunc, bf buildFunc) {
	// Inserting to beginning to match the most latest added
	osModules = append([]*osFactory{{MatchFunc: mf, BuildFunc: bf}}, osModules...)
}

func GetOSModuleBuilder(os *rig.Os) (buildFunc, error) {
	for _, of := range osModules {
		if of.MatchFunc(os) {
			return of.BuildFunc, nil
		}
	}

	return nil, fmt.Errorf("os support module not found")
}
