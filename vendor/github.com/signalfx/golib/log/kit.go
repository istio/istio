package log

// ErrorLogger is the interface exposed by go-kit
type ErrorLogger interface {
	Log(keyvals ...interface{}) error
}

// DefaultErrorHandler is the error handler used by default in FromGokit methods
var DefaultErrorHandler ErrorHandler = Discard

// ErrorLogLogger turns a gokit logger into a golib logger
type ErrorLogLogger struct {
	RootLogger ErrorLogger
	ErrHandler ErrorHandler
}

// Log calls the go-kit logger and handles errors
func (e *ErrorLogLogger) Log(keyvals ...interface{}) {
	if err := e.RootLogger.Log(keyvals...); err != nil && e.ErrHandler != nil {
		e.ErrorLogger(err).Log(keyvals...)
	}
}

// ErrorLogger returns the internal error logger
func (e *ErrorLogLogger) ErrorLogger(err error) Logger {
	return e.ErrHandler.ErrorLogger(err)
}

var _ ErrorHandlingLogger = &ErrorLogLogger{}

// FromGokit turns a gokit logger into a golib logger
func FromGokit(logger ErrorLogger) *ErrorLogLogger {
	return &ErrorLogLogger{
		RootLogger: logger,
		ErrHandler: DefaultErrorHandler,
	}
}

// LoggerKit wraps a logger to make it go-kit compliant
type LoggerKit struct {
	LogTo Logger
}

// ToGokit turns a golib logger into a go-kit logger
func ToGokit(logger Logger) ErrorLogger {
	if root, ok := logger.(*ErrorLogLogger); ok {
		return root.RootLogger
	}
	return &LoggerKit{
		LogTo: logger,
	}
}

var _ ErrorLogger = &LoggerKit{}

// Log calls the wrapped logger and returns nil
func (k *LoggerKit) Log(keyvals ...interface{}) error {
	k.LogTo.Log(keyvals...)
	return nil
}
