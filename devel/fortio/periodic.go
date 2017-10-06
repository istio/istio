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

// Package fortio (from greek for load) is a set of utilities to run a given
// task at a target rate (qps) and gather statistics - for instance http
// requests.
//
// The main executable using the library is cmd/fortio but there
// is also cmd/histogram to use the stats from the command line and cmd/echosrv
// as a very light http server that can be used to test proxies etc like
// the Istio components.
package fortio // import "istio.io/istio/devel/fortio"

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultRunnerOptions are the default values for options (do not mutate!).
// This is only useful for initializing flag default values.
// You do not need to use this directly, you can pass a newly created
// RunnerOptions and 0 valued fields will be reset to these defaults.
var DefaultRunnerOptions = RunnerOptions{
	Duration:    5 * time.Second,
	NumThreads:  4,
	Percentiles: []float64{90.0},
	Resolution:  0.001, // milliseconds
}

// Function to run periodically.
type Function func(tid int)

// RunnerOptions are the parameters to the PeriodicRunner.
type RunnerOptions struct {
	Function Function
	QPS      float64
	Duration time.Duration
	// Note that this actually maps to gorountines and not actual threads
	// but threads seems like a more familiar name to use for non go users
	// and in a benchmarking context
	NumThreads  int
	Percentiles []float64
	Resolution  float64
}

