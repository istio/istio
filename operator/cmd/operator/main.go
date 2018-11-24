// Copyright 2018 Istio Authors
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
	"os"
	"time"
	"flag"
	"runtime/pprof"

	"istio.io/istio/operator/cmd/operator/cmd"
	"istio.io/istio/operator/cmd/shared"
)

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
var memprofile = flag.String("memprofile", "", "write memory profile to this file")

func main() {
        f, err := os.Create("cpuprof.prof")
        if err != nil {
//         log.Fatal(err)
        }
        pprof.StartCPUProfile(f)
        defer pprof.StopCPUProfile()

	rootCmd := cmd.GetRootCmd(os.Args[1:], shared.Printf, shared.Fatalf)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
	time.Sleep(100*time.Second)

	f, _ = os.Create("memprof.prof")
	pprof.WriteHeapProfile(f)
        f.Close()

}
