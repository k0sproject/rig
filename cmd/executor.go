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
	"github.com/k0sproject/rig/v2/sh"
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
	gate       CommandGate
	shell      string
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

// SetCommandGate installs a [CommandGate] that is consulted by the Start and
// Exec family of methods before a command runs. A nil gate disables gating.
// The gate is not consulted by [Executor.StartProcess], which exists only to
// satisfy the connection interface for runner chaining. SetCommandGate must
// not be called concurrently with Start, Exec, or any other command-execution
// method.
func (r *Executor) SetCommandGate(gate CommandGate) {
	r.gate = gate
}

// SetShell imposes an explicit shell for running commands on non-Windows hosts.
// When set, commands are wrapped in `<shell> -c <quoted command>` so that the
// remote user's login shell, which is not guaranteed to be POSIX compatible,
// never gets to interpret rig's POSIX command strings. POSIX shells and fish are
// covered; see [shellescape.QuoteForLoginShell] for what a csh login shell can
// still break.
// Use [sh.DefaultShell] unless the host is known to keep its POSIX shell
// somewhere else. An empty shell disables the wrapping.
//
// Windows commands are never wrapped, and neither are commands formatted before
// the host's OS has been determined, in which case [Executor.Explain] reports
// OSWrappingKnown: false. Decorators that bring a shell of their own, like the
// ones in the sudo package, keep using [sh.DefaultShell] regardless of this
// setting; they only need a shell for the command they elevate, and the shell
// set here still wraps the result.
//
// SetShell must not be called concurrently with Start, Exec, or any other
// command-execution method.
func (r *Executor) SetShell(shell string) {
	r.shell = shell
}

// windowsShellPrefix runs a command that is not an executable through cmd.exe on
// Windows hosts. We don't know whether the default shell there is cmd or
// powershell, so non-prefixed commands consistently go through cmd.exe.
func windowsShellPrefix(cmd string, isWindows bool) string {
	if isWindows && !isExe(cmd) {
		return "cmd.exe /C " + cmd
	}
	return cmd
}

// applyDecorators runs the decorators in order. When mask is set it runs after
// every decorator, so a secret is masked at the first point it appears in full,
// before a later decorator quotes it into pieces a literal redacter can no
// longer match.
func applyDecorators(cmd string, decorators []DecorateFunc, mask func(string) string) string {
	for _, decorator := range decorators {
		cmd = decorator(cmd)
		if mask != nil {
			cmd = mask(cmd)
		}
	}
	return cmd
}

func (r *Executor) formatCommandForOS(command string, execOpts *ExecOptions, isWindows bool) string {
	return windowsShellPrefix(r.Format(execOpts.Format(command)), isWindows)
}

// parentFormat applies the formatting that every parent runner contributes,
// masking after each of their decorators when mask is set. A chained runner - the
// sudo runners wrap the base one - has its command formatted by its parents too,
// which is how an imposed shell reaches a sudo-decorated command.
//
// Start applies this itself and starts the process on the connection underneath
// the parents (see [Executor.baseConnection]), so that each decorator runs once
// for a command and the string reported to logs, tracers and a [CommandGate] is
// the very one that gets sent.
func (r *Executor) parentFormat(cmd string, mask func(string) string) string {
	for parent, ok := r.connection.(*Executor); ok; parent, ok = parent.connection.(*Executor) {
		cmd = parent.format(cmd, mask)
	}
	return cmd
}

// baseConnection returns the first connection in the chain that is not an
// [Executor], which is the one Start hands a fully formatted command to. Every
// Executor in between has already contributed its formatting via parentFormat;
// going through their StartProcess would apply their decorators a second time.
func (r *Executor) baseConnection() protocol.ProcessStarter {
	conn := r.connection
	for {
		parent, ok := conn.(*Executor)
		if !ok {
			return conn
		}
		conn = parent.connection
	}
}

// formatCommand returns the fully decorated command string.
func (r *Executor) formatCommand(command string, execOpts *ExecOptions) string {
	return r.formatCommandForOS(command, execOpts, r.IsWindows())
}

// explainCommand returns the command as this runner formats it, before any
// parent runner formats it again, and whether the host's OS was known.
func (r *Executor) explainCommand(command string, execOpts *ExecOptions) (string, bool) {
	if !r.osKnown.Load() {
		return r.formatCommandForOS(command, execOpts, false), false
	}
	return r.formatCommandForOS(command, execOpts, r.IsWindows()), true
}

