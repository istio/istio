package timekeeper

import "time"

// TimeKeeper is how we stub the progress of time in applications
type TimeKeeper interface {
	Now() time.Time
	Sleep(dur time.Duration)
	After(d time.Duration) <-chan time.Time
	NewTimer(d time.Duration) Timer
}

// Timer is an object that returns periodic timing events.  We use the builtin time.NewTimer()
// generally but can also stub this out in constructors
type Timer interface {
	Chan() <-chan time.Time
	Stop() bool
}

// builtinTimer is a real-time timer
type builtinTimer struct {
	timer *time.Timer
}

// Stop the internal timer
func (b *builtinTimer) Stop() bool {
	return b.timer.Stop()
}

// Chan returns the single event channel for this timer
func (b *builtinTimer) Chan() <-chan time.Time {
	return b.timer.C
}

var _ Timer = &builtinTimer{}

// RealTime calls bulit in time package functions that follow the real flow of time
type RealTime struct{}

var _ TimeKeeper = RealTime{}

// Now calls time.Now()
func (RealTime) Now() time.Time {
	return time.Now()
}

// Sleep calls time.Sleep
func (RealTime) Sleep(dur time.Duration) {
	time.Sleep(dur)
}

// After calls time.After
func (RealTime) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

// NewTimer calls time.NewTimer and gives it a stubable interface
func (RealTime) NewTimer(d time.Duration) Timer {
	return &builtinTimer{
		timer: time.NewTimer(d),
	}
}
