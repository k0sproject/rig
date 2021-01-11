package log

import "fmt"

type Logger interface {
	Debugf(string, ...interface{})
	Infof(string, ...interface{})
	Errorf(string, ...interface{})
}

var Log Logger

func Debugf(t string, args ...interface{}) {
	Log.Debugf(t, args...)
}

func Infof(t string, args ...interface{}) {
	Log.Infof(t, args...)
}

func Errorf(t string, args ...interface{}) {
	Log.Errorf(t, args...)
}

type StdLog struct {
	Logger
}

func (l *StdLog) Debugf(t string, args ...interface{}) {
	fmt.Println("DEBUG", fmt.Sprintf(t, args...))
}

func (l *StdLog) Infof(t string, args ...interface{}) {
	fmt.Println("INFO ", fmt.Sprintf(t, args...))
}

func (l *StdLog) Errorf(t string, args ...interface{}) {
	fmt.Println("ERROR", fmt.Sprintf(t, args...))
}
