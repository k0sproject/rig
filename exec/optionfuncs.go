package exec

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/k0sproject/rig/log"
)

// Option is a functional option type and it appears in function signatures mostly
// in the form of "...exec.Option".
type Option func(*Options)

// Option functions
//
// This file defines functional options for exec.Runner or NewClient(WithExecOptions(..))
//
// These can be used to configure the execution of remote commands, for example
// setting input/output streams, adding text patterns for redacting sensitive
// information from logs or running the command via sudo.
//
// An example of using these options is for example running a command with sudo:
//
// 	err := runner.Exec("reboot", exec.Sudo())
//
// Or running a command that passes a secret token in arguments that needs to be
// redacted from the logs:
//
// 	err := runner.Exec(fmt.Sprintf("cluster join --token=%s", token), exec.RedactStrings(token))

// StdoutPipe is an option that can be used to connect the stdout stream of a command
// to the given writer.
func StdoutPipe(w io.Writer) Option {
	return func(o *Options) {
		o.Stdout = w
	}
}

// StdinPipe is an option that can be used to connect the stdin stream of a command
// to the given reader.
func StdinPipe(r io.Reader) Option {
	return func(o *Options) {
		o.Stdin = r
	}
}

// StderPipe is an option that can be used to connect the stderr stream of a command
// to the given writer.
func StderrPipe(w io.Writer) Option {
	return func(o *Options) {
		o.Stderr = w
	}
}

// StdinString is a shorthand for StdinPipe(strings.NewReader(s))
func StdinString(s string) Option {
	return func(o *Options) {
		o.Stdin = strings.NewReader(s)
	}
}

// RedactStrings is an option that can be used to redact sensitive information from
// the logs. The given strings will be replaced with the RedactMask.
func RedactStrings(v ...string) Option {
	return func(o *Options) {
		o.RedactFuncs = append(o.RedactFuncs, func(s string) string {
			for _, redact := range v {
				s = strings.ReplaceAll(s, redact, RedactMask)
			}
			return s
		})
	}
}

// RedactRegex is an option that can be used to redact sensitive information from
// the logs using a regular expression. The given regex will be used to replace the
// matches with the RedactMask.
func RedactRegex(r *regexp.Regexp) Option {
	return func(o *Options) {
		o.RedactFuncs = append(o.RedactFuncs, func(s string) string {
			return r.ReplaceAllString(s, RedactMask)
		})
	}
}

// RedactFunc is an option that can be used to redact sensitive information from the
// logs using a RedactFn function that takes a string and returns a modified version
// of it.
func RedactFunc(f RedactFn) Option {
	return func(o *Options) {
		o.RedactFuncs = append(o.RedactFuncs, f)
	}
}

// Sudo is an option that can be used to make the runner execute the command
// using elevated privileges. The modified command is obtained using a SudoProvider
// that should be autodetected when sudo is requested.
func Sudo() Option {
	return func(o *Options) {
		o.Sudo = true
	}
}

// NoLogging is an option that can be used to disable logging of the command line,
// its input and output streams. This can be useful when uploading or downloading
// large files through the streams and logging the contents would be too verbose.
func NoLogging() Option {
	return func(o *Options) {
		o.disableLogStreams = true
	}
}

// AllowStderr is an option that can be used to make commands fail if they write
// to stderr even if their exit code is zero. This is mostly useful on windows where
// it is by default used to determine if a command failed, as the exit codes on
// windows are not always a reliable way to determine success or failure.
func AllowStderr(v bool) Option {
	return func(o *Options) {
		o.DisallowStderr = !v
	}
}

// WithLogger can be used to set a custom logger for the runner or command execution.
func WithLogger(l *log.Logger) Option {
	return func(o *Options) {
		o.Logger = l
	}
}

// WithConfirmationDialog turns on prompting for approval dialog before every
// command execution.
func WithConfirmationDialog() Option {
	return func(o *Options) {
		var mutex sync.Mutex
		o.ConfirmFunc = func(s string) bool {
			mutex.Lock()
			defer mutex.Unlock()
			fmt.Println(s)
			fmt.Print("Allow? [Y/n]: ")
			reader := bufio.NewReader(os.Stdin)
			text, _ := reader.ReadString('\n')
			text = strings.TrimSpace(text)
			return text == "" || text == "Y" || text == "y"
		}
	}
}

// WithSudoRepo is an option that can be used to set the sudo provider repository to use.
// This is mostly useful for testing purposes.
func WithSudoRepository(repo SudoProviderRepository) Option {
	return func(o *Options) {
		o.sudoRepo = repo
	}
}

// WithSudoFunc is an option that can be used to override the default sudo provider
// with a custom one. A SudoFn takes a command string and returns a modified version
// of it, for example "reboot" could become "sudo -n reboot". It is mostly useful for
// testing purposes.
func WithSudoFunc(f SudoFn) Option {
	return func(o *Options) {
		o.SudoFn = f
	}
}

// WithPasswordCallback is an option that can be used to set a callback function
// that is used to request passwords
func WithPasswordCallback(f PasswordCallback) Option {
	return func(o *Options) {
		o.PasswordFunc = f
	}
}
