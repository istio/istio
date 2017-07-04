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

package fortio

import (
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"
)

// Function to run periodically.
type Function func(tid int)

// IPeriodicRunner is the public interface to the periodic runner.
type IPeriodicRunner interface {
	Run(duration time.Duration)
	SetNumThreads(int)
	GetNumThreads() int
	SetDebugLevel(level int)
}

// PeriodicRunner is a class encapsulating running code.
type periodicRunner struct {
	qps        float64
	numThreads int // not yet used
	function   Function
	verbose    int
}

// internal version, returning the concrete class
func newPeriodicRunner(qps float64, function Function) *periodicRunner {
	r := new(periodicRunner)
	if qps <= 0 {
		log.Printf("Normalizing bad qps %f to 1", qps)
		qps = 1
	}
	r.qps = qps
	r.numThreads = 10 // default
	r.function = function
	r.verbose = 0
	return r
}

// NewPeriodicRunner constructs a runner for a given qps.
func NewPeriodicRunner(qps float64, function Function) IPeriodicRunner {
	return newPeriodicRunner(qps, function)
}

// Start starts the runner.
func (r *periodicRunner) Run(duration time.Duration) {
	var numCalls int64 = int64(r.qps * duration.Seconds())
	if numCalls < 2 {
		log.Print("Increasing the number of calls to the minimum of 2 with 1 thread. total duration will increase")
		numCalls = 2
		r.numThreads = 1
	}
	if int64(2*r.numThreads) > numCalls {
		r.numThreads = int(numCalls / 2)
		log.Printf("Lowering number of threads - total call %d -> lowering to %d threads", numCalls, r.numThreads)
	}
	numCalls /= int64(r.numThreads)
	totalCalls := numCalls * int64(r.numThreads)
	fmt.Printf("Starting at %g qps with %d thread(s) [gomax %d] for %v : %d calls each (total %d)\n",
		r.qps, r.numThreads, runtime.GOMAXPROCS(0), duration, numCalls, totalCalls)
	start := time.Now()
	if r.numThreads <= 1 {
		if r.verbose > 0 {
			log.Printf("Running single threaded")
		}
		runOne(0, numCalls, r.function, start, r.qps, r.verbose)
	} else {
		threadQPS := r.qps / float64(r.numThreads)
		var wg sync.WaitGroup
		for t := 0; t < r.numThreads; t++ {
			wg.Add(1)
			go func(t int) {
				defer wg.Done()
				runOne(t, numCalls, r.function, start, threadQPS, r.verbose)
			}(t)
		}
		wg.Wait()
	}
	elapsed := time.Since(start)
	actualQPS := float64(totalCalls) / elapsed.Seconds()
	fmt.Printf("Ended after %v : %d calls. qps=%.5g\n", elapsed, totalCalls, actualQPS)
}

func runOne(t int, numCalls int64, f Function, start time.Time, qps float64, verbose int) {
	var i int64
	// Histogram  and stats for Function duration - millisecond precision
	cF := NewHistogram(0, 0.001)
	// Histogram and stats for Sleep time (negative offset to capture <0 sleep in their own bucket):
	cS := NewHistogram(-0.001, 0.001)
	var elapsed time.Duration
	tIDStr := fmt.Sprintf("T%03d", t)
	for i < numCalls {
		fStart := time.Now()
		f(t)
		cF.Record(time.Since(fStart).Seconds())
		elapsed = time.Since(start)
		// next time
		i++
		if i >= numCalls {
			break
		}
		// This next line is tricky - such as for 2s duration and 1qps there is 1
		// sleep of 2s between the 2 calls and for 3qps in 1sec 2 sleep of 1/2s etc
		targetElapsedInSec := (float64(i) + float64(i)/float64(numCalls-1)) / qps
		targetElapsedDuration := time.Duration(int64(targetElapsedInSec * 1e9))
		sleepDuration := targetElapsedDuration - elapsed
		if verbose > 3 {
			log.Printf("%s target next dur %v - sleep %v", tIDStr, targetElapsedDuration, sleepDuration)
		}
		cS.Record(sleepDuration.Seconds())
		time.Sleep(sleepDuration)
	}
	actualQPS := float64(numCalls) / elapsed.Seconds()
	log.Printf("%s ended after %v : %d calls. qps=%g", tIDStr, elapsed, numCalls, actualQPS)
	cF.Log(tIDStr+" Function duration", 99)
	percentNegative := 100. * float64(cS.hdata[0]) / float64(cS.Count)
	if percentNegative > .5 {
		log.Printf("%s WARNING %.2f%% of sleep were falling behind ", tIDStr, percentNegative)
	}
	if verbose > 0 {
		cS.Log(tIDStr+" Sleep time", 50)
	} else {
		cS.Counter.Log(tIDStr + " Sleep time")
	}
}

// SetNumThreads changes the thread count.
func (r *periodicRunner) SetNumThreads(numThreads int) {
	if numThreads < 1 {
		log.Printf("Normalizing bad numThreads %d to 1", numThreads)
		numThreads = 1
	}
	r.numThreads = numThreads
}

// GetNumThreads returns the thread count.
func (r *periodicRunner) GetNumThreads() int {
	return r.numThreads
}

// SetDebugLevel sets the level of debugging/verbosity.
func (r *periodicRunner) SetDebugLevel(level int) {
	r.verbose = level
}
