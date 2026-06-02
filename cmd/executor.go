package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"

	"github.com/k0sproject/rig/v2/iostream"
	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/protocol"
)

// validate interfaces.
var (
	_ Runner        = (*Executor)(nil)
	_ SimpleRunner  = (*Executor)(nil)
	_ ContextRunner = (*Executor)(nil)
	_ Formatter     = (*Executor)(nil)
	_ fmt.Stringer  = (*Executor)(nil)
)

// DisableRedact will disable all redaction of sensitive data.
var DisableRedact = false

var (
	errInternal       = errors.New("internal error")
	errOddUTF16Length = errors.New("odd byte length in UTF-16LE payload")
)

// bufferPool recycles bytes.Buffer instances for ExecOutputContext to reduce allocations.
var bufferPool = sync.Pool{
	New: func() any { return &bytes.Buffer{} },
}

// Executor is an Runner that runs commands on a host.
type Executor struct {
	log.LoggerInjectable
	connection protocol.ProcessStarter
	decorators []DecorateFunc
	isWin      func() bool
	osKnown    atomic.Bool
	tracer     Tracer
}

func isWinFunc(conn protocol.ProcessStarter) func() bool {
	return func() bool {
		if wc, ok := conn.(WindowsChecker); ok {
			return wc.IsWindows()
		}
		cmd, err := conn.StartProcess(context.Background(), "ver.exe", nil, nil, nil)
		if err != nil || cmd.Wait() != nil {
			return false
		}
		return true
	}
}

// NewExecutor returns a new Executor.
func NewExecutor(conn protocol.ProcessStarter, decorators ...DecorateFunc) *Executor {
	return &Executor{
		connection: conn,
		decorators: decorators,
		isWin:      sync.OnceValue(isWinFunc(conn)),
	}
}

// SetTracer attaches a runner-wide [Tracer]. It fires for every command
// unless overridden per-call via the [Trace] option.
// SetTracer must not be called concurrently with Start, Exec, or any other
// command-execution method.
func (r *Executor) SetTracer(t Tracer) {
	r.tracer = t
}

func (r *Executor) formatCommandForOS(command string, execOpts *ExecOptions, isWindows bool) string {
	cmd := r.Format(execOpts.Format(command))
	if isWindows && !isExe(cmd) {
		cmd = "cmd.exe /C " + cmd
	}
	return cmd
}

// formatCommand returns the fully decorated command string.
func (r *Executor) formatCommand(command string, execOpts *ExecOptions) string {
	return r.formatCommandForOS(command, execOpts, r.IsWindows())
}

func (r *Executor) explainCommand(command string, execOpts *ExecOptions) (string, bool) {
	if !r.osKnown.Load() {
		return r.formatCommandForOS(command, execOpts, false), false
	}
	return r.formatCommandForOS(command, execOpts, r.IsWindows()), true
}

// Explain returns the formatted command without running it. Use this to
// inspect the effect of decorators, sudo wrapping, PowerShell encoding,
// and redaction without executing anything. Explain never probes the host
// to determine OS-specific wrapping; wrapping is included only when the OS
// has already been determined (see OSWrappingKnown in the returned Explanation).
func (r *Executor) Explain(command string, opts ...ExecOption) Explanation {
	execOpts := Build(opts...)
	formatted, osWrappingKnown := r.explainCommand(command, execOpts)
	decoded := decodeEncoded(formatted)
	logged := ""
	if execOpts.LogCommand() {
		logged = execOpts.Redact(decoded)
	}
	return Explanation{
		Formatted:       formatted,
		Decoded:         decoded,
		Logged:          logged,
		CommandLogged:   execOpts.LogCommand(),
		OSWrappingKnown: osWrappingKnown,
	}
}

// Format returns the command string decorated with the runner's global decorators.
func (r *Executor) Format(cmd string) string {
	for _, decorator := range r.decorators {
		cmd = decorator(cmd)
	}
	return cmd
}

