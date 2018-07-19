// Package sfxclient creates convenient go functions and wrappers to send metrics to SignalFx.
//
// The core of the library is HTTPSink which allows users to send metrics and events to SignalFx
// ad-hoc.  A Scheduler is built on top of this to facility easy management of metrics for multiple
// SignalFx reporters at once in more complex libraries.
//
// HTTPSink
//
// The simplest way to send metrics and events to SignalFx is with HTTPSink.  The only struct
// parameter that needs to be configured is AuthToken.  To make it easier to create common
// Datapoint objects, wrappers exist for Gauge and Cumulative.  An example of sending a hello
// world metric would look like this:
//     func SendHelloWorld() {
//         client := NewHTTPSink()
//         client.AuthToken = "ABCDXYZ"
//         ctx := context.Background()
//         client.AddDatapoints(ctx, []*datapoint.Datapoint{
//             GaugeF("hello.world", nil, 1.0),
//         })
//     }
//
// Scheduler
//
// To facilitate periodic sending of datapoints to SignalFx, a Scheduler abstraction exists.  You
// can use this to report custom metrics to SignalFx at some periodic interval.
//     type CustomApplication struct {
//         queue chan int64
//     }
//     func (c *CustomApplication) Datapoints() []*datapoint.Datapoint {
//         return []*datapoint.Datapoint {
//           sfxclient.Gauge("queue.size", nil, len(queue)),
//         }
//     }
//     func main() {
//         scheduler := sfxclient.NewScheduler()
//         scheduler.Sink.(*HTTPSink).AuthToken = "ABCD-XYZ"
//         app := &CustomApplication{}
//         scheduler.AddCallback(app)
//         go scheduler.Schedule(context.Background())
//     }
//
// RollingBucket and CumulativeBucket
//
// Because counting things and calculating percentiles like p99 or median are common operations,
// RollingBucket and CumulativeBucket exist to make this easier.  They implement the Collector
// interface which allows users to add them to an existing Scheduler.
//
// To run integration tests, testing sending to SignalFx with an actual token, create a file named
// authinfo.json that has your auth Token, similar to the following
//     {
//       "AuthToken": "abcdefg"
//     }
//
// Then execute the following:
//     go test -v --tags=integration -run TestDatapointSending ./sfxclient/
package sfxclient

import (
	"expvar"

	"github.com/signalfx/golib/datapoint"

	"sync"
	"sync/atomic"
	"time"

	"context"
	"github.com/signalfx/golib/errors"
	"github.com/signalfx/golib/log"
	"github.com/signalfx/golib/timekeeper"
)

// DefaultReportingDelay is the default interval Scheduler users to report metrics to SignalFx
const DefaultReportingDelay = time.Second * 20

// DefaultErrorHandler is the default way to handle errors by a scheduler.  It simply prints them to stdout
var DefaultErrorHandler = func(err error) error {
	log.DefaultLogger.Log(log.Err, err, "Unable to handle error")
	return nil
}

// Sink is anything that can receive points collected by a Scheduler.  This can be useful for
// stubbing out your collector to test the points that will be sent to SignalFx.
type Sink interface {
	AddDatapoints(ctx context.Context, points []*datapoint.Datapoint) (err error)
}

// Collector is anything Scheduler can track that emits points
type Collector interface {
	Datapoints() []*datapoint.Datapoint
}

// HashableCollector is a Collector function that can be inserted into a hashmap.  You can use it
// to wrap a functional callback and insert it into a Scheduler.
type HashableCollector struct {
	Callback func() []*datapoint.Datapoint
}

// CollectorFunc wraps a function to make it a Collector.
func CollectorFunc(callback func() []*datapoint.Datapoint) *HashableCollector {
	return &HashableCollector{
		Callback: callback,
	}
}

// Datapoints calls the wrapped function.
func (h *HashableCollector) Datapoints() []*datapoint.Datapoint {
	return h.Callback()
}

var _ Collector = CollectorFunc(nil)

type callbackPair struct {
	callbacks         map[Collector]struct{}
	defaultDimensions map[string]string
	expectedSize      int
}

func (c *callbackPair) getDatapoints(now time.Time, sendZeroTime bool) []*datapoint.Datapoint {
	ret := make([]*datapoint.Datapoint, 0, c.expectedSize)
	for callback := range c.callbacks {
		ret = append(ret, callback.Datapoints()...)
	}
	for _, dp := range ret {
		// It's a bit dangerous to modify the map (we don't know how it was passed in) so
		// make a copy to be safe
		dp.Dimensions = datapoint.AddMaps(c.defaultDimensions, dp.Dimensions)
		if !sendZeroTime && dp.Timestamp.IsZero() {
			dp.Timestamp = now
		}
	}
	c.expectedSize = len(ret)
	return ret
}

// A Scheduler reports metrics to SignalFx at some timely manner.
type Scheduler struct {
	Sink             Sink
	Timer            timekeeper.TimeKeeper
	SendZeroTime     bool
	ErrorHandler     func(error) error
	ReportingDelayNs int64

	callbackMutex      sync.Mutex
	callbackMap        map[string]*callbackPair
	previousDatapoints []*datapoint.Datapoint
	stats              struct {
		scheduledSleepCounts int64
		resetIntervalCounts  int64
	}
}

