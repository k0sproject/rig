package log

import (
	"fmt"
	"log"
	"os"
)

type Logger interface {
	Trace(...interface{})
	Debug(...interface{})
	Info(...interface{})
	Warn(...interface{})
	Error(...interface{})
	Fatal(...interface{})

	Traceln(...interface{})
	Debugln(...interface{})
	Infoln(...interface{})
	Warnln(...interface{})
	Errorln(...interface{})
	Fatalln(...interface{})

	Tracef(string, ...interface{})
	Debugf(string, ...interface{})
	Infof(string, ...interface{})
	Warnf(string, ...interface{})
	Errorf(string, ...interface{})
	Fatalf(string, ...interface{})

	SetLevel(uint32)
	IsLevelEnabled(uint32) bool
}

var Log Logger

func Trace(args ...interface{}) {
	Log.Trace(args)
}

func Debug(args ...interface{}) {
	Log.Debug(args)
}

func Info(args ...interface{}) {
	Log.Info(args)
}

func Warn(args ...interface{}) {
	Log.Warn(args)
}

func Error(args ...interface{}) {
	Log.Error(args)
}

func Fatal(args ...interface{}) {
	Log.Fatal(args)
}

func Traceln(args ...interface{}) {
	Log.Traceln(args)
}

func Debugln(args ...interface{}) {
	Log.Debugln(args)
}

func Infoln(args ...interface{}) {
	Log.Info(args)
}

func Warnln(args ...interface{}) {
	Log.Warn(args)
}

func Errorln(args ...interface{}) {
	Log.Warn(args)
}

func Fatalln(args ...interface{}) {
	Log.Fatal(args)
}

func Tracef(t string, args ...interface{}) {
	Log.Tracef(t, args...)
}

func Debugf(t string, args ...interface{}) {
	Log.Debugf(t, args...)
}

func Infof(t string, args ...interface{}) {
	Log.Infof(t, args...)
}

func Warnf(t string, args ...interface{}) {
	Log.Warnf(t, args...)
}

func Errorf(t string, args ...interface{}) {
	Log.Errorf(t, args...)
}

func Fatalf(t string, args ...interface{}) {
	Log.Fatalf(t, args...)
}

func IsLevelEnabled(l uint32) bool {
	return Log.IsLevelEnabled(l)
}

func SetLevel(l uint32) {
	Log.SetLevel(l)
}

type StdLog struct {
	Logger

	level uint32
}

func (l *StdLog) Trace(args ...interface{}) {
	if l.IsLevelEnabled(0) {
		log.Println("TRACE " + fmt.Sprint(args...))
	}
}

func (l *StdLog) Debug(args ...interface{}) {
	if l.IsLevelEnabled(1) {
		log.Println("DEBUG " + fmt.Sprint(args...))
	}
}

func (l *StdLog) Info(args ...interface{}) {
	if l.IsLevelEnabled(2) {
		log.Printf("INFO %s", args...)
	}
}

func (l *StdLog) Warn(args ...interface{}) {
	if l.IsLevelEnabled(3) {
		log.Printf("WARN %s", args...)
	}
}

func (l *StdLog) Error(args ...interface{}) {
	if l.IsLevelEnabled(4) {
		log.Printf("ERROR %s", args...)
	}
}

func (l *StdLog) Fatal(args ...interface{}) {
	log.Printf("FATAL %s", args...)
	os.Exit(1)
}

func (l *StdLog) Traceln(args ...interface{}) {
	l.Trace(args...)
}

func (l *StdLog) Debugln(args ...interface{}) {
	l.Debug(args...)
}

func (l *StdLog) Infoln(args ...interface{}) {
	l.Info(args...)
}

func (l *StdLog) Warnln(args ...interface{}) {
	l.Warn(args...)
}

func (l *StdLog) Errorln(args ...interface{}) {
	l.Error(args...)
}

func (l *StdLog) Fatalln(args ...interface{}) {
	l.Fatal(args...)
}

func (l *StdLog) Tracef(t string, args ...interface{}) {
	l.Trace(fmt.Sprintf(t, args...))
}

func (l *StdLog) Debugf(t string, args ...interface{}) {
	l.Debug(fmt.Sprintf(t, args...))
}

func (l *StdLog) Infof(t string, args ...interface{}) {
	l.Info(fmt.Sprintf(t, args...))
}

func (l *StdLog) Warnf(t string, args ...interface{}) {
	l.Warn(fmt.Sprintf(t, args...))
}

func (l *StdLog) Errorf(t string, args ...interface{}) {
	l.Error(fmt.Sprintf(t, args...))
}

func (l *StdLog) Fatalf(t string, args ...interface{}) {
	l.Fatal(fmt.Sprintf(t, args...))
}

func (l *StdLog) IsLevelEnabled(level uint32) bool {
	return level >= l.level
}

func (l *StdLog) SetLevel(level uint32) {
	l.level = level
}