// Proc returns a Proc bound to this runner for the given command.
// Set Stdin, Stdout, Stderr on the returned Proc before calling Start or Run.
func (r *Executor) Proc(cmd string) *Proc {
	return &Proc{runner: r, command: cmd}
}

// IsWindows returns true if the host is running Windows.
func (r *Executor) IsWindows() bool {
	result := r.isWin()
	r.osKnown.Store(true)
	return result
}

// String returns the client's string representation.
func (r *Executor) String() string {
	if s, ok := r.connection.(fmt.Stringer); ok {
		return s.String()
	}
	return "rig-executor"
}

func getPrintfErrorAt(s string, idx int) error {
	if idx > len(s)-6 {
		// can't fit %!a()
		return nil
	}

	if s[idx+1] != '!' {
		return nil
	}

	if (s[idx+2] < 'a' || s[idx+2] > 'z') && (s[idx+2] < 'A' || s[idx+2] > 'Z') {
		return nil
	}

	if s[idx+3] != '(' {
		return nil
	}

	end := strings.Index(s[idx:], ")")
	if end == -1 {
		return nil
	}

	return fmt.Errorf("%w: printf error at index %d: %s", ErrInvalidCommand, idx, s[idx:idx+end+1])
}

func findPrintfError(s string) error {
	var err error
	for idx, c := range s {
		if c == '%' && idx < len(s)-1 {
			if e := getPrintfErrorAt(s, idx); e != nil {
				err = errors.Join(e, err)
			}
		}
	}
	return err
}

type waiterWrapper struct {
	waiter       protocol.Waiter
	opts         *ExecOptions
	isWindows    bool
	tracer       Tracer
	host         string
	formatted    string
	started      time.Time
	traceClosers []io.Closer
}

var xmlTagRe = regexp.MustCompile(`<.+?>`)

func formatStderr(stderr string, isWindows bool) string {
	if stderr != "" {
		stderr = strings.TrimSpace(strings.ReplaceAll(stderr, "\n", " "))
		if isWindows {
			stderr = strings.ReplaceAll(stderr, "\r", "")
			stderr = strings.TrimPrefix(stderr, "#<CLIXML")
			stderr = xmlTagRe.ReplaceAllString(stderr, "")
		}
		if len(stderr) > 100 {
			stderr = stderr[:97] + "..."
		}
	}
	return stderr
}

// Wait waits for the command to finish and returns an error if it fails or if it wrote to stderr.
func (w *waiterWrapper) Wait() error {
	waitErr := w.waiter.Wait()

	// flush per-line trace writers before reading errBuf so all output is delivered
	for _, c := range w.traceClosers {
		_ = c.Close()
	}

	stderr := formatStderr(w.opts.ErrString(), w.isWindows)
	if waitErr == nil && w.isWindows && !w.opts.AllowWinStderr() && len(stderr) > 0 {
		waitErr = ErrWroteStderr
	}
	var result error
	if waitErr != nil {
		if len(stderr) > 0 {
			result = fmt.Errorf("process finished with error: %w (%s)", waitErr, stderr)
		} else {
			result = fmt.Errorf("process finished with error: %w", waitErr)
		}
	}
	if w.tracer != nil {
		w.tracer.ProcessFinished(w.host, w.formatted, time.Since(w.started), result)
	}
	return result
}

func isExe(cmd string) bool {
	firstWord, _, found := strings.Cut(cmd, " ")
	if !found {
		return strings.HasSuffix(cmd, ".exe")
	}
	return strings.HasSuffix(firstWord, ".exe")
}

