package log

import (
	"encoding"
	"encoding/json"
	"fmt"
	"github.com/signalfx/golib/errors"
	"io"
	"io/ioutil"
	"reflect"
)

// JSONLogger logs out JSON objects to a writer
type JSONLogger struct {
	Out             io.Writer
	MissingValueKey Key
}

var _ ErrorLogger = &JSONLogger{}

// NewJSONLogger creates a new JSON logger
func NewJSONLogger(w io.Writer, ErrHandler ErrorHandler) Logger {
	if w == ioutil.Discard {
		return Discard
	}
	return &ErrorLogLogger{
		RootLogger: &JSONLogger{
			Out:             w,
			MissingValueKey: Msg,
		},
		ErrHandler: ErrHandler,
	}
}

// Log will format a JSON map and write it to Out
func (j *JSONLogger) Log(keyvals ...interface{}) error {
	m := mapFromKeyvals(j.MissingValueKey, keyvals...)
	return errors.Annotate(json.NewEncoder(j.Out).Encode(m), "cannot JSON encode log")
}

func mapFromKeyvals(missingValueKey Key, keyvals ...interface{}) map[string]interface{} {
	n := (len(keyvals) + 1) / 2 // +1 to handle case when len is odd
	m := make(map[string]interface{}, n)
	for i := 0; i < len(keyvals); i += 2 {
		var k, v interface{}
		if i == len(keyvals)-1 {
			k, v = missingValueKey, keyvals[i]
		} else {
			k, v = keyvals[i], keyvals[i+1]
		}
		m[mapKey(k)] = mapValue(v)
	}
	return m
}

// Different from go-kit.  People just shouldn't pass nil values and should know if they do
func mapKey(k interface{}) (s string) {
	defer func() {
		if panicVal := recover(); panicVal != nil {
			s = nilCheck(k, panicVal, "NULL").(string)
		}
	}()
	switch x := k.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		return fmt.Sprint(x)
	}
}

func nilCheck(ptr, panicVal interface{}, onError interface{}) interface{} {
	if vl := reflect.ValueOf(ptr); vl.Kind() == reflect.Ptr && vl.IsNil() {
		return onError
	}
	panic(panicVal)
}

func mapValue(v interface{}) (s interface{}) {
	defer func() {
		if panicVal := recover(); panicVal != nil {
			s = nilCheck(v, panicVal, nil)
		}
	}()
	// See newTypeEncoder
	switch x := v.(type) {
	case json.Marshaler:
	case encoding.TextMarshaler:
	case error:
		return x.Error()
	case fmt.Stringer:
		return x.String()
	}
	return v
}
