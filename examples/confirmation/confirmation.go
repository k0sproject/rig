// Package main demonstrates how to use confirmation dialogs with rig.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
)

func main() {
	conn := &rig.Localhost{Enabled: true}
	exec.Confirm = true
	exec.ConfirmFunc = func(cmd string) bool {
		fmt.Println("Executing function:")
		fmt.Println(cmd)
		fmt.Print("Allow? [Y/n]: ")

		reader := bufio.NewReader(os.Stdin)
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)
		return text == "" || text == "Y" || text == "y"
	}

	_ = conn.Exec("echo Hello, world", exec.StreamOutput())
}
