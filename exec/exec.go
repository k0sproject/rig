// Package exec provides helpers for setting execution options for commands
package exec

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"regexp"
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

// Option is a functional option for the exec package
type Option func(*Options)

// Options is a collection of exec options
type Options struct {
	Stdin          string
	AllowWinStderr bool
	LogInfo        bool
	LogDebug       bool
	LogError       bool
	LogCommand     bool
	LogOutput      bool
	StreamOutput   bool
	Sudo           bool
	RedactFunc     func(string) string
	Output         *string
	Writer         io.Writer

	host host
}

type host interface {
	Sudo(cmd string) (string, error)
}

// Command returns the command wrapped in a sudo if sudo is enabled or the original command
func (o *Options) Command(cmd string) (string, error) {
	if !o.Sudo {
		return cmd, nil
	}

	out, err := o.host.Sudo(cmd)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrSudo, err)
	}
	return out, nil
}

func decodeEncoded(cmd string) string {
	if !strings.Contains(cmd, "powershell") {
		return cmd
	}

	parts := strings.Split(cmd, " ")
	for i, p := range parts {
		if p == "-E" || p == "-EncodedCommand" && len(parts) > i+1 {
			decoded, err := base64.StdEncoding.DecodeString(parts[i+1])
			if err == nil {
				parts[i+1] = strings.ReplaceAll(string(decoded), "\x00", "")
			}
		}
	}
	return strings.Join(parts, " ")
}

// LogCmd is for logging the command to be executed
func (o *Options) LogCmd(prefix, cmd string) {
	if Confirm {
		mutex.Lock()
		if !ConfirmFunc(fmt.Sprintf("\nHost: %s\nCommand: %s", prefix, o.Redact(cmd))) {
			os.Stderr.WriteString("aborted\n")
			os.Exit(1)
		}
		mutex.Unlock()
	}

	if o.LogCommand {
		DebugFunc("%s: executing `%s`", prefix, o.Redact(decodeEncoded(cmd)))
	} else {
		DebugFunc("%s: executing command", prefix)
	}
}

// LogStdin is for logging information about command stdin input
func (o *Options) LogStdin(prefix string) {
	if o.Stdin == "" || !o.LogDebug {
		return
	}

	if len(o.Stdin) > 256 {
		o.LogDebugf("%s: writing %d bytes to command stdin", prefix, len(o.Stdin))
	} else {
		o.LogDebugf("%s: writing %d bytes to command stdin: %s", prefix, len(o.Stdin), o.Redact(o.Stdin))
	}
}

// LogDebugf is a conditional debug logger
func (o *Options) LogDebugf(s string, args ...any) {
	if o.LogDebug {
		DebugFunc(s, args...)
	}
}

// LogInfof is a conditional info logger
func (o *Options) LogInfof(s string, args ...any) {
	if o.LogInfo {
		InfoFunc(s, args...)
	}
}

// LogErrorf is a conditional error logger
func (o *Options) LogErrorf(s string, args ...any) {
	if o.LogError {
		ErrorFunc(s, args...)
	}
}

// AddOutput is for appending / displaying output of the command
func (o *Options) AddOutput(prefix, stdout, stderr string) {
	mutex.Lock()
	defer mutex.Unlock()

	if o.Output != nil && stdout != "" {
		*o.Output += stdout
	}

	if o.StreamOutput {
		if stdout != "" {
			InfoFunc("%s: %s", prefix, strings.TrimSpace(o.Redact(stdout)))
		} else if stderr != "" {
			ErrorFunc("%s: %s", prefix, strings.TrimSpace(o.Redact(stderr)))
		}
		return
	}
	if o.LogOutput {
		if stdout != "" {
			DebugFunc("%s: %s", prefix, strings.TrimSpace(o.Redact(stdout)))
		} else if stderr != "" {
			DebugFunc("%s: (stderr) %s", prefix, strings.TrimSpace(o.Redact(stderr)))
		}
	}
}

// AllowWinStderr exec option allows command to output to stderr without failing
func AllowWinStderr() Option {
	return func(o *Options) {
		o.AllowWinStderr = true
	}
}

// Redact is for filtering out sensitive text using a regexp
func (o *Options) Redact(s string) string {
	if DisableRedact || o.RedactFunc == nil {
		return s
	}
	return o.RedactFunc(s)
}

// Stdin exec option for sending data to the command through stdin
func Stdin(t string) Option {
	return func(o *Options) {
		o.Stdin = t
	}
}

// Output exec option for setting output string target
func Output(output *string) Option {
	return func(o *Options) {
		o.Output = output
	}
}

// StreamOutput exec option for sending the command output to info log
func StreamOutput() Option {
	return func(o *Options) {
		o.StreamOutput = true
	}
}

// LogError exec option for enabling or disabling live error logging during exec
func LogError(v bool) Option {
	return func(o *Options) {
		o.LogError = v
	}
}

// HideCommand exec option for hiding the command-string and stdin contents from the logs
func HideCommand() Option {
	return func(o *Options) {
		o.LogCommand = false
	}
}

// HideOutput exec option for hiding the command output from logs
func HideOutput() Option {
	return func(o *Options) {
		o.LogOutput = false
	}
}

// Sensitive exec option for disabling all logging of the command
func Sensitive() Option {
	return func(o *Options) {
		o.LogDebug = false
		o.LogInfo = false
		o.LogError = false
		o.LogCommand = false
	}
}

// Sudo exec option for running the command with elevated permissions
func Sudo(h host) Option {
	return func(o *Options) {
		o.host = h
		o.Sudo = true
	}
}

// Redact exec option for defining a redact regexp pattern that will be replaced with [REDACTED] in the logs
func Redact(rexp string) Option {
	return func(o *Options) {
		re := regexp.MustCompile(rexp)
		o.RedactFunc = func(s2 string) string {
			return re.ReplaceAllString(s2, "[REDACTED]")
		}
	}
}

// RedactString exec option for defining one or more strings to replace with [REDACTED] in the log output
func RedactString(s ...string) Option {
	var newS []string
	for _, str := range s {
		if str != "" {
			newS = append(newS, str)
		}
	}

	return func(o *Options) {
		o.RedactFunc = func(s2 string) string {
			newstr := s2
			for _, r := range newS {
				newstr = strings.ReplaceAll(newstr, r, "[REDACTED]")
			}
			return newstr
		}
	}
}

// Writer exec option for sending command stdout to an io.Writer
func Writer(w io.Writer) Option {
	return func(o *Options) {
		o.Writer = w
	}
}

// Build returns an instance of Options
func Build(opts ...Option) *Options {
	options := &Options{
		Stdin:        "",
		LogInfo:      false,
		LogCommand:   true,
		LogDebug:     true,
		LogError:     false,
		LogOutput:    true,
		StreamOutput: false,
		Sudo:         false,
		Output:       nil,
		Writer:       nil,
		host:         nil,
	}

	for _, o := range opts {
		o(options)
	}

	return options
}
