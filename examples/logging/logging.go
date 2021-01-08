package main

import (
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/log"
)

func main() {
	rig.SetLogger(&log.StdLog{}) // this is the default. it will also accept a logrus instance.

	log.SetLevel(0) // this is the default level of the internal simplistic logger
	log.Tracef("Testing trace level logging: %s", "You should see this")

	log.SetLevel(1)
	log.Tracef("Testing trace level logging: %s", "You should not see this")

	log.Debug("This is a debug level message")
	log.Debugln("This is another debug level message")
}
