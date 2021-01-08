package rig

import "github.com/k0sproject/rig/log"

func SetLogger(logger log.Logger) {
	log.Log = logger
}

func init() {
	SetLogger(&log.StdLog{})
}
