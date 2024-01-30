// Package exec provides helpers for setting execution options for commands
package exec

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

var (
	// DisableRedact will make redact not redact anything
	DisableRedact = false

	// Confirm will make all command execs ask for confirmation - this is a simplistic way for auditing what will be executed
	Confirm = false

	// ConfirmFunc is called to ask for confirmation
	ConfirmFunc = func(s string) bool {
		fmt.Println(s)
		fmt.Print("Allow? [Y/n]: ")
		reader := bufio.NewReader(os.Stdin)
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)
		return text == "" || text == "Y" || text == "y"
	}

	mutex sync.Mutex
)

// Waiter is a process that can be waited to finish
type Waiter interface {
	Wait() error
}
