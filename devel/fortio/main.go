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

	"istio.io/istio/devel/fortio/fortioLib"
)

var debugFlag = flag.Bool("debug", false, "Turn verbose debug output")
var url string

type threadStats struct {
	retCodes map[int]int64
	sizes    fortio.Counter
}

var stats []threadStats

func test(t int) {
	if *debugFlag {
		log.Printf("Calling in %d", t)
	}
	code, body := fortio.FetchURL(url)
	size := len(body)
	log.Printf("Got in %3d sz %d", code, size)
	stats[t].retCodes[code]++
	stats[t].sizes.Record(float64(size))
}

func main() {
	var qpsFlag = flag.Float64("qps", 100.0, "Queries Per Seconds")
	var numThreadsFlag = flag.Int("num-threads", 0, "Number of threads (0 doesn't change internal default)")
	var durationFlag = flag.Duration("t", 10*time.Second, "How long to run the test")
	flag.Parse()
	if len(flag.Args()) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] url\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}
	url = flag.Arg(0)
	// 1 Warm up / smoke test:
	code, body := fortio.FetchURL(url)
	if code != http.StatusOK {
		fmt.Printf("Aborting because of error %d for %s\n%s", code, url, string(body))
		os.Exit(1)
	}
	fmt.Printf("Running at %g queries per second for %v: %s\n", *qpsFlag, *durationFlag, url)
	r := fortio.NewPeriodicRunner(*qpsFlag, test)
	if *numThreadsFlag != 0 {
		r.SetNumThreads(*numThreadsFlag)
	}
	r.SetDebug(*debugFlag)
	stats = make([]threadStats, r.GetNumThreads())
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
		stats[i].sizes.Printf(os.Stdout, tid+" Sizes")
	}
}
