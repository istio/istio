package testing

import "github.com/go-logr/logr"

// NullLogger is a logr.Logger that does nothing.
type NullLogger struct{}

var _ logr.Logger = NullLogger{}

func (_ NullLogger) Info(_ string, _ ...interface{}) {
	// Do nothing.
}

func (_ NullLogger) Enabled() bool {
	return false
}

func (_ NullLogger) Error(_ error, _ string, _ ...interface{}) {
	// Do nothing.
}

func (log NullLogger) V(_ int) logr.InfoLogger {
	return log
}

func (log NullLogger) WithName(_ string) logr.Logger {
	return log
}

func (log NullLogger) WithValues(_ ...interface{}) logr.Logger {
	return log
}
