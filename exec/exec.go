package exec

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/AlecAivazis/survey/v2"
	"github.com/mattn/go-isatty"
	log "github.com/sirupsen/logrus"
)

var (
	// DisableRedact will make redact not redact anything
	DisableRedact = false
	// Confirm will make all command execs ask for confirmation
	Confirm = false

	mutex sync.Mutex
)

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
	StreamOutput   bool
	RedactFunc     func(string) string
	Output         *string
}

// LogCmd is for logging the command to be executed
func (o *Options) LogCmd(prefix, cmd string) {
	if Confirm {
		if !isatty.IsTerminal(os.Stdout.Fd()) {
			os.Stderr.WriteString("--confirm requires an interactive terminal")
			os.Exit(1)
		}

		mutex.Lock()

		confirmed := false
		fmt.Printf("\nHost: %s\nCommand: %s\n", prefix, o.Redact(cmd))
		prompt := &survey.Confirm{
			Message: fmt.Sprintf("Run this command?"),
			Default: true,
		}
		survey.AskOne(prompt, &confirmed)
		if !confirmed {
			os.Stderr.WriteString("aborted\n")
			os.Exit(1)
		}

		mutex.Unlock()
	}

	if o.LogCommand {
		log.Debugf("%s: executing `%s`", prefix, o.Redact(cmd))
	} else {
		log.Debugf("%s: executing [REDACTED]", prefix)
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
func (o *Options) LogDebugf(s string, args ...interface{}) {
	if o.LogDebug {
		log.Debugf(s, args...)
	}
}

// LogInfof is a conditional info logger
func (o *Options) LogInfof(s string, args ...interface{}) {
	if o.LogInfo {
		log.Infof(s, args...)
	}
}

// LogErrorf is a conditional error logger
func (o *Options) LogErrorf(s string, args ...interface{}) {
	if o.LogError {
		log.Errorf(s, args...)
	}
}

// AddOutput is for appending / displaying output of the command
func (o *Options) AddOutput(prefix, s string) {
	if o.Output != nil {
		*o.Output += s
	}

	if o.StreamOutput {
		mutex.Lock()
		defer mutex.Unlock()
		log.Infof("%s: %s", prefix, o.Redact(s))
	} else {
		if log.IsLevelEnabled(log.DebugLevel) {
			mutex.Lock()
			defer mutex.Unlock()
		}
		o.LogDebugf("%s: %s", prefix, o.Redact(s))
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

// HideCommand exec option for hiding the command-string and stdin contents from the logs
func HideCommand() Option {
	return func(o *Options) {
		o.LogCommand = false
	}
}

// HideOutput exec option for hiding the command output from logs
func HideOutput() Option {
	return func(o *Options) {
		o.LogDebug = false
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

// Build returns an instance of Options
func Build(opts ...Option) *Options {
	options := &Options{
		Stdin:        "",
		LogInfo:      false,
		LogCommand:   true,
		LogDebug:     true,
		LogError:     true,
		StreamOutput: false,
		Output:       nil,
	}

	for _, o := range opts {
		o(options)
	}

	return options
}
