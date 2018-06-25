package log

type nop struct{}

// Discard is a logger that does nothing
var Discard ErrorHandlingDisableableLogger = nop{}

// Log does nothing
func (n nop) Log(keyvals ...interface{}) {
}

// ErrorLogger returns the discard logger
func (n nop) ErrorLogger(error) Logger {
	return n
}

// Disabled always returns true
func (n nop) Disabled() bool {
	return true
}
