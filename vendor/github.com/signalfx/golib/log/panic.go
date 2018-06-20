package log

import "bytes"

type panicLogger struct {
	err error
}

// Panic is a logger that always panics
var Panic ErrorHandlingLogger = &panicLogger{}

// Log calls panic for keyvals
func (n *panicLogger) Log(keyvals ...interface{}) {
	r := keyvals
	if n.err != nil {
		r = append(r[0:len(r):len(r)], n.err)
	}
	buf := &bytes.Buffer{}
	f := LogfmtLogger{
		Out:             buf,
		MissingValueKey: Msg,
	}
	if err := f.Log(r...); err != nil {
		panic(r)
	}
	panic(buf.String())
}

// ErrorLogger returns the wrapped logger
func (n *panicLogger) ErrorLogger(err error) Logger {
	return &panicLogger{
		err: err,
	}
}
