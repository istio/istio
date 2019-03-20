// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package periodic for fortio (from greek for load) is a set of utilities to
// run a given task at a target rate (qps) and gather statistics - for instance
// http requests.
//
// The main executable using the library is fortio but there
// is also ../histogram to use the stats from the command line and ../echosrv
// as a very light http server that can be used to test proxies etc like
// the Istio components.
package periodic // import "fortio.org/fortio/periodic"

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"time"

	"fortio.org/fortio/log"
	"fortio.org/fortio/stats"
	"fortio.org/fortio/version"
)

// DefaultRunnerOptions are the default values for options (do not mutate!).
// This is only useful for initializing flag default values.
// You do not need to use this directly, you can pass a newly created
// RunnerOptions and 0 valued fields will be reset to these defaults.
var DefaultRunnerOptions = RunnerOptions{
	QPS:         8,
	Duration:    5 * time.Second,
	NumThreads:  4,
	Percentiles: []float64{90.0},
	Resolution:  0.001, // milliseconds
}

// Runnable are the function to run periodically.
type Runnable interface {
	Run(tid int)
}

// MakeRunners creates an array of NumThreads identical Runnable instances.
// (for the (rare/test) cases where there is no unique state needed)
func (r *RunnerOptions) MakeRunners(rr Runnable) {
	log.Infof("Making %d clone of %+v", r.NumThreads, rr)
	if len(r.Runners) < r.NumThreads {
		log.Infof("Resizing runners from %d to %d", len(r.Runners), r.NumThreads)
		r.Runners = make([]Runnable, r.NumThreads)
	}
	for i := 0; i < r.NumThreads; i++ {
		r.Runners[i] = rr
	}
}

// ReleaseRunners clear the runners state.
func (r *RunnerOptions) ReleaseRunners() {
	for idx := range r.Runners {
		r.Runners[idx] = nil
	}
}

// Aborter is the object controlling Abort() of the runs.
type Aborter struct {
	sync.Mutex
	StopChan chan struct{}
}

// Abort signals all the go routine of this run to stop.
// Implemented by closing the shared channel. The lock is to make sure
// we close it exactly once to avoid go panic.
func (a *Aborter) Abort() {
	a.Lock()
	if a.StopChan != nil {
		log.LogVf("Closing %v", a.StopChan)
		close(a.StopChan)
		a.StopChan = nil
	}
	a.Unlock()
}

// NewAborter makes a new Aborter and initialize its StopChan.
// The pointer should be shared. The structure is NoCopy.
func NewAborter() *Aborter {
	return &Aborter{StopChan: make(chan struct{}, 1)}
}

// RunnerOptions are the parameters to the PeriodicRunner.
type RunnerOptions struct {
	// Type of run (to be copied into results)
	RunType string
	// Array of objects to run in each thread (use MakeRunners() to clone the same one)
	Runners []Runnable
	// At which (target) rate to run the Runners across NumThreads.
	QPS float64
	// How long to run the test for. Unless Exactly is specified.
	Duration time.Duration
	// Note that this actually maps to gorountines and not actual threads
	// but threads seems like a more familiar name to use for non go users
	// and in a benchmarking context
	NumThreads  int
	Percentiles []float64
	Resolution  float64
	// Where to write the textual version of the results, defaults to stdout
	Out io.Writer
	// Extra data to be copied back to the results (to be saved/JSON serialized)
	Labels string
	// Aborter to interrupt a run. Will be created if not set/left nil. Or you
	// can pass your own. It is very important this is a pointer and not a field
	// as RunnerOptions themselves get copied while the channel and lock must
	// stay unique (per run).
	Stop *Aborter
	// Mode where an exact number of iterations is requested. Default (0) is
	// to not use that mode. If specified Duration is not used.
	Exactly int64
}

// RunnerResults encapsulates the actual QPS observed and duration histogram.
type RunnerResults struct {
	RunType           string
	Labels            string
	StartTime         time.Time
	RequestedQPS      string
	RequestedDuration string // String version of the requested duration or exact count
	ActualQPS         float64
	ActualDuration    time.Duration
	NumThreads        int
	Version           string
	DurationHistogram *stats.HistogramData
	Exactly           int64 // Echo back the requested count
}

