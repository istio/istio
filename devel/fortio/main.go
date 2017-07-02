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
	"time"

	"istio.io/istio/devel/fortio/fortioLib"
)

var debugFlag = flag.Bool("debug", false, "Turn verbose debug output")

func foo() {
	if *debugFlag {
		log.Println("Running!")
	}
}

func main() {
	var qpsFlag = flag.Float64("qps", 100.0, "Queries Per Seconds")
	var numThreadsFlag = flag.Int("num-threads", 0, "Number of threads (0 doesn't change internal default)")
	var durationFlag = flag.Duration("t", 10*time.Second, "How long to run the test")
	flag.Parse()
	fmt.Printf("Running at %g queries per second\n", *qpsFlag)
	r := fortio.NewPeriodicRunner(*qpsFlag, foo)
	if *numThreadsFlag != 0 {
		r.SetNumThreads(*numThreadsFlag)
	}
	r.SetDebug(*debugFlag)
	r.Run(*durationFlag)
}