func decodeUTF16LE(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	if len(raw)%2 != 0 {
		return "", errOddUTF16Length
	}
	words := make([]uint16, len(raw)/2)
	for i := range words {
		words[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	return string(utf16.Decode(words)), nil
}

func decodeEncoded(cmd string) string {
	if !strings.Contains(cmd, "powershell") {
		return cmd
	}

	parts := strings.Split(cmd, " ")
	for i, p := range parts {
		if (p == "-E" || p == "-EncodedCommand") && i+1 < len(parts) {
			if plain, err := decodeUTF16LE(parts[i+1]); err == nil {
				parts[i+1] = plain
			}
		}
	}
	return strings.Join(parts, " ")
}

// Start starts the command and returns a Waiter.
func (r *Executor) Start(ctx context.Context, command string, opts ...ExecOption) (protocol.Waiter, error) {
	log.Trace(ctx, "starting command", log.HostAttr(r), log.KeyCommand, command)
	if ctx.Err() != nil {
		return nil, fmt.Errorf("runner context error: %w", ctx.Err())
	}
	if err := findPrintfError(command); err != nil {
		return nil, fmt.Errorf("refusing to run a command containing printf-style %%!(..) errors: %w", err)
	}

	execOpts := Build(opts...)
	r.InjectLoggerTo(execOpts) //nolint:contextcheck // uses trace logger which takes context

	// resolve active tracer: per-call overrides runner-wide
	tracer := execOpts.tracer
	if tracer == nil {
		tracer = r.tracer
	}

	// wire per-line output hooks for OutputTracer before computing final writers
	var traceClosers []io.Closer //nolint:prealloc // OutputClosers length is unknown until Stdout/Stderr are called below
	if ot, ok := tracer.(OutputTracer); ok {
		host := r.String()
		outSW := iostream.NewScanWriter(func(line string) { ot.StdoutLine(host, line) })
		errSW := iostream.NewScanWriter(func(line string) { ot.StderrLine(host, line) })
		traceClosers = []io.Closer{outSW, errSW}
		execOpts.traceOut = outSW
		execOpts.traceErr = errSW
	}

	// we don't know if the default shell is cmd or powershell, so to make sure commands
	// without a shell prefix go consistently go through the same shell, we default to running
	// non-prefixed commands through cmd.exe.
	cmd := r.formatCommand(command, execOpts)

	if execOpts.LogCommand() {
		r.Log().Debug("executing command", log.KeyCommand, execOpts.Redact(decodeEncoded(cmd)))
	}

	if tracer != nil {
		tracer.CommandFormatted(r.String(), cmd)
	}

	stdout := execOpts.Stdout()
	stderr := execOpts.Stderr()
	traceClosers = append(traceClosers, execOpts.OutputClosers()...)

	waiter, err := r.connection.StartProcess(ctx, cmd, execOpts.Stdin(), stdout, stderr) //nolint:contextcheck // Stdin() uses trace logger which takes context
	if err != nil {
		for _, c := range traceClosers {
			_ = c.Close()
		}
		log.Trace(ctx, "start process failed", log.HostAttr(r), log.KeyCommand, cmd, log.KeyError, err)
		return nil, fmt.Errorf("runner start command: %w", err)
	}

	if waiter == nil {
		for _, c := range traceClosers {
			_ = c.Close()
		}
		log.Trace(ctx, "start process returned nil waiter", log.HostAttr(r), log.KeyCommand, cmd, log.KeyError, err)
		return nil, fmt.Errorf("%w: connection returned no error but a nil waiter", errInternal)
	}

	started := time.Now()
	if tracer != nil {
		tracer.ProcessStarted(r.String(), cmd)
	}

	return &waiterWrapper{
		waiter:       waiter,
		opts:         execOpts,
		isWindows:    r.IsWindows(),
		tracer:       tracer,
		host:         r.String(),
		formatted:    cmd,
		started:      started,
		traceClosers: traceClosers,
	}, nil
}

// StartBackground starts the command and returns a Waiter.
func (r *Executor) StartBackground(command string, opts ...ExecOption) (protocol.Waiter, error) {
	return r.Start(context.Background(), command, opts...)
}

// ExecContext executes the command and returns an error if unsuccessful.
func (r *Executor) ExecContext(ctx context.Context, command string, opts ...ExecOption) error {
	proc, err := r.Start(ctx, command, opts...)
	if err != nil {
		return fmt.Errorf("start command: %w", err)
	}
	if err := proc.Wait(); err != nil {
		return fmt.Errorf("command result: %w", err)
	}

	return nil
}

// Exec executes the command and returns an error if unsuccessful.
func (r *Executor) Exec(command string, opts ...ExecOption) error {
	return r.ExecContext(context.Background(), command, opts...)
}

// ExecOutputContext executes the command and returns the stdout output or an error.
func (r *Executor) ExecOutputContext(ctx context.Context, command string, opts ...ExecOption) (string, error) {
	out, ok := bufferPool.Get().(*bytes.Buffer)
	if !ok {
		out = &bytes.Buffer{}
	}
	defer func() {
		if out.Cap() <= 64<<10 {
			clear(out.Bytes()) // zero backing array so output doesn't linger in pool memory
			out.Reset()
			bufferPool.Put(out)
		}
	}()

	opts = append(opts, Stdout(out))

	log.Trace(ctx, "starting command for execoutput", log.HostAttr(r), log.KeyCommand, command)
	proc, err := r.Start(ctx, command, opts...)
	if err != nil {
		return "", fmt.Errorf("start command: %w", err)
	}
	log.Trace(ctx, "waiting on command", log.HostAttr(r), log.KeyCommand, command)
	if err := proc.Wait(); err != nil {
		log.Trace(ctx, "waiting returned an error", log.HostAttr(r), log.KeyCommand, command, log.KeyError, err)
		return "", fmt.Errorf("command result: %w", err)
	}

	log.Trace(ctx, "command finished", log.HostAttr(r), log.KeyCommand, command)
	execOpts := Build(opts...)
	return execOpts.FormatOutput(out.String()), nil
}

// ExecOutput executes the command and returns the stdout output or an error.
func (r *Executor) ExecOutput(command string, opts ...ExecOption) (string, error) {
	return r.ExecOutputContext(context.Background(), command, opts...)
}

// ExecReaderContext executes the command and returns a reader for the stdout output. Reads from the
// reader will return any error that occurred during the command's execution.
func (r *Executor) ExecReaderContext(ctx context.Context, command string, opts ...ExecOption) io.Reader {
	pipeR, pipeW := io.Pipe()
	if ctx.Err() != nil {
		pipeW.CloseWithError(fmt.Errorf("context error: %w", ctx.Err()))
		return pipeR
	}
	opts = append(opts, Stdout(pipeW))
	go func() {
		if err := r.ExecContext(ctx, command, opts...); err != nil {
			log.Trace(ctx, "execreader: execcontext returned an error", log.HostAttr(r), log.KeyCommand, command, log.KeyError, err)
			pipeW.CloseWithError(fmt.Errorf("exec reader: %w", err))
		} else {
			pipeW.Close()
		}
		log.Trace(ctx, "execreader: execcontext finished", log.HostAttr(r), log.KeyCommand, command)
	}()
	return pipeR
}

// ExecScannerContext executes the command and returns a bufio.Scanner for the stdout output. Reads from the
// scanner will return any error that occurred during the command's execution.
func (r *Executor) ExecScannerContext(ctx context.Context, command string, opts ...ExecOption) *bufio.Scanner {
	return bufio.NewScanner(r.ExecReaderContext(ctx, command, opts...))
}

// ExecReader executes the command and returns a reader for the stdout output. Reads from the Reader will
// return any error that occurred during the command's execution.
func (r *Executor) ExecReader(command string, opts ...ExecOption) io.Reader {
	return r.ExecReaderContext(context.Background(), command, opts...)
}

// ExecScanner executes the command and returns a bufio.Scanner for the stdout output. Reads from the
// scanner will return any error that occurred during the command's execution.
func (r *Executor) ExecScanner(command string, opts ...ExecOption) *bufio.Scanner {
	return r.ExecScannerContext(context.Background(), command, opts...)
}

// StartProcess calls the connection's StartProcess method. This is done to satisfy the
// connection interface and thus allow chaining of runners.
func (r *Executor) StartProcess(ctx context.Context, command string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (protocol.Waiter, error) {
	waiter, err := r.connection.StartProcess(ctx, r.Format(command), stdin, stdout, stderr)
	if err != nil {
		return nil, fmt.Errorf("runner start process: %w", err)
	}
	return waiter, nil
}