// RunnerResults encapsulates the actual QPS observed and duration histogram.
type RunnerResults struct {
	DurationHistogram *Histogram
	ActualQPS         float64
	ActualDuration    time.Duration
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
type PeriodicRunner interface {
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

// internal version, returning the concrete implementation.
func newPeriodicRunner(opts *RunnerOptions) *periodicRunner {
	r := &periodicRunner{*opts} // by default just copy the input params
	if r.QPS < 0 {
		Infof("Negative qps %f means max speed mode/no wait between calls", r.QPS)
		r.QPS = 0
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
	if r.Duration <= 0 {
		r.Duration = DefaultRunnerOptions.Duration
	}
	return r
}

// NewPeriodicRunner constructs a runner from input parameters/options.
func NewPeriodicRunner(params *RunnerOptions) PeriodicRunner {
	return newPeriodicRunner(params)
}

// Options returns the options pointer.
func (r *periodicRunner) Options() *RunnerOptions {
	return &r.RunnerOptions // sort of returning this here
}

// Run starts the runner.
func (r *periodicRunner) Run() RunnerResults {
	useQPS := (r.QPS > 0)
	var numCalls int64
	if useQPS {
		numCalls = int64(r.QPS * r.Duration.Seconds())
		if numCalls < 2 {
			Warnf("Increasing the number of calls to the minimum of 2 with 1 thread. total duration will increase")
			numCalls = 2
			r.NumThreads = 1
		}
		if int64(2*r.NumThreads) > numCalls {
			r.NumThreads = int(numCalls / 2)
			Warnf("Lowering number of threads - total call %d -> lowering to %d threads", numCalls, r.NumThreads)
		}
		numCalls /= int64(r.NumThreads)
		totalCalls := numCalls * int64(r.NumThreads)
		fmt.Printf("Starting at %g qps with %d thread(s) [gomax %d] for %v : %d calls each (total %d)\n",
			r.QPS, r.NumThreads, runtime.GOMAXPROCS(0), r.Duration, numCalls, totalCalls)
	} else {
		fmt.Printf("Starting at max qps with %d thread(s) [gomax %d] for %v\n",
			r.NumThreads, runtime.GOMAXPROCS(0), r.Duration)
	}
	start := time.Now()
	// Histogram  and stats for Function duration - millisecond precision
	functionDuration := NewHistogram(0, r.Resolution)
	// Histogram and stats for Sleep time (negative offset to capture <0 sleep in their own bucket):
	sleepTime := NewHistogram(-0.001, 0.001)
	if r.NumThreads <= 1 {
		Infof("Running single threaded")
		runOne(0, functionDuration, sleepTime, numCalls, start, r)
	} else {
		var wg sync.WaitGroup
		var fDs []*Histogram
		var sDs []*Histogram
		for t := 0; t < r.NumThreads; t++ {
			durP := functionDuration.Clone()
			sleepP := sleepTime.Clone()
			fDs = append(fDs, durP)
			sDs = append(sDs, sleepP)
			wg.Add(1)
			go func(t int, durP *Histogram, sleepP *Histogram) {
				runOne(t, durP, sleepP, numCalls, start, r)
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
	fmt.Printf("Ended after %v : %d calls. qps=%.5g\n", elapsed, functionDuration.Count, actualQPS)
	if useQPS {
		percentNegative := 100. * float64(sleepTime.hdata[0]) / float64(sleepTime.Count)
		// Somewhat arbitrary percentage of time the sleep was behind so we
		// may want to know more about the distribution of sleep time and warn the
		// user.
		if percentNegative > 5 {
			sleepTime.Print(os.Stdout, "Aggregated Sleep Time", 50)
			fmt.Printf("WARNING %.2f%% of sleep were falling behind\n", percentNegative)
		} else {
			if Log(Verbose) {
				sleepTime.Print(os.Stdout, "Aggregated Sleep Time", 50)
			} else {
				sleepTime.Counter.Print(os.Stdout, "Sleep times")
			}
		}
	}
	functionDuration.Print(os.Stdout, "Aggregated Function Time", r.Percentiles[0])
	for _, p := range r.Percentiles[1:] {
		fmt.Printf("# target %g%% %.6g\n", p, functionDuration.CalcPercentile(p))
	}
	return RunnerResults{functionDuration, actualQPS, elapsed}
}

// runOne runs in 1 go routine.
func runOne(id int, funcTimes *Histogram, sleepTimes *Histogram, numCalls int64, start time.Time, r *periodicRunner) {
	var i int64
	endTime := start.Add(r.Duration)
	tIDStr := fmt.Sprintf("T%03d", id)
	perThreadQPS := r.QPS / float64(r.NumThreads)
	useQPS := (perThreadQPS > 0)
	f := r.Function
	for {
		fStart := time.Now()
		if fStart.After(endTime) {
			if !useQPS {
				// max speed test reached end:
				break
			}
			// QPS mode:
			// Do least 2 iterations, and the last one before bailing because of time
			if (i >= 2) && (i != numCalls-1) {
				Warnf("%s warning only did %d out of %d calls before reaching %v", tIDStr, i, numCalls, r.Duration)
				break
			}
		}
		f(id)
		funcTimes.Record(time.Since(fStart).Seconds())
		i++
		// if using QPS / pre calc expected call # mode:
		if useQPS {
			if i >= numCalls {
				break // expected exit for that mode
			}
			elapsed := time.Since(start)
			// This next line is tricky - such as for 2s duration and 1qps there is 1
			// sleep of 2s between the 2 calls and for 3qps in 1sec 2 sleep of 1/2s etc
			targetElapsedInSec := (float64(i) + float64(i)/float64(numCalls-1)) / perThreadQPS
			targetElapsedDuration := time.Duration(int64(targetElapsedInSec * 1e9))
			sleepDuration := targetElapsedDuration - elapsed
			Debugf("%s target next dur %v - sleep %v", tIDStr, targetElapsedDuration, sleepDuration)
			sleepTimes.Record(sleepDuration.Seconds())
			time.Sleep(sleepDuration)
		}
	}
	elapsed := time.Since(start)
	actualQPS := float64(i) / elapsed.Seconds()
	Infof("%s ended after %v : %d calls. qps=%g", tIDStr, elapsed, i, actualQPS)
	if (numCalls > 0) && Log(Verbose) {
		funcTimes.Log(tIDStr+" Function duration", 99)
		if Log(Debug) {
			sleepTimes.Log(tIDStr+" Sleep time", 50)
		} else {
			sleepTimes.Counter.Log(tIDStr + " Sleep time")
		}
	}
}

// ParsePercentiles extracts the percentiles from string (flag).
func ParsePercentiles(percentiles string) ([]float64, error) {
	percs := strings.Split(percentiles, ",") // will make a size 1 array for empty input!
	res := make([]float64, 0, len(percs))
	for _, pStr := range percs {
		pStr = strings.TrimSpace(pStr)
		if len(pStr) == 0 {
			continue
		}
		p, err := strconv.ParseFloat(pStr, 64)
		if err != nil {
			return res, err
		}
		res = append(res, p)
	}
	if len(res) == 0 {
		return res, errors.New("list can't be empty")
	}
	LogVf("Will use %v for percentiles", res)
	return res, nil
}
