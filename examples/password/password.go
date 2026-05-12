// Package main demonstrates how to use password authentication with rig.
package main

import (
	"flag"
	"fmt"
	"syscall"

	"github.com/k0sproject/rig"
	"golang.org/x/term"
)

/*
	This example shows how to use a key password provider
*/

func main() {
	user := flag.String("user", "root", "SSH User")
	host := flag.String("host", "localhost", "Host")
	flag.Parse()
	conn := rig.Connection{
		SSH: &rig.SSH{
			User:    *user,
			Address: *host,
			PasswordCallback: func() (string, error) {
				fmt.Println("Enter password:")
				pass, err := term.ReadPassword(int(syscall.Stdin)) //nolint:unconvert // syscall.Stdin is uintptr on Windows
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