// HasRunnerResult is the interface implictly implemented by HTTPRunnerResults
// and GrpcRunnerResults so the common results can ge extracted irrespective
// of the type.
type HasRunnerResult interface {
	Result() *RunnerResults
}

// Result returns the common RunnerResults.
func (r *RunnerResults) Result() *RunnerResults {
	return r
}

// PeriodicRunner let's you exercise the Function at the given QPS and collect
// statistics and histogram about the run.
type PeriodicRunner interface { // nolint: golint
	// Starts the run. Returns actual QPS and Histogram of function durations.
	Run() RunnerResults
	// Returns the options normalized by constructor - do not mutate
	// (where is const when you need it...)
	Options() *RunnerOptions
}

// Unexposed implementation details for PeriodicRunner.
type periodicRunner struct {
	RunnerOptions
}

var (
	gAbortChan       chan os.Signal
	gOutstandingRuns int64
	gAbortMutex      sync.Mutex
)

// Normalize initializes and normalizes the runner options. In particular it sets
// up the channel that can be used to interrupt the run later.
// Once Normalize is called, if Run() is skipped, Abort() must be called to
// cleanup the watchers.
func (r *RunnerOptions) Normalize() {
	if r.QPS == 0 {
		r.QPS = DefaultRunnerOptions.QPS
	} else if r.QPS < 0 {
		log.LogVf("Negative qps %f means max speed mode/no wait between calls", r.QPS)
		r.QPS = -1
	}
	if r.Out == nil {
		r.Out = os.Stdout
	}
	if r.NumThreads == 0 {
		r.NumThreads = DefaultRunnerOptions.NumThreads
	}
	if r.NumThreads < 1 {
		r.NumThreads = 1
	}
	if r.Percentiles == nil {
		r.Percentiles = make([]float64, len(DefaultRunnerOptions.Percentiles))
		copy(r.Percentiles, DefaultRunnerOptions.Percentiles)
	}
	if r.Resolution <= 0 {
		r.Resolution = DefaultRunnerOptions.Resolution
	}
	if r.Duration == 0 {
		r.Duration = DefaultRunnerOptions.Duration
	}
	if r.Runners == nil {
		r.Runners = make([]Runnable, r.NumThreads)
	}
	if r.Stop == nil {
		r.Stop = NewAborter()
		runnerChan := r.Stop.StopChan // need a copy to not race with assignement to nil
		go func() {
			gAbortMutex.Lock()
			gOutstandingRuns++
			n := gOutstandingRuns
			if gAbortChan == nil {
				log.LogVf("WATCHER %d First outstanding run starting, catching signal", n)
				gAbortChan = make(chan os.Signal, 1)
				signal.Notify(gAbortChan, os.Interrupt)
			}
			abortChan := gAbortChan
			gAbortMutex.Unlock()
			log.LogVf("WATCHER %d starting new watcher for signal! chan  g %v r %v (%d)", n, abortChan, runnerChan, runtime.NumGoroutine())
			select {
			case _, ok := <-abortChan:
				log.LogVf("WATCHER %d got interrupt signal! %v", n, ok)
				if ok {
					gAbortMutex.Lock()
					if gAbortChan != nil {
						log.LogVf("WATCHER %d closing %v to notify all", n, gAbortChan)
						close(gAbortChan)
						gAbortChan = nil
					}
					gAbortMutex.Unlock()
				}
				r.Abort()
			case <-runnerChan:
				log.LogVf("WATCHER %d r.Stop readable", n)
				// nothing to do, stop happened
			}
			log.LogVf("WATCHER %d End of go routine", n)
			gAbortMutex.Lock()
			gOutstandingRuns--
			if gOutstandingRuns == 0 {
				log.LogVf("WATCHER %d Last watcher: resetting signal handler", n)
				gAbortChan = nil
				signal.Reset(os.Interrupt)
			} else {
				log.LogVf("WATCHER %d isn't the last one, %d left", n, gOutstandingRuns)
			}
			gAbortMutex.Unlock()
		}()
	}
}

// Abort safely aborts the run by closing the channel and resetting that channel
// to nil under lock so it can be called multiple times and not create panic for
// already closed channel.
func (r *RunnerOptions) Abort() {
	log.LogVf("Abort called for %p %+v", r, r)
	if r.Stop != nil {
		r.Stop.Abort()
	}
}