// redactedForm returns the human-readable form of a command for logging and for
// a [CommandGate]: registered secrets are masked and PowerShell -EncodedCommand
// payloads are decoded. formatted must be the command as the host receives it,
// including what parent runners add; with nothing to mask, decoding it is all
// this has to do.
//
// A secret that contains a single quote or a backslash needs more care, because
// both the sudo decorators and the imposed shell quote what they wrap, and
// quoting rewrites exactly those two characters - splitting the secret into
// pieces a literal redacter can no longer match. For those, the command is
// formatted a second time starting from the masked original, masking again after
// every decorator so that a secret a decorator introduces itself is caught
// before the next decorator or the shell wrapping quotes it.
//
// That second pass feeds decorators an input the host never sees, so a decorator
// whose output depends on the content of the command it is given can describe a
// command that differs from the one sent (see [DecorateFunc]). That is accepted:
// the alternative is printing the secret. Every other secret - one without a
// quote or a backslash in it - survives quoting intact and is masked in the
// formatted command directly, with no second pass at all.
func (r *Executor) redactedForm(command, formatted string, execOpts *ExecOptions, isWindows bool) string {
	if !execOpts.hasRedaction() {
		return decodeEncoded(formatted)
	}
	mask := execOpts.Redacter().Redact
	if !execOpts.needsMaskedReplay() {
		return mask(decodeEncoded(formatted))
	}
	cmd := execOpts.formatMasked(mask(command), mask)
	cmd = r.format(cmd, mask)
	cmd = r.parentFormat(windowsShellPrefix(cmd, isWindows), mask)
	return mask(decodeEncoded(cmd))
}

// Explain returns the formatted command without running it. Use this to
// inspect the effect of decorators, sudo wrapping, shell imposition,
// PowerShell encoding, and redaction without executing anything. Explain never probes the host
// to determine OS-specific wrapping; wrapping is included only when the OS
// has already been determined (see OSWrappingKnown in the returned Explanation).
func (r *Executor) Explain(command string, opts ...ExecOption) Explanation {
	execOpts := Build(opts...)
	ownFormatted, osWrappingKnown := r.explainCommand(command, execOpts)
	// What the host receives includes the formatting the parent runners add.
	formatted := r.parentFormat(ownFormatted, nil)
	decoded := decodeEncoded(formatted)
	logged := ""
	if execOpts.LogCommand() {
		logged = r.redactedForm(command, formatted, execOpts, osWrappingKnown && r.IsWindows())
	}
	return Explanation{
		Formatted:       formatted,
		Decoded:         decoded,
		Logged:          logged,
		CommandLogged:   execOpts.LogCommand(),
		OSWrappingKnown: osWrappingKnown,
	}
}

// Format returns the command string decorated with the runner's global
// decorators and, when a shell has been imposed via [Executor.SetShell],
// wrapped in an explicit shell invocation.
func (r *Executor) Format(cmd string) string {
	return r.format(cmd, nil)
}

// format applies the global decorators and the imposed shell wrapping, masking
// after every decorator when mask is set. See [applyDecorators].
func (r *Executor) format(cmd string, mask func(string) string) string {
	cmd = applyDecorators(cmd, r.decorators, mask)
	if r.shellWrapApplies() {
		cmd = sh.ShellWith(r.shell, cmd)
	}
	return cmd
}

