// Package main demonstrates the confirmation/interactive prompt functionality.
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
	localhost := &rig.Localhost{Enabled: true}
	exec.Confirm = true
	exec.ConfirmFunc = func(s string) bool {
		fmt.Println("Executing function:")
		fmt.Println(s)
		fmt.Print("Allow? [Y/n]: ")

		reader := bufio.NewReader(os.Stdin)
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)
		return text == "" || text == "Y" || text == "y"
	}

	if err := localhost.Exec("echo Hello, world", exec.StreamOutput()); err != nil {
		fmt.Printf("exec error: %v\n", err)
	}
}