// internal version, returning the concrete implementation. logical std::move
func newPeriodicRunner(opts *RunnerOptions) *periodicRunner {
	r := &periodicRunner{*opts} // by default just copy the input params
	opts.ReleaseRunners()
	opts.Stop = nil
	r.Normalize()
	return r
}

// NewPeriodicRunner constructs a runner from input parameters/options.
// The options will be moved and normalized to the returned object, do
// not use the original options after this call, call Options() instead.
// Abort() must be called if Run() is not called.
func NewPeriodicRunner(params *RunnerOptions) PeriodicRunner {
	return newPeriodicRunner(params)
}

// Options returns the options pointer.
func (r *periodicRunner) Options() *RunnerOptions {
	return &r.RunnerOptions // sort of returning this here
}

// Run starts the runner.
func (r *periodicRunner) Run() RunnerResults {
	r.Stop.Lock()
	runnerChan := r.Stop.StopChan // need a copy to not race with assignement to nil
	r.Stop.Unlock()
	useQPS := (r.QPS > 0)
	// r.Duration will be 0 if endless flag has been provided. Otherwise it will have the provided duration time.
	hasDuration := (r.Duration > 0)
	// r.Exactly is > 0 if we use Exactly iterations instead of the duration.
	useExactly := (r.Exactly > 0)
	var numCalls int64
	var leftOver int64 // left over from r.Exactly / numThreads
	requestedQPS := "max"
	requestedDuration := "until stop"
	if useQPS {
		requestedQPS = fmt.Sprintf("%.9g", r.QPS)
		if hasDuration || useExactly {
			requestedDuration = fmt.Sprint(r.Duration)
			numCalls = int64(r.QPS * r.Duration.Seconds())
			if useExactly {
				numCalls = r.Exactly
				requestedDuration = fmt.Sprintf("exactly %d calls", numCalls)
			}
			if numCalls < 2 {
				log.Warnf("Increasing the number of calls to the minimum of 2 with 1 thread. total duration will increase")
				numCalls = 2
				r.NumThreads = 1
			}
			if int64(2*r.NumThreads) > numCalls {
				newN := int(numCalls / 2)
				log.Warnf("Lowering number of threads - total call %d -> lowering from %d to %d threads", numCalls, r.NumThreads, newN)
				r.NumThreads = newN
			}
			numCalls /= int64(r.NumThreads)
			totalCalls := numCalls * int64(r.NumThreads)
			if useExactly {
				leftOver = r.Exactly - totalCalls
				if log.Log(log.Warning) {
					// nolint: gas
					_, _ = fmt.Fprintf(r.Out, "Starting at %g qps with %d thread(s) [gomax %d] : exactly %d, %d calls each (total %d + %d)\n",
						r.QPS, r.NumThreads, runtime.GOMAXPROCS(0), r.Exactly, numCalls, totalCalls, leftOver)
				}
			} else {
				if log.Log(log.Warning) {
					// nolint: gas
					_, _ = fmt.Fprintf(r.Out, "Starting at %g qps with %d thread(s) [gomax %d] for %v : %d calls each (total %d)\n",
						r.QPS, r.NumThreads, runtime.GOMAXPROCS(0), r.Duration, numCalls, totalCalls)
				}
			}
		} else {
			// Always print that as we need ^C to interrupt, in that case the user need to notice
			// nolint: gas
			_, _ = fmt.Fprintf(r.Out, "Starting at %g qps with %d thread(s) [gomax %d] until interrupted\n",
				r.QPS, r.NumThreads, runtime.GOMAXPROCS(0))
			numCalls = 0
		}
	} else {
		if !useExactly && !hasDuration {
			// Always log something when waiting for ^C
			// nolint: gas
			_, _ = fmt.Fprintf(r.Out, "Starting at max qps with %d thread(s) [gomax %d] until interrupted\n",
				r.NumThreads, runtime.GOMAXPROCS(0))
		} else {
			if log.Log(log.Warning) {
				// nolint: gas
				_, _ = fmt.Fprintf(r.Out, "Starting at max qps with %d thread(s) [gomax %d] ",
					r.NumThreads, runtime.GOMAXPROCS(0))
			}
			if useExactly {
				requestedDuration = fmt.Sprintf("exactly %d calls", r.Exactly)
				numCalls = r.Exactly / int64(r.NumThreads)
				leftOver = r.Exactly % int64(r.NumThreads)
				if log.Log(log.Warning) {
					// nolint: gas
					_, _ = fmt.Fprintf(r.Out, "for %s (%d per thread + %d)\n", requestedDuration, numCalls, leftOver)
				}
			} else {
				requestedDuration = fmt.Sprint(r.Duration)
				if log.Log(log.Warning) {
					// nolint: gas
					_, _ = fmt.Fprintf(r.Out, "for %s\n", requestedDuration)
				}
			}
		}
	}
	runnersLen := len(r.Runners)
	if runnersLen == 0 {
		log.Fatalf("Empty runners array !")
	}
	if r.NumThreads > runnersLen {
		r.MakeRunners(r.Runners[0])
		log.Warnf("Context array was of %d len, replacing with %d clone of first one", runnersLen, len(r.Runners))
	}
	start := time.Now()
	// Histogram  and stats for Function duration - millisecond precision
	functionDuration := stats.NewHistogram(0, r.Resolution)
	// Histogram and stats for Sleep time (negative offset to capture <0 sleep in their own bucket):
	sleepTime := stats.NewHistogram(-0.001, 0.001)
	if r.NumThreads <= 1 {
		log.LogVf("Running single threaded")
		runOne(0, runnerChan, functionDuration, sleepTime, numCalls+leftOver, start, r)
	} else {
		var wg sync.WaitGroup
		var fDs []*stats.Histogram
		var sDs []*stats.Histogram
		for t := 0; t < r.NumThreads; t++ {
			durP := functionDuration.Clone()
			sleepP := sleepTime.Clone()
			fDs = append(fDs, durP)
			sDs = append(sDs, sleepP)
			wg.Add(1)
			thisNumCalls := numCalls
			if (leftOver > 0) && (t == 0) {
				// The first thread gets to do the additional work
				thisNumCalls += leftOver
			}
			go func(t int, durP *stats.Histogram, sleepP *stats.Histogram) {
				runOne(t, runnerChan, durP, sleepP, thisNumCalls, start, r)
				wg.Done()
			}(t, durP, sleepP)
		}
		wg.Wait()
		for t := 0; t < r.NumThreads; t++ {
			functionDuration.Transfer(fDs[t])
			sleepTime.Transfer(sDs[t])
		}
	}
	elapsed := time.Since(start)
	actualQPS := float64(functionDuration.Count) / elapsed.Seconds()
	if log.Log(log.Warning) {
		// nolint: gas
		_, _ = fmt.Fprintf(r.Out, "Ended after %v : %d calls. qps=%.5g\n", elapsed, functionDuration.Count, actualQPS)
	}
	if useQPS {
		percentNegative := 100. * float64(sleepTime.Hdata[0]) / float64(sleepTime.Count)
		// Somewhat arbitrary percentage of time the sleep was behind so we
		// may want to know more about the distribution of sleep time and warn the
		// user.
		if percentNegative > 5 {
			sleepTime.Print(r.Out, "Aggregated Sleep Time", []float64{50})
			_, _ = fmt.Fprintf(r.Out, "WARNING %.2f%% of sleep were falling behind\n", percentNegative) // nolint: gas
		} else {
			if log.Log(log.Verbose) {
				sleepTime.Print(r.Out, "Aggregated Sleep Time", []float64{50})
			} else if log.Log(log.Warning) {
				sleepTime.Counter.Print(r.Out, "Sleep times")
			}
		}
	}
	actualCount := functionDuration.Count
	if useExactly && actualCount != r.Exactly {
		requestedDuration += fmt.Sprintf(", interrupted after %d", actualCount)
	}
	result := RunnerResults{r.RunType, r.Labels, start, requestedQPS, requestedDuration,
		actualQPS, elapsed, r.NumThreads, version.Short(), functionDuration.Export().CalcPercentiles(r.Percentiles), r.Exactly}
	if log.Log(log.Warning) {
		result.DurationHistogram.Print(r.Out, "Aggregated Function Time")
	} else {
		functionDuration.Counter.Print(r.Out, "Aggregated Function Time")
		for _, p := range result.DurationHistogram.Percentiles {
			_, _ = fmt.Fprintf(r.Out, "# target %g%% %.6g\n", p.Percentile, p.Value) // nolint: gas
		}
	}
	select {
	case <-runnerChan: // nothing
		log.LogVf("RUNNER r.Stop already closed")
	default:
		log.LogVf("RUNNER r.Stop not already closed, closing")
		r.Abort()
	}
	return result
}

