package log

import (
	"github.com/signalfx/golib/timekeeper"
	"gopkg.in/stack.v1"
	"time"
)

// Dynamic values are evaluated at Log() time, not when they are added to the context. They are also only evaulated by
// Context{} objects
type Dynamic interface {
	LogValue() interface{}
}

// DynamicFunc wraps a function to make it Dynamic
type DynamicFunc func() interface{}

// LogValue calls the wrapped function
func (d DynamicFunc) LogValue() interface{} {
	return d()
}

func copyIfDynamic(keyvals []interface{}) []interface{} {
	var newArray []interface{}
	for i := range keyvals {
		if v, ok := keyvals[i].(Dynamic); ok {
			if newArray == nil {
				newArray = make([]interface{}, len(keyvals))
				copy(newArray, keyvals[0:i])
			}
			newArray[i] = v.LogValue()
			continue
		}
		if newArray != nil {
			newArray[i] = keyvals[i]
		}
	}
	if newArray == nil {
		return keyvals
	}
	return newArray
}

// Caller returns line in the stack trace at Depth stack depth
type Caller struct {
	Depth int
}

// LogValue returs the call stack at Depth depth
func (c *Caller) LogValue() interface{} {
	return stack.Caller(c.Depth)
}

// TimeDynamic returns a time.Time() or time string of when the log message happened
type TimeDynamic struct {
	Layout     string
	TimeKeeper timekeeper.TimeKeeper
	UTC        bool
	AsString   bool
}

var _ Dynamic = &TimeDynamic{}

// LogValue returns a timestamp as described by parameters
func (t *TimeDynamic) LogValue() interface{} {
	var now time.Time
	if t.TimeKeeper == nil {
		now = time.Now()
	} else {
		now = t.TimeKeeper.Now()
	}
	if t.UTC {
		now = now.UTC()
	}
	if !t.AsString {
		return now
	}
	if t.Layout == "" {
		return now.Format(time.RFC3339)
	}
	return now.Format(t.Layout)
}

// TimeSince logs the time since the start of the program
type TimeSince struct {
	Start      time.Time
	TimeKeeper timekeeper.TimeKeeper
}

var defaultStartTime = time.Now()

// LogValue returs the time since Start time
func (c *TimeSince) LogValue() interface{} {
	nowFunc := time.Now
	if c.TimeKeeper != nil {
		nowFunc = c.TimeKeeper.Now
	}
	if c.Start.IsZero() {
		return nowFunc().Sub(defaultStartTime)
	}
	return nowFunc().Sub(c.Start)
}

var (
	// DefaultTimestamp returns the local time as a string
	DefaultTimestamp = &TimeDynamic{AsString: true}
	// DefaultTimestampUTC returns local UTC time as a string
	DefaultTimestampUTC = &TimeDynamic{UTC: true, AsString: true}
	// DefaultCaller is what you probably want when using a context
	DefaultCaller = &Caller{Depth: 3}
)
