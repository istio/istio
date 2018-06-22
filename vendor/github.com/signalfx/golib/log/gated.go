package log

import "sync/atomic"

// Gate will enable or disable a wrapped logger
type Gate struct {
	DisabledFlag int64
	Logger       Logger
}

// Log calls the wrapped logger only if the gate is enabled
func (g *Gate) Log(kvs ...interface{}) {
	if !g.Disabled() {
		g.Logger.Log(kvs...)
	}
}

// Disabled returns true if the gate or wrapped logger is disabled
func (g *Gate) Disabled() bool {
	return atomic.LoadInt64(&g.DisabledFlag) == 1 || IsDisabled(g.Logger)
}

// Disable turns off the gate
func (g *Gate) Disable() {
	atomic.StoreInt64(&g.DisabledFlag, 1)
}

// Enable turns on the gate
func (g *Gate) Enable() {
	atomic.StoreInt64(&g.DisabledFlag, 0)
}
