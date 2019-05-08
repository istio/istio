package log

// MultiLogger is a logger that sends to all the wrapped Loggers
type MultiLogger []Logger

var _ Logger = MultiLogger(nil)

// Log will call log on every logger that isn't disabled
func (c MultiLogger) Log(keyvals ...interface{}) {
	for _, l := range c {
		if !IsDisabled(l) {
			l.Log(keyvals...)
		}
	}
}

// Disabled returns true if all the wrapped loggers are disabled
func (c MultiLogger) Disabled() bool {
	for _, l := range c {
		if !IsDisabled(l) {
			return false
		}
	}
	return true
}
