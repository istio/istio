package log

import (
	"fmt"
	"io"
	"os"

	"istio.io/pkg/log"
)

// Logger provides optional log taps for console and test buffer outputs.
type Logger interface {
	LogAndPrint(v ...interface{})
	LogAndError(v ...interface{})
	LogAndFatal(a ...interface{})
	LogAndPrintf(format string, a ...interface{})
	LogAndErrorf(format string, a ...interface{})
	LogAndFatalf(format string, a ...interface{})
	Print(s string)
	PrintErr(s string)
}

// DefaultLogger is a passthrough to istio.io/pkg/log.
type DefaultLogger struct{}

// NewDefaultLogger creates a new logger and returns a pointer to it.
func NewDefaultLogger() *DefaultLogger {
	return &DefaultLogger{}
}

func (l *DefaultLogger) LogAndPrint(v ...interface{}) {
	if len(v) == 0 {
		return
	}
	s := fmt.Sprint(v...)
	log.Infof(s)
}

func (l *DefaultLogger) LogAndError(v ...interface{}) {
	if len(v) == 0 {
		return
	}
	s := fmt.Sprint(v...)
	log.Infof(s)
}

func (l *DefaultLogger) LogAndFatal(a ...interface{}) {
	l.LogAndError(a...)
	os.Exit(-1)
}

func (l *DefaultLogger) LogAndPrintf(format string, a ...interface{}) {
	s := fmt.Sprintf(format, a...)
	log.Infof(s)
}

func (l *DefaultLogger) LogAndErrorf(format string, a ...interface{}) {
	s := fmt.Sprintf(format, a...)
	log.Infof(s)
}

func (l *DefaultLogger) LogAndFatalf(format string, a ...interface{}) {
	l.LogAndErrorf(format, a...)
	os.Exit(-1)
}

func (l *DefaultLogger) Print(s string) {
}

func (l *DefaultLogger) PrintErr(s string) {
}

//ConsoleLogger is the struct used for mesh command
type ConsoleLogger struct {
	logToStdErr bool
	stdOut      io.Writer
	stdErr      io.Writer
}

// NewConsoleLogger creates a new logger and returns a pointer to it.
// stdOut and stdErr can be used to capture output for testing.
func NewConsoleLogger(logToStdErr bool, stdOut, stdErr io.Writer) *ConsoleLogger {
	return &ConsoleLogger{
		logToStdErr: logToStdErr,
		stdOut:      stdOut,
		stdErr:      stdErr,
	}
}

func (l *ConsoleLogger) LogAndPrint(v ...interface{}) {
	if len(v) == 0 {
		return
	}
	s := fmt.Sprint(v...)
	if !l.logToStdErr {
		l.Print(s + "\n")
	} else {
		log.Infof(s)
	}
}

func (l *ConsoleLogger) LogAndError(v ...interface{}) {
	if len(v) == 0 {
		return
	}
	s := fmt.Sprint(v...)
	if !l.logToStdErr {
		l.PrintErr(s + "\n")
	} else {
		log.Infof(s)
	}
}

func (l *ConsoleLogger) LogAndFatal(a ...interface{}) {
	l.LogAndError(a...)
	os.Exit(-1)
}

func (l *ConsoleLogger) LogAndPrintf(format string, a ...interface{}) {
	s := fmt.Sprintf(format, a...)
	if !l.logToStdErr {
		l.Print(s + "\n")
	} else {
		log.Infof(s)
	}
}

func (l *ConsoleLogger) LogAndErrorf(format string, a ...interface{}) {
	s := fmt.Sprintf(format, a...)
	if !l.logToStdErr {
		l.PrintErr(s + "\n")
	} else {
		log.Infof(s)
	}
}

func (l *ConsoleLogger) LogAndFatalf(format string, a ...interface{}) {
	l.LogAndErrorf(format, a...)
	os.Exit(-1)
}

func (l *ConsoleLogger) Print(s string) {
	_, _ = l.stdOut.Write([]byte(s))
}

func (l *ConsoleLogger) PrintErr(s string) {
	_, _ = l.stdErr.Write([]byte(s))
}
