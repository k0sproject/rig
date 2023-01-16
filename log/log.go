// Package log provides a simple pluggable logging interface
package log

import "fmt"

// Logger interface should be implemented by the logging library you wish to use
type Logger interface {
	Tracef(string, ...interface{})
	Debugf(string, ...interface{})
	Infof(string, ...interface{})
	Warnf(string, ...interface{})
	Errorf(string, ...interface{})
}

// Log can be assigned a proper logger, such as logrus configured to your liking.
var Log Logger

// Tracef logs a trace level log message
func Tracef(t string, args ...interface{}) {
	Log.Debugf(t, args...)
}

// Debugf logs a debug level log message
func Debugf(t string, args ...interface{}) {
	Log.Debugf(t, args...)
}

// Infof logs an info level log message
func Infof(t string, args ...interface{}) {
	Log.Infof(t, args...)
}

// Errorf logs an error level log message
func Errorf(t string, args ...interface{}) {
	Log.Errorf(t, args...)
}

// Warnf logs a warn level log message
func Warnf(t string, args ...interface{}) {
	Log.Warnf(t, args...)
}

// StdLog is a simplistic logger for rig
type StdLog struct {
	Logger
}

// Tracef prints a debug level log message
func (l *StdLog) Tracef(t string, args ...interface{}) {
	fmt.Println("TRACE", fmt.Sprintf(t, args...))
}

// Debugf prints a debug level log message
func (l *StdLog) Debugf(t string, args ...interface{}) {
	fmt.Println("DEBUG", fmt.Sprintf(t, args...))
}

// Infof prints an info level log message
func (l *StdLog) Infof(t string, args ...interface{}) {
	fmt.Println("INFO ", fmt.Sprintf(t, args...))
}

// Warnf prints a warn level log message
func (l *StdLog) Warnf(t string, args ...interface{}) {
	fmt.Println("WARN", fmt.Sprintf(t, args...))
}

// Errorf prints an error level log message
func (l *StdLog) Errorf(t string, args ...interface{}) {
	fmt.Println("ERROR", fmt.Sprintf(t, args...))
}
