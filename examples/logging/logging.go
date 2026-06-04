// Package main demonstrates logging configuration for rig connections.
package main

import (
	"fmt"

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
	if err := c.Exec("echo Hello, world", exec.StreamOutput()); err != nil {
		fmt.Printf("exec error: %v\n", err)
	}

	log.Infof("testing without HideOutput()")
	if err := c.Exec("ls"); err != nil {
		fmt.Printf("exec error: %v\n", err)
	}
	log.Infof("testing with HideOutput()")
	if err := c.Exec("ls", exec.HideOutput()); err != nil {
		fmt.Printf("exec error: %v\n", err)
	}
}
