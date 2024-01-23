// Package exec provides helpers for setting execution options for commands
package exec

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/k0sproject/rig/log"
)

var (
	// DisableRedact will make redact not redact anything
	DisableRedact = false

	// Confirm will make all command execs ask for confirmation - this is a simplistic way for auditing what will be executed
	Confirm = false

	// DebugFunc can be replaced to direct the output of exec logging into your own function (standard sprintf interface)
	DebugFunc = func(s string, args ...any) {
		log.Debugf(s, args...)
	}

	// InfoFunc can be replaced to direct the output of exec logging into your own function (standard sprintf interface)
	InfoFunc = func(s string, args ...any) {
		log.Infof(s, args...)
	}

	// ErrorFunc can be replaced to direct the output of exec logging into your own function (standard sprintf interface)
	ErrorFunc = func(s string, args ...any) {
		log.Errorf(s, args...)
	}

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
