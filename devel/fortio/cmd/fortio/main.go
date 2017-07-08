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

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"

	"istio.io/istio/devel/fortio"
)

type threadStats struct {
	retCodes map[int]int64
	sizes    *fortio.Histogram
}

// Used globally / in test() TODO: change periodic.go to carry caller defined context
var (
	url           string
	stats         []threadStats
	verbosityFlag = flag.Int("v", 0, "Verbosity level (0 is quiet)")
)

// Main call being run at the target QPS
func test(t int) {
	if *verbosityFlag > 1 {
		log.Printf("Calling in %d", t)
	}
	code, body := fortio.FetchURL(url)
	size := len(body)
	if *verbosityFlag > 1 {
		log.Printf("Got in %3d sz %d", code, size)
	}
	stats[t].retCodes[code]++
	stats[t].sizes.Record(float64(size))
}

// -- Support for multiple instances of -H flag on cmd line:
type flagList struct {
}

// Unclear when/why this is called and necessary
func (f *flagList) String() string {
	return ""
}
func (f *flagList) Set(value string) error {
	return fortio.AddAndValidateExtraHeader(value)
}

// -- end of functions for -H support

// Prints usage
func usage() {
	fmt.Fprintf(os.Stderr, "Φορτίο %s usage:\n\n%s [flags] url\n", fortio.Version, os.Args[0]) // nolint(gas)
	flag.PrintDefaults()
	os.Exit(1)
}

func main() {
	var (
		defaults = &fortio.DefaultRunnerOptions
		// Very small default so people just trying with random URLs don't affect the target
		qpsFlag         = flag.Float64("qps", 8.0, "Queries Per Seconds or 0 for no wait")
		numThreadsFlag  = flag.Int("c", defaults.NumThreads, "Number of connections/goroutine/threads")
		durationFlag    = flag.Duration("t", defaults.Duration, "How long to run the test")
		percentilesFlag = flag.String("p", "50,75,99,99.9", "List of pXX to calculate")
		resolutionFlag  = flag.Float64("r", defaults.Resolution, "Resolution of the histogram lowest buckets in seconds")
		headersFlags    flagList
	)
	flag.Var(&headersFlags, "H", "Additional Header(s)")
	flag.Parse()

	verbosity := *verbosityFlag
	fortio.Verbosity = verbosity
	pList, err := fortio.ParsePercentiles(*percentilesFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to extract percentiles from -p: %v\n", err) // nolint(gas)
		usage()
	}
	if len(flag.Args()) != 1 {
		usage()
	}
	url = flag.Arg(0)
	fmt.Printf("Fortio running at %g queries per second for %v: %s\n", *qpsFlag, *durationFlag, url)
	o := fortio.RunnerOptions{
		QPS:         *qpsFlag,
		Function:    test,
		Duration:    *durationFlag,
		NumThreads:  *numThreadsFlag,
		Verbosity:   verbosity,
		Percentiles: pList,
		Resolution:  *resolutionFlag,
	}
	r := fortio.NewPeriodicRunner(&o)
	numThreads := r.Options().NumThreads
	http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost = numThreads + 1
	// 1 Warm up / smoke test:  TODO: warm up all threads/connections
	code, body := fortio.FetchURL(url)
	if code != http.StatusOK {
		fmt.Printf("Aborting because of error %d for %s\n%s\n", code, url, string(body))
		os.Exit(1)
	}
	if verbosity > 0 {
		fmt.Printf("first hit of url %s: status %03d\n%s\n", url, code, string(body))
	}

	total := threadStats{
		retCodes: make(map[int]int64),
		sizes:    fortio.NewHistogram(0, 100),
	}

	stats = make([]threadStats, numThreads)
	for i := 0; i < numThreads; i++ {
		stats[i].sizes = total.sizes.Clone()
		stats[i].retCodes = make(map[int]int64)
	}
	r.Run()
	// Numthreads may have reduced
	numThreads = r.Options().NumThreads
	keys := []int{}
	for i := 0; i < numThreads; i++ {
		// Q: is there some copying each time stats[i] is used?
		for k := range stats[i].retCodes {
			if _, exists := total.retCodes[k]; !exists {
				keys = append(keys, k)
			}
			total.retCodes[k] += stats[i].retCodes[k]
		}
		total.sizes.Transfer(stats[i].sizes)
	}
	sort.Ints(keys)
	for _, k := range keys {
		fmt.Printf("Code %3d : %d\n", k, total.retCodes[k])
	}
	if verbosity > 0 {
		total.sizes.Print(os.Stdout, "Response Body Sizes Histogram", 50)
	} else {
		total.sizes.Counter.Print(os.Stdout, "Response Body Sizes")
	}
}