// NewScheduler creates a default SignalFx scheduler that can report metrics to SignalFx at some
// interval.
func NewScheduler() *Scheduler {
	return &Scheduler{
		Sink:             NewHTTPSink(),
		Timer:            timekeeper.RealTime{},
		ErrorHandler:     DefaultErrorHandler,
		ReportingDelayNs: DefaultReportingDelay.Nanoseconds(),
		callbackMap:      make(map[string]*callbackPair),
	}
}

// Var returns an expvar variable that prints the values of the previously reported datapoints.
func (s *Scheduler) Var() expvar.Var {
	return expvar.Func(func() interface{} {
		s.callbackMutex.Lock()
		defer s.callbackMutex.Unlock()
		return s.previousDatapoints
	})
}

func (s *Scheduler) collectDatapoints() []*datapoint.Datapoint {
	ret := make([]*datapoint.Datapoint, 0, len(s.previousDatapoints))
	now := s.Timer.Now()
	for _, p := range s.callbackMap {
		ret = append(ret, p.getDatapoints(now, s.SendZeroTime)...)
	}
	return ret
}

// AddCallback adds a collector to the default group.
func (s *Scheduler) AddCallback(db Collector) {
	s.AddGroupedCallback("", db)
}

// DefaultDimensions adds a dimension map that are appended to all metrics in the default group.
func (s *Scheduler) DefaultDimensions(dims map[string]string) {
	s.GroupedDefaultDimensions("", dims)
}

// GroupedDefaultDimensions adds default dimensions to a specific group.
func (s *Scheduler) GroupedDefaultDimensions(group string, dims map[string]string) {
	s.callbackMutex.Lock()
	defer s.callbackMutex.Unlock()
	subgroup, exists := s.callbackMap[group]
	if !exists {
		subgroup = &callbackPair{
			callbacks:         make(map[Collector]struct{}, 0),
			defaultDimensions: dims,
		}
		s.callbackMap[group] = subgroup
	}
	subgroup.defaultDimensions = dims
}

// AddGroupedCallback adds a collector to a specific group.
func (s *Scheduler) AddGroupedCallback(group string, db Collector) {
	s.callbackMutex.Lock()
	defer s.callbackMutex.Unlock()
	subgroup, exists := s.callbackMap[group]
	if !exists {
		subgroup = &callbackPair{
			callbacks:         map[Collector]struct{}{db: {}},
			defaultDimensions: map[string]string{},
		}
		s.callbackMap[group] = subgroup
	}
	subgroup.callbacks[db] = struct{}{}
}

// RemoveCallback removes a collector from the default group.
func (s *Scheduler) RemoveCallback(db Collector) {
	s.RemoveGroupedCallback("", db)
}

// RemoveGroupedCallback removes a collector from a specific group.
func (s *Scheduler) RemoveGroupedCallback(group string, db Collector) {
	s.callbackMutex.Lock()
	defer s.callbackMutex.Unlock()
	if g, exists := s.callbackMap[group]; exists {
		delete(g.callbacks, db)
		if len(g.callbacks) == 0 {
			delete(s.callbackMap, group)
		}
	}
}

// ReportOnce will report any metrics saved in this reporter to SignalFx
func (s *Scheduler) ReportOnce(ctx context.Context) error {
	datapoints := func() []*datapoint.Datapoint {
		// Only take the mutex here so that we don't hold it during the HTTP call
		s.callbackMutex.Lock()
		defer s.callbackMutex.Unlock()
		datapoints := s.collectDatapoints()
		s.previousDatapoints = datapoints
		return datapoints
	}()
	return s.Sink.AddDatapoints(ctx, datapoints)
}

// ReportingDelay sets the interval metrics are reported to SignalFx.
func (s *Scheduler) ReportingDelay(delay time.Duration) {
	atomic.StoreInt64(&s.ReportingDelayNs, delay.Nanoseconds())
}

// Schedule will run until either the ErrorHandler returns an error or the context is canceled.  This is intended to
// be run inside a goroutine.
func (s *Scheduler) Schedule(ctx context.Context) error {
	lastReport := s.Timer.Now()
	for {
		reportingDelay := time.Duration(atomic.LoadInt64(&s.ReportingDelayNs))
		wakeupTime := lastReport.Add(reportingDelay)
		now := s.Timer.Now()
		if now.After(wakeupTime) {
			wakeupTime = now.Add(reportingDelay)
			atomic.AddInt64(&s.stats.resetIntervalCounts, 1)
		}
		sleepTime := wakeupTime.Sub(now)

		atomic.AddInt64(&s.stats.scheduledSleepCounts, 1)
		select {
		case <-ctx.Done():
			return errors.Annotate(ctx.Err(), "context closed")
		case <-s.Timer.After(sleepTime):
			lastReport = s.Timer.Now()
			if err := errors.Annotate(s.ReportOnce(ctx), "failed reporting single metric"); err != nil {
				if err2 := errors.Annotate(s.ErrorHandler(err), "error handler returned an error"); err2 != nil {
					return err2
				}
			}
		}
	}
}
