package errors

import "fmt"

// Loggable is anything the error loggers can print to
type Loggable interface {
	Printf(string, ...interface{})
}

// LogIfErr will log to l a Printf message if err is not nil
func LogIfErr(err error, l Loggable, msg string, args ...interface{}) {
	if err != nil {
		l.Printf("%s: %s", err.Error(), fmt.Sprintf(msg, args...))
	}
}

// DeferLogIfErr will log to l a Printf message if the return value of errCallback is not nil.  Intended use
// is during a defer function whos return value you don't really care about.
//
//  func Thing() error {
//    f, err := os.Open("/tmp/a")
//    if err != nil { return Annotate(err, "Cannot open /tmp/a") }
//    defer DeferLogIfErr(f.Close, log, "Cannot close file %s", "/tmp/a")
//    // Do something with f
//  }
func DeferLogIfErr(errCallback func() error, l Loggable, msg string, args ...interface{}) {
	err := errCallback()
	if err != nil {
		l.Printf("%s: %s", err.Error(), fmt.Sprintf(msg, args...))
	}
}

// PanicIfErr is useful if writing shell scripts.  It will panic with a msg if err != nil
func PanicIfErr(err error, msg string, args ...interface{}) {
	if err != nil {
		panic(fmt.Sprintf("%s: %s", err.Error(), fmt.Sprintf(msg, args...)))
	}
}

// PanicIfErrWrite is similar to PanicIfErr, but works well with io results that return integer+err
func PanicIfErrWrite(numWritten int, err error) {
	if err != nil {
		panic(fmt.Sprintf("Write err: %s", err.Error()))
	}
}
