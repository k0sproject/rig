package log

import (
	"io"
	"sync/atomic"

	"golang.org/x/exp/slog"
)

// Relevant x/exp/slog types are shadowed here to avoid having to import it directly elsewhere
// in the codebase. This will help with the eventual transition to stdlib slog if it is added or
// some other logging library.

type Attr = slog.Attr
type Level = slog.Level
type Leveler = slog.Leveler
type Logger = slog.Logger
type Value = slog.Value

const (
	LevelDebug = slog.LevelDebug // LevelDebug low level messages for diagnosing connection issues
	LevelInfo  = slog.LevelInfo  // LevelInfo general informational messages such as stdout from a command
	LevelWarn  = slog.LevelWarn  // LevelWarn warnings, such as ssh hostkey mismatch
	LevelError = slog.LevelError // LevelError failure messages such as stderr from commands
)

var (
	// These are slog.Attr functions (log.Bool("key", true), etc)
	String   = slog.String
	Int64    = slog.Int64
	Int      = slog.Int
	Bool     = slog.Bool
	Duration = slog.Duration
	Any      = slog.Any
	AnyValue = slog.AnyValue

	defaultLogger atomic.Value
)

func init() {
	// Default logging handler is a no-op handler that will output nothing.
	defaultLogger.Store(slog.New(NopHandler))
}

// Default returns the default Logger.
func Default() *Logger { return defaultLogger.Load().(*Logger) } //nolint:forcetypeassert

// SetLogger sets the logger used by rig
func SetLogger(l *Logger) { defaultLogger.Store(l) }

// New logging at the given level.
func New(out io.Writer, lvl Level) *Logger {
	slogOpts := slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(_ []string, attr Attr) Attr {
			switch attr.Key {
			case slog.TimeKey:
				// Don't output time, it will be added by the consuming logger
				// if desired (easier to add than remove).
				return slog.Attr{}
			case slog.LevelKey:
				// Everything from rig is considered debug, let the consuming
				// logger display its own levels. WARN+ERROR left in for
				// them to stand out.
				if v, ok := attr.Value.Any().(Level); ok && v < LevelWarn {
					return slog.Attr{}
				}
				return attr
			default:
				return attr
			}
		},
	}
	return slog.New(slogOpts.NewTextHandler(out))
}

type Logging struct {
	logger *Logger
}

func (l *Logging) Log() *Logger {
	return l.logger
}

func (l *Logging) SetLogger(logger *Logger) {
	l.logger = logger
}
