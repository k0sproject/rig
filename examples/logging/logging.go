package main

import (
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
)

func main() {
	rig.SetLogger(&log.StdLog{}) // this is the default. it will also accept a logrus instance for example.

	log.Debugf("Testing DEBUG level logging: %s", "Hello")
	log.Infof("Testing INFO level logging: %s", "Hello")
	log.Errorf("Testing ERROR level logging: %s", "Hello")

	c := &rig.Localhost{Enabled: true}
	c.Exec("echo Hello, world", exec.StreamOutput())

	log.Infof("testing without HideOutput()")
	c.Exec("ls")
	log.Infof("testing with HideOutput()")
	c.Exec("ls", exec.HideOutput())
}
