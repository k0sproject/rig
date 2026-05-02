// package main example shows how to use a key password provider
package main

import (
	"flag"
	"fmt"
	"syscall"

	"github.com/k0sproject/rig"
	"golang.org/x/crypto/ssh/terminal" //nolint:staticcheck
)

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
				pass, err := terminal.ReadPassword(syscall.Stdin)
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
