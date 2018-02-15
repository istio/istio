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

package fhttp

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"

	"istio.io/fortio/log"
	"istio.io/fortio/periodic"
	"istio.io/fortio/stats"
)

// Most of the code in this file is the library-fication of code originally
// in cmd/fortio/main.go

// HTTPRunnerResults is the aggregated result of an HTTPRunner.
// Also is the internal type used per thread/goroutine.
type HTTPRunnerResults struct {
	periodic.RunnerResults
	client   Fetcher
	RetCodes map[int]int64
	// internal type/data
	sizes       *stats.Histogram
	headerSizes *stats.Histogram
	// exported result
	Sizes       *stats.HistogramData
	HeaderSizes *stats.HistogramData
	URL         string
}

// Run tests http request fetching. Main call being run at the target QPS.
// To be set as the Function in RunnerOptions.
func (httpstate *HTTPRunnerResults) Run(t int) {
	log.Debugf("Calling in %d", t)
	code, body, headerSize := httpstate.client.Fetch()
	size := len(body)
	log.Debugf("Got in %3d hsz %d sz %d", code, headerSize, size)
	httpstate.RetCodes[code]++
	httpstate.sizes.Record(float64(size))
	httpstate.headerSizes.Record(float64(headerSize))
}

// HTTPRunnerOptions includes the base RunnerOptions plus http specific
// options.
type HTTPRunnerOptions struct {
	periodic.RunnerOptions
	HTTPOptions               // Need to call Init() to initialize
	Profiler           string // file to save profiles to. defaults to no profiling
	AllowInitialErrors bool   // whether initial errors don't cause an abort
}

// RunHTTPTest runs an http test and returns the aggregated stats.
func RunHTTPTest(o *HTTPRunnerOptions) (*HTTPRunnerResults, error) {
	// TODO 1. use std client automatically when https url
	log.Infof("Starting http test for %s with %d threads at %.1f qps", o.URL, o.NumThreads, o.QPS)
	r := periodic.NewPeriodicRunner(&o.RunnerOptions)
	defer r.Options().Abort()
	numThreads := r.Options().NumThreads
	o.HTTPOptions.Init(o.URL)
	out := r.Options().Out // Important as the default value is set from nil to stdout inside NewPeriodicRunner
	total := HTTPRunnerResults{
		RetCodes:    make(map[int]int64),
		sizes:       stats.NewHistogram(0, 100),
		headerSizes: stats.NewHistogram(0, 5),
		URL:         o.URL,
	}
	httpstate := make([]HTTPRunnerResults, numThreads)
	for i := 0; i < numThreads; i++ {
		r.Options().Runners[i] = &httpstate[i]
		// Create a client (and transport) and connect once for each 'thread'
		httpstate[i].client = NewClient(&o.HTTPOptions)
		if httpstate[i].client == nil {
			return nil, fmt.Errorf("unable to create client %d for %s", i, o.URL)
		}
		if o.Exactly <= 0 {
			code, data, headerSize := httpstate[i].client.Fetch()
			if !o.AllowInitialErrors && code != http.StatusOK {
				return nil, fmt.Errorf("error %d for %s: %q", code, o.URL, string(data))
			}
			if i == 0 && log.LogVerbose() {
				log.LogVf("first hit of url %s: status %03d, headers %d, total %d\n%s\n", o.URL, code, headerSize, len(data), data)
			}
		}
		// Setup the stats for each 'thread'
		httpstate[i].sizes = total.sizes.Clone()
		httpstate[i].headerSizes = total.headerSizes.Clone()
		httpstate[i].RetCodes = make(map[int]int64)
	}

	if o.Profiler != "" {
		fc, err := os.Create(o.Profiler + ".cpu")
		if err != nil {
			log.Critf("Unable to create .cpu profile: %v", err)
			return nil, err
		}
		pprof.StartCPUProfile(fc) //nolint: gas,errcheck
	}
	total.RunnerResults = r.Run()
	if o.Profiler != "" {
		pprof.StopCPUProfile()
		fm, err := os.Create(o.Profiler + ".mem")
		if err != nil {
			log.Critf("Unable to create .mem profile: %v", err)
			return nil, err
		}
		runtime.GC()               // get up-to-date statistics
		pprof.WriteHeapProfile(fm) // nolint:gas,errcheck
		fm.Close()                 // nolint:gas,errcheck
		fmt.Fprintf(out, "Wrote profile data to %s.{cpu|mem}\n", o.Profiler)
	}
	// Numthreads may have reduced
	numThreads = r.Options().NumThreads
	keys := []int{}
	for i := 0; i < numThreads; i++ {
		// Q: is there some copying each time stats[i] is used?
		for k := range httpstate[i].RetCodes {
			if _, exists := total.RetCodes[k]; !exists {
				keys = append(keys, k)
			}
			total.RetCodes[k] += httpstate[i].RetCodes[k]
		}
		total.sizes.Transfer(httpstate[i].sizes)
		total.headerSizes.Transfer(httpstate[i].headerSizes)
	}
	sort.Ints(keys)
	totalCount := float64(total.DurationHistogram.Count)
	for _, k := range keys {
		fmt.Fprintf(out, "Code %3d : %d (%.1f %%)\n", k, total.RetCodes[k], 100.*float64(total.RetCodes[k])/totalCount)
	}
	total.HeaderSizes = total.headerSizes.Export()
	total.Sizes = total.sizes.Export()
	if log.LogVerbose() {
		total.HeaderSizes.Print(out, "Response Header Sizes Histogram")
		total.Sizes.Print(out, "Response Body/Total Sizes Histogram")
	} else if log.Log(log.Warning) {
		total.headerSizes.Counter.Print(out, "Response Header Sizes")
		total.sizes.Counter.Print(out, "Response Body/Total Sizes")
	}
	return &total, nil
}
