package log

import (
	"os"
	"sync/atomic"
)

// DefaultLogger is a root hierarchy logger that other packages can point to.  By default it logs to stderr
var DefaultLogger = NewHierarchy(NewLogfmtLogger(os.Stderr, Discard))

// LoggingEnv is the env variable that if exported to /dev/null can disable the default logger
const LoggingEnv = "GOLIB_LOG"

func init() {
	DefaultLogger.setupFromEnv(os.Getenv)
}

// Hierarchy is a type of logger that an atomically point to another logger.  It's primary usage is as a hierarchy of
// loggers where one defaults to another if not set.
type Hierarchy struct {
	logger atomic.Value
}

type atomicStruct struct {
	logger Logger
}

// NewHierarchy creates a Hierarchy type pointer pointing to defaultLogger
func NewHierarchy(defaultLogger Logger) *Hierarchy {
	ret := &Hierarchy{}
	ret.Set(defaultLogger)
	return ret
}

func (l *Hierarchy) setupFromEnv(getEnv func(string) string) {
	v := getEnv(LoggingEnv)
	if v == "/dev/null" || v == os.DevNull {
		l.Set(Discard)
		return
	}
}

// Log calls log of the wrapped logger
func (l *Hierarchy) Log(kvs ...interface{}) {
	// Note: Log here messes up "caller" :/
	if logger := l.loadLogger(); logger != nil {
		logger.Log(kvs...)
	}
}

// Disabled is true if the wrapped logger is disabled
func (l *Hierarchy) Disabled() bool {
	return IsDisabled(l.loadLogger())
}

// CreateChild returns a logger that points to this one by default
func (l *Hierarchy) CreateChild() *Hierarchy {
	return NewHierarchy(l)
}

func (l *Hierarchy) loadLogger() Logger {
	if logger, ok := l.logger.Load().(atomicStruct); ok && logger.logger != nil {
		return logger.logger
	}
	return nil
}

// Set atomically changes where this logger logs to
func (l *Hierarchy) Set(logger Logger) {
	l.logger.Store(atomicStruct{logger})
}
