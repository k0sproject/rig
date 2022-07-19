package main

import (
	"fmt"
	"syscall"

	"github.com/k0sproject/rig"
	"golang.org/x/crypto/ssh/terminal"
)

/*
	This example shows how to use a key password provider
*/

func main() {
	conn := rig.Connection{
		SSH: &rig.SSH{
			User:    "root",
			Address: "localhost",
			PasswordCallback: func() (string, error) {
				fmt.Println("Enter password:")
				pass, err := terminal.ReadPassword(int(syscall.Stdin))
				return string(pass), err
			},
		},
	}
	if err := conn.Connect(); err != nil {
		panic(err)
	}
	defer conn.Disconnect()
	output, err := conn.ExecOutput("ls -al")
	if err != nil {
		panic(err)
	}
	println(output)
}
