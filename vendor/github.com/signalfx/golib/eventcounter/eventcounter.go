package eventcounter

import (
	"sync/atomic"
	"time"
)

// EventCounter is used to track events over time, potentially allowing you to limit them
// per time period
type EventCounter struct {
	startingTime  time.Time
	eventDuration time.Duration

	nsSinceStart     int64
	eventsThisPeriod int64
}

// New returns a new EventCounter object that resets event counts per duration.
func New(now time.Time, duration time.Duration) EventCounter {
	return EventCounter{
		startingTime:     now,
		nsSinceStart:     0,
		eventDuration:    duration,
		eventsThisPeriod: 0,
	}
}

// Event returns the number of events in the time period now.  Note, now should be >= the
// last event threshold.  Also, we avoid any loops with compare and swap atomics because we're ok
// being off a few items as long as we're generally accurate and fast.
func (a *EventCounter) Event(now time.Time) int64 {
	return a.Events(now, 1)
}

// Events behaves just like Event() but acts on multiple events at once to reduce the number
// of atomic increment calls
func (a *EventCounter) Events(now time.Time, count int64) int64 {
	for {
		nsSince := now.Sub(a.startingTime).Nanoseconds()
		prevNsSinceStart := atomic.LoadInt64(&a.nsSinceStart)
		if nsSince-prevNsSinceStart >= a.eventDuration.Nanoseconds() {
			//		 Note: There is an accepted race here when we transition states that could cause us to
			//		       rarely miss an event that crosses the threshold.  This is an accepted aspect of
			//		       having a very fast non threshold event
			if atomic.CompareAndSwapInt64(&a.nsSinceStart, prevNsSinceStart, nsSince) {
				atomic.StoreInt64(&a.eventsThisPeriod, 0)
				break
			}
		} else {
			break
		}
	}
	return atomic.AddInt64(&a.eventsThisPeriod, count)
}
