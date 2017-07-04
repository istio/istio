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
	"time"

	fortio "istio.io/istio/devel/fortio/fortioLib"
)

var verbosityFlag = flag.Int("v", 0, "Verbosity level (0 is quiet)")
var url string

type threadStats struct {
	retCodes map[int]int64
	sizes    fortio.Counter
}

var stats []threadStats

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

func main() {
	// Very small default so people just trying with random URLs don't affect the target
	var qpsFlag = flag.Float64("qps", 8.0, "Queries Per Seconds")
	var numThreadsFlag = flag.Int("c", 0, "Number of connections/goroutine/threads (0 doesn't change internal default)")
	var durationFlag = flag.Duration("t", 5*time.Second, "How long to run the test")
	flag.Parse()
	verbose := *verbosityFlag
	fortio.Verbose = verbose
	if len(flag.Args()) != 1 {
		fmt.Fprintf(os.Stderr, "Φορτίο %s usage:\n\n%s [flags] url\n", fortio.Version, os.Args[0]) // nolint(gas)
		flag.PrintDefaults()
		os.Exit(1)
	}
	url = flag.Arg(0)
	fmt.Printf("Running at %g queries per second for %v: %s\n", *qpsFlag, *durationFlag, url)
	r := fortio.NewPeriodicRunner(*qpsFlag, test)
	if *numThreadsFlag != 0 {
		r.SetNumThreads(*numThreadsFlag)
	}
	r.SetDebugLevel(verbose)
	numThreads := r.GetNumThreads()
	http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost = numThreads + 1
	// 1 Warm up / smoke test:  TODO: warm up all threads/connections
	code, body := fortio.FetchURL(url)
	if code != http.StatusOK {
		fmt.Printf("Aborting because of error %d for %s\n%s\n", code, url, string(body))
		os.Exit(1)
	}
	if verbose > 0 {
		fmt.Printf("first hit of url %s: status %03d\n%s\n", url, code, string(body))
	}

	stats = make([]threadStats, numThreads)
	for i := 0; i < r.GetNumThreads(); i++ {
		stats[i].retCodes = make(map[int]int64)
	}
	r.Run(*durationFlag)
	// Numthreads may have reduced
	for i := 0; i < r.GetNumThreads(); i++ {
		tid := fmt.Sprintf("T%02d", i)
		keys := []int{}
		// Q: is there some copying each time stats[i] is used?
		for k := range stats[i].retCodes {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		for _, k := range keys {
			fmt.Printf("%s Code %3d : %d\n", tid, k, stats[i].retCodes[k])
		}
		stats[i].sizes.FPrint(os.Stdout, tid+" Sizes")
	}
}
