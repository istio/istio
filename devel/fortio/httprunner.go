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
	"net/http"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
)

// Most of the code in this file is the library-fication of code originally
// in cmd/fortio/main.go

// HTTPRunnerResults is the aggregated result of an HTTPRunner.
// Also is the internal type used per thread/goroutine.
type HTTPRunnerResults struct {
	client            Fetcher
	RetCodes          map[int]int64
	Sizes             *Histogram
	HeaderSizes       *Histogram
	DurationHistogram *Histogram
	ActualQPS         float64
}

// Used globally / in TestHttp() TODO: change periodic.go to carry caller defined context
var (
	state []HTTPRunnerResults
)

// TestHTTP http request fetching. Main call being run at the target QPS.
// To be set as the Function in RunnerOptions.
func TestHTTP(t int) {
	Debugf("Calling in %d", t)
	code, body, headerSize := state[t].client.Fetch()
	size := len(body)
	Debugf("Got in %3d hsz %d sz %d", code, headerSize, size)
	state[t].RetCodes[code]++
	state[t].Sizes.Record(float64(size))
	state[t].HeaderSizes.Record(float64(headerSize))
}

// HTTPRunnerOptions includes the base RunnerOptions plus http specific
// options.
type HTTPRunnerOptions struct {
	RunnerOptions
	URL               string
	Compression       bool   // defaults to no compression, only used by std client
	DisableFastClient bool   // defaults to fast client
	HTTP10            bool   // defaults to http1.1
	DisableKeepAlive  bool   // so default is keep alive
	Profiler          string // file to save profiles to. defaults to no profiling
}

// RunHTTPTest runs an http test and returns the aggregated stats.
func RunHTTPTest(o *HTTPRunnerOptions) (*HTTPRunnerResults, error) {
	// TODO 1. use std client automatically when https url
	// TODO 2. lock
	if o.Function == nil {
		o.Function = TestHTTP
	}
	r := NewPeriodicRunner(&o.RunnerOptions)
	numThreads := r.Options().NumThreads
	total := HTTPRunnerResults{
		RetCodes:    make(map[int]int64),
		Sizes:       NewHistogram(0, 100),
		HeaderSizes: NewHistogram(0, 5),
	}
	state = make([]HTTPRunnerResults, numThreads)
	for i := 0; i < numThreads; i++ {
		// Create a client (and transport) and connect once for each 'thread'
		if o.DisableFastClient {
			state[i].client = NewStdClient(o.URL, 1, o.Compression)
		} else {
			if o.HTTP10 {
				state[i].client = NewBasicClient(o.URL, "1.0", !o.DisableKeepAlive)
			} else {
				state[i].client = NewBasicClient(o.URL, "1.1", !o.DisableKeepAlive)
			}
		}
		if state[i].client == nil {
			return nil, fmt.Errorf("unable to create client %d for %s", i, o.URL)
		}
		code, data, headerSize := state[i].client.Fetch()
		if code != http.StatusOK {
			return nil, fmt.Errorf("error %d for %s: %q", code, o.URL, string(data))
		}
		if i == 0 && LogVerbose() {
			LogVf("first hit of url %s: status %03d, headers %d, total %d\n%s\n", o.URL, code, headerSize, len(data), data)
		}
		// Setup the stats for each 'thread'
		state[i].Sizes = total.Sizes.Clone()
		state[i].HeaderSizes = total.HeaderSizes.Clone()
		state[i].RetCodes = make(map[int]int64)
	}

	if o.Profiler != "" {
		fc, err := os.Create(o.Profiler + ".cpu")
		if err != nil {
			Critf("Unable to create .cpu profile: %v", err)
			return nil, err
		}
		pprof.StartCPUProfile(fc) //nolint: gas,errcheck
	}
	total.ActualQPS, total.DurationHistogram = r.Run()
	if o.Profiler != "" {
		pprof.StopCPUProfile()
		fm, err := os.Create(o.Profiler + ".mem")
		if err != nil {
			Critf("Unable to create .mem profile: %v", err)
			return nil, err
		}
		runtime.GC()               // get up-to-date statistics
		pprof.WriteHeapProfile(fm) // nolint:gas,errcheck
		fm.Close()                 // nolint:gas,errcheck
		fmt.Printf("Wrote profile data to %s.{cpu|mem}\n", o.Profiler)
	}
	// Numthreads may have reduced
	numThreads = r.Options().NumThreads
	keys := []int{}
	for i := 0; i < numThreads; i++ {
		// Q: is there some copying each time stats[i] is used?
		for k := range state[i].RetCodes {
			if _, exists := total.RetCodes[k]; !exists {
				keys = append(keys, k)
			}
			total.RetCodes[k] += state[i].RetCodes[k]
		}
		total.Sizes.Transfer(state[i].Sizes)
		total.HeaderSizes.Transfer(state[i].HeaderSizes)
	}
	sort.Ints(keys)
	for _, k := range keys {
		fmt.Printf("Code %3d : %d\n", k, total.RetCodes[k])
	}
	if LogVerbose() {
		total.HeaderSizes.Print(os.Stdout, "Response Header Sizes Histogram", 50)
		total.Sizes.Print(os.Stdout, "Response Body/Total Sizes Histogram", 50)
	} else {
		total.HeaderSizes.Counter.Print(os.Stdout, "Response Header Sizes")
		total.Sizes.Counter.Print(os.Stdout, "Response Body/Total Sizes")
	}
	return &total, nil
}
