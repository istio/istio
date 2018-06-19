package log

// Logger is the minimal interface for logging methods
type Logger interface {
	Log(keyvals ...interface{})
}

// Disableable is an optional interface that a logger can implement to signal it is disabled and should not
// be sent log messages for optimization reasons
type Disableable interface {
	Disabled() bool
}

// ErrorHandler can handle errors that various loggers may return
type ErrorHandler interface {
	ErrorLogger(error) Logger
}

// ErrorHandlingLogger wraps Logger and ErrorHandler
type ErrorHandlingLogger interface {
	Logger
	ErrorHandler
}

// ErrorHandlingDisableableLogger wraps Logger and ErrorHandler and Disabled
type ErrorHandlingDisableableLogger interface {
	Logger
	ErrorHandler
	Disableable
}

// ErrorHandlerFunc converts a function into a ErrorHandler
type ErrorHandlerFunc func(error) Logger

// ErrorLogger calls the wrapped function
func (f ErrorHandlerFunc) ErrorLogger(e error) Logger {
	return f(e)
}

// Context allows users to create a logger that appends key/values to log statements.  Note that a nil Context is ok to
// use and works generally as expected, allowing optional logging in default struct{} objects.
type Context struct {
	Logger  Logger
	KeyVals []interface{}
}

// NewContext creates a context out of a logger
func NewContext(logger Logger) *Context {
	if logger == nil {
		return nil
	}
	if c, ok := logger.(*Context); ok {
		return c
	}
	ctx := &Context{
		Logger: logger,
	}
	return ctx
}

// IsDisabled returns true if the wrapped logger implements Disableable (or is nil).  Will signal that it's not worth
// sending a logger messages.
func IsDisabled(l Logger) bool {
	if l == nil {
		return true
	}
	if disable, ok := l.(Disableable); ok && disable.Disabled() {
		return true
	}
	return false
}

// addArrays will add two arrays.  Note that we CANNOT append(a, b...) because that will use the extra
// capacity of a and if two groutines call this at the same time they will race with each other.
func addArrays(a, b []interface{}) []interface{} {
	if len(a) == 0 && len(b) == 0 {
		return []interface{}{}
	}
	n := len(a) + len(b)
	ret := make([]interface{}, 0, n)
	ret = append(ret, a...)
	ret = append(ret, b...)
	return ret
}

// Log calls Log() on the wrapped logger appending the Context's values
func (l *Context) Log(keyvals ...interface{}) {
	// Note: The assumption here is that copyIfDynamic is "slow" since dynamic values
	//       could be slow to calculate.  So to optimize the case of "logs are turned off"
	//       we enable the ability to early return if the logger is off.
	if l == nil || IsDisabled(l.Logger) {
		return
	}
	l.Logger.Log(copyIfDynamic(addArrays(l.KeyVals, keyvals))...)
}

// Disabled returns true if the wrapped logger is disabled
func (l *Context) Disabled() bool {
	return l == nil || IsDisabled(l.Logger)
}

// With returns a new context that adds key/values to log statements
func (l *Context) With(keyvals ...interface{}) *Context {
	if len(keyvals)%2 != 0 {
		panic("Programmer error.  Please call log.Context.With() only with an even number of arguments.")
	}
	if len(keyvals) == 0 || l == nil {
		return l
	}
	return &Context{
		Logger:  l.Logger,
		KeyVals: addArrays(l.KeyVals, keyvals),
	}
}

// WithPrefix is like With but adds keyvalus to the beginning of the eventual log statement
func (l *Context) WithPrefix(keyvals ...interface{}) *Context {
	if len(keyvals)%2 != 0 {
		panic("Programmer error.  Please call log.Context.WithPrefix() only with an even number of arguments.")
	}
	if len(keyvals) == 0 || l == nil {
		return l
	}
	return &Context{
		Logger:  l.Logger,
		KeyVals: addArrays(keyvals, l.KeyVals),
	}
}

// LoggerFunc converts a function into a Logger
type LoggerFunc func(...interface{})

// Log calls the wrapped function
func (f LoggerFunc) Log(keyvals ...interface{}) {
	f(keyvals...)
}

// IfErr is a shorthand that will log an error if err is not nil
func IfErr(l Logger, err error) {
	if err != nil {
		l.Log(Err, err)
	}
}
