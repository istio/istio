package log

import "sync/atomic"

// Counter will count log statements
type Counter struct {
	Count int64
}

var _ Logger = &Counter{}

// Log increments Count
func (c *Counter) Log(keyvals ...interface{}) {
	atomic.AddInt64(&c.Count, 1)
}

// ErrorLogger returns itself
func (c *Counter) ErrorLogger(error) Logger {
	return c
}