// runOne runs in 1 go routine.
func runOne(id int, runnerChan chan struct{},
	funcTimes *stats.Histogram, sleepTimes *stats.Histogram, numCalls int64, start time.Time, r *periodicRunner) {
	var i int64
	endTime := start.Add(r.Duration)
	tIDStr := fmt.Sprintf("T%03d", id)
	perThreadQPS := r.QPS / float64(r.NumThreads)
	useQPS := (perThreadQPS > 0)
	hasDuration := (r.Duration > 0)
	useExactly := (r.Exactly > 0)
	f := r.Runners[id]

MainLoop:
	for {
		fStart := time.Now()
		if !useExactly && (hasDuration && fStart.After(endTime)) {
			if !useQPS {
				// max speed test reached end:
				break
			}
			// QPS mode:
			// Do least 2 iterations, and the last one before bailing because of time
			if (i >= 2) && (i != numCalls-1) {
				log.Warnf("%s warning only did %d out of %d calls before reaching %v", tIDStr, i, numCalls, r.Duration)
				break
			}
		}
		f.Run(id)
		funcTimes.Record(time.Since(fStart).Seconds())
		i++
		// if using QPS / pre calc expected call # mode:
		if useQPS {
			if (useExactly || hasDuration) && i >= numCalls {
				break // expected exit for that mode
			}
			elapsed := time.Since(start)
			var targetElapsedInSec float64
			if hasDuration {
				// This next line is tricky - such as for 2s duration and 1qps there is 1
				// sleep of 2s between the 2 calls and for 3qps in 1sec 2 sleep of 1/2s etc
				targetElapsedInSec = (float64(i) + float64(i)/float64(numCalls-1)) / perThreadQPS
			} else {
				// Calculate the target elapsed when in endless execution
				targetElapsedInSec = float64(i) / perThreadQPS
			}
			targetElapsedDuration := time.Duration(int64(targetElapsedInSec * 1e9))
			sleepDuration := targetElapsedDuration - elapsed
			log.Debugf("%s target next dur %v - sleep %v", tIDStr, targetElapsedDuration, sleepDuration)
			sleepTimes.Record(sleepDuration.Seconds())
			select {
			case <-runnerChan:
				break MainLoop
			case <-time.After(sleepDuration):
				// continue normal execution
			}
		} else { // Not using QPS
			if useExactly && i >= numCalls {
				break
			}
			select {
			case <-runnerChan:
				break MainLoop
			default:
				// continue to the next iteration
			}
		}
	}
	elapsed := time.Since(start)
	actualQPS := float64(i) / elapsed.Seconds()
	log.Infof("%s ended after %v : %d calls. qps=%g", tIDStr, elapsed, i, actualQPS)
	if (numCalls > 0) && log.Log(log.Verbose) {
		funcTimes.Log(tIDStr+" Function duration", []float64{99})
		if log.Log(log.Debug) {
			sleepTimes.Log(tIDStr+" Sleep time", []float64{50})
		} else {
			sleepTimes.Counter.Log(tIDStr + " Sleep time")
		}
	}
}

func formatDate(d *time.Time) string {
	return fmt.Sprintf("%d-%02d-%02d-%02d%02d%02d", d.Year(), d.Month(), d.Day(),
		d.Hour(), d.Minute(), d.Second())
}

// ID Returns an id for the result: 64 bytes YYYY-MM-DD-HHmmSS_{alpha_labels}
// where alpha_labels is the filtered labels with only alphanumeric characters
// and all non alpha num replaced by _; truncated to 64 bytes.
func (r *RunnerResults) ID() string {
	base := formatDate(&r.StartTime)
	if r.Labels == "" {
		return base
	}
	last := '_'
	base += string(last)
	for _, rune := range r.Labels {
		if (rune >= 'a' && rune <= 'z') || (rune >= 'A' && rune <= 'Z') || (rune >= '0' && rune <= '9') {
			last = rune
		} else {
			if last == '_' {
				continue // only 1 _ separator at a time
			}
			last = '_'
		}
		base += string(last)
	}
	if last == '_' {
		base = base[:len(base)-1]
	}
	if len(base) > 64 {
		return base[:64]
	}
	return base
}