// shellWrapApplies reports whether the imposed shell should wrap a command.
// The OS check is deliberately probe-free: the command-execution path resolves
// and caches the host's OS before Format runs (Start evaluates IsWindows to
// build the formatCommandForOS argument), while Explain must not probe and
// leaves out OS-specific wrapping until the OS is known.
func (r *Executor) shellWrapApplies() bool {
	if r.shell == "" || !r.osKnown.Load() {
		return false
	}
	return !r.isWin()
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

// cleanStderr collapses a command's stderr into a single line and strips the
// CLIXML framing PowerShell wraps its error records in. It deliberately does not
// shorten the result: that happens in [ProcessError.Error], so the full output
// stays available to callers through [StderrOf].
func cleanStderr(stderr string, isWindows bool) string {
	if stderr == "" {
		return ""
	}
	stderr = strings.TrimSpace(strings.ReplaceAll(stderr, "\n", " "))
	if isWindows {
		stderr = strings.ReplaceAll(stderr, "\r", "")
		stderr = strings.TrimPrefix(stderr, "#<CLIXML")
		stderr = xmlTagRe.ReplaceAllString(stderr, "")
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

	stderr := cleanStderr(w.opts.ErrString(), w.isWindows)
	if waitErr == nil && w.isWindows && !w.opts.AllowWinStderr() && len(stderr) > 0 {
		waitErr = ErrWroteStderr
	}
	var result error
	if waitErr != nil {
		result = &ProcessError{err: waitErr, stderr: stderr}
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

// closeAll closes every closer, ignoring individual close errors.
func closeAll(closers []io.Closer) {
	for _, c := range closers {
		_ = c.Close()
	}
}

// gateCommand consults the runner's [CommandGate] for the given command, which
// must already be in redacted, human-readable form. It returns nil when there
// is no gate, when the command opts out via [Ungated], or when the gate allows
// the command. A non-nil return means the command must not be started: a
// rejection wraps [ErrCommandRejected], while a context cancellation/deadline
// is wrapped without [ErrCommandRejected] so that errors.Is reports the context
// error but not a rejection, letting callers tell the two apart.
func (r *Executor) gateCommand(ctx context.Context, execOpts *ExecOptions, redacted string) error {
	if r.gate == nil || execOpts.SkipGate() {
		return nil
	}
	err := r.gate.AllowCommand(ctx, r.String(), redacted)
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		log.Trace(ctx, "command gate cancelled", log.HostAttr(r), log.KeyCommand, redacted, log.KeyError, err)
		return fmt.Errorf("command gate: %w", err)
	}
	log.Trace(ctx, "command rejected by gate", log.HostAttr(r), log.KeyCommand, redacted, log.KeyError, err)
	return fmt.Errorf("%w: %w", ErrCommandRejected, err)
}

// Start starts the command and returns a Waiter.
func (r *Executor) Start(ctx context.Context, command string, opts ...ExecOption) (protocol.Waiter, error) {
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

	// fullCmd is the command as the host receives it: this runner's formatting
	// plus what every parent runner contributes. It is both what gets sent (to
	// the connection underneath the parents, so their decorators are not applied
	// twice) and what tracers, the gate and the logs describe.
	fullCmd := r.parentFormat(cmd, nil)

	// redactedCmd is the human-readable command (secrets masked, PowerShell
	// -EncodedCommand decoded), computed once and reused for gating and every
	// log line below. Tracer callbacks deliberately receive the unredacted
	// command, as their contract documents.
	redactedCmd := r.redactedForm(command, fullCmd, execOpts, r.IsWindows())

	// Consult the gate before emitting any command-logging or tracer
	// lifecycle events so a rejected command produces no dangling
	// "executing"/CommandFormatted/ProcessStarted events.
	if err := r.gateCommand(ctx, execOpts, redactedCmd); err != nil {
		closeAll(traceClosers)
		return nil, err
	}

	log.Trace(ctx, "starting command", log.HostAttr(r), log.KeyCommand, redactedCmd)

	if execOpts.LogCommand() {
		r.Log().Debug("executing command", log.KeyCommand, redactedCmd)
	}

	if tracer != nil {
		tracer.CommandFormatted(r.String(), fullCmd)
	}

	stdout := execOpts.Stdout()
	stderr := execOpts.Stderr()
	traceClosers = append(traceClosers, execOpts.OutputClosers()...)

	waiter, err := r.baseConnection().StartProcess(ctx, fullCmd, execOpts.Stdin(), stdout, stderr) //nolint:contextcheck // Stdin() uses trace logger which takes context
	if err != nil {
		closeAll(traceClosers)
		log.Trace(ctx, "start process failed", log.HostAttr(r), log.KeyCommand, redactedCmd, log.KeyError, err)
		return nil, fmt.Errorf("runner start command: %w", err)
	}

	if waiter == nil {
		closeAll(traceClosers)
		log.Trace(ctx, "start process returned nil waiter", log.HostAttr(r), log.KeyCommand, redactedCmd, log.KeyError, errInternal)
		return nil, fmt.Errorf("%w: connection returned no error but a nil waiter", errInternal)
	}

	started := time.Now()
	if tracer != nil {
		tracer.ProcessStarted(r.String(), fullCmd)
	}

	return &waiterWrapper{
		waiter:       waiter,
		opts:         execOpts,
		isWindows:    r.IsWindows(),
		tracer:       tracer,
		host:         r.String(),
		formatted:    fullCmd,
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

	// Start gates the command and logs it once in its canonical redacted,
	// decoded, fully decorated form. These traces only mark the execoutput
	// lifecycle, so they deliberately omit the command to avoid re-deriving
	// (and drifting from) that canonical form.
	proc, err := r.Start(ctx, command, opts...)
	if err != nil {
		return "", fmt.Errorf("start command: %w", err)
	}
	log.Trace(ctx, "waiting on command", log.HostAttr(r))
	if err := proc.Wait(); err != nil {
		log.Trace(ctx, "waiting returned an error", log.HostAttr(r), log.KeyError, err)
		return "", fmt.Errorf("command result: %w", err)
	}

	log.Trace(ctx, "command finished", log.HostAttr(r))
	return Build(opts...).FormatOutput(out.String()), nil
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
	// ExecContext -> Start gates and logs the command once in its canonical
	// redacted, decoded form. These traces only mark the execreader lifecycle,
	// so they deliberately omit the command to avoid re-deriving it here.
	go func() {
		if err := r.ExecContext(ctx, command, opts...); err != nil {
			log.Trace(ctx, "execreader: execcontext returned an error", log.HostAttr(r), log.KeyError, err)
			pipeW.CloseWithError(fmt.Errorf("exec reader: %w", err))
		} else {
			pipeW.Close()
		}
		log.Trace(ctx, "execreader: execcontext finished", log.HostAttr(r))
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
	// With a shell imposed, resolve the host's OS first. The wrapping decision in
	// Format is deliberately probe-free so that Explain never probes, which means
	// the OS has to be known by the time Format runs, and a runner that is not an
	// Executor can reach this method without going through Start. Skipped when no
	// shell is imposed: there is no wrapping decision to make, and probing would
	// cost a command on a connection that cannot report its OS by itself.
	if r.shell != "" {
		r.IsWindows()
	}
	waiter, err := r.connection.StartProcess(ctx, r.Format(command), stdin, stdout, stderr)
	if err != nil {
		return nil, fmt.Errorf("runner start process: %w", err)
	}
	return waiter, nil
}
