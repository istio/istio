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
	SetDebug(debug bool)
}

// PeriodicRunner is a class encapsulating running code.
type periodicRunner struct {
	qps        float64
	numThreads int // not yet used
	function   Function
	debug      bool
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
	r.debug = false
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
	fmt.Printf("Starting at %g qps with %d thread(s) for %v : %d calls each (total %d)\n", r.qps, r.numThreads, duration, numCalls, totalCalls)
	start := time.Now()
	if r.numThreads <= 1 {
		if r.debug {
			log.Printf("Running single threaded")
		}
		runOne(0, numCalls, r.function, start, r.qps, r.debug)
	} else {
		threadQPS := r.qps / float64(r.numThreads)
		var wg sync.WaitGroup
		for t := 0; t < r.numThreads; t++ {
			wg.Add(1)
			go func(t int) {
				defer wg.Done()
				runOne(t, numCalls, r.function, start, threadQPS, r.debug)
			}(t)
		}
		wg.Wait()
	}
	elapsed := time.Since(start)
	actualQPS := float64(totalCalls) / elapsed.Seconds()
	fmt.Printf("Ended after %v : %d calls. qps=%g\n", elapsed, totalCalls, actualQPS)
}

func runOne(t int, numCalls int64, f Function, start time.Time, qps float64, debug bool) {
	var i int64
	var cF Counter // stats about function duration
	var cS Counter // stats about sleep time
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
		if debug {
			log.Printf("%s target next dur %v - sleep %v", tIDStr, targetElapsedDuration, sleepDuration)
		}
		cS.Record(sleepDuration.Seconds())
		time.Sleep(sleepDuration)
	}
	actualQPS := float64(numCalls) / elapsed.Seconds()
	log.Printf("%s ended after %v : %d calls. qps=%g\n", tIDStr, elapsed, numCalls, actualQPS)
	cF.Log(tIDStr + " Function duration")
	cS.Log(tIDStr + " Sleep time")
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

// SetDebug turns debuging on/off.
func (r *periodicRunner) SetDebug(debug bool) {
	r.debug = debug
}
