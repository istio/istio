//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package stress

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc/grpclog"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/sink"
)

func runStressTest(tb testing.TB, o options) {
	tb.Helper()

	d, err := newDriver(o)
	if err != nil {
		tb.Fatalf("error creating driver: %v", err)
	}
	defer d.Close()
	d.run(tb)
}

func TestFullState(t *testing.T) {
	o := defaultOptions()
	runStressTest(t, o)
}

func TestServerSupportedIncrementalOnly(t *testing.T) {
	o := defaultOptions()
	o.serverIncSupported = true
	runStressTest(t, o)
}

func TestServerSupportedIncremental50PercentClients(t *testing.T) {
	o := defaultOptions()
	o.serverIncSupported = true
	o.clientIncPercentage = 0.5
	runStressTest(t, o)
}

func TestServerSupportedIncremental100PercentClients(t *testing.T) {
	o := defaultOptions()
	o.serverIncSupported = true
	o.clientIncPercentage = 1
	runStressTest(t, o)
}

func TestFullStateSlowClients(t *testing.T) {
	o := defaultOptions()
	o.serverIncSupported = true
	o.clientIncPercentage = 1
	o.minApplyDelay = 50 * time.Millisecond
	o.maxApplyDelay = 100 * time.Millisecond
	runStressTest(t, o)
}

// NOTE: these benchmarks don't provide meaningful ns/ops numbers in the
// traditional sense. These are intended to be used with the -cpuprofile and -memprofile
// options along with `go tool pprof` to analyze resource usage in various cases.

func BenchmarkFullState(b *testing.B) {
	o := defaultOptions()
	o.iterations = b.N
	o.numClients = 50
	runStressTest(b, o)
}

func BenchmarkServerSupportedIncrementalOnly(b *testing.B) {
	o := defaultOptions()
	o.iterations = b.N
	o.numClients = 50
	o.serverIncSupported = true
	runStressTest(b, o)
}

func BenchmarkServerSupportedIncremental50PercentClients(b *testing.B) {
	o := defaultOptions()
	o.iterations = b.N
	o.numClients = 50
	o.serverIncSupported = true
	o.clientIncPercentage = 0.5
	runStressTest(b, o)
}

func BenchmarkServerSupportedIncremental100PercentClients(b *testing.B) {
	o := defaultOptions()
	o.iterations = b.N
	o.numClients = 50
	o.serverIncSupported = true
	o.clientIncPercentage = 1
	runStressTest(b, o)
}

func benchmarkUnknownCollection(clients int, b *testing.B) {
	o := defaultOptions()
	o.setupFn = func(d *driver) {
		d.clientOpts.CollectionOptions = sink.CollectionOptionsFromSlice(generateCollectionNames(*numCollections, 1))
	}
	o.iterations = b.N
	o.numClients = clients
	runStressTest(b, o)
}

func BenchmarkUnknownCollection_1Clients(b *testing.B) {
	benchmarkUnknownCollection(1, b)
}

func BenchmarkUnknownCollection_10Clients(b *testing.B) {
	benchmarkUnknownCollection(10, b)
}

func BenchmarkUnknownCollection_100Clients(b *testing.B) {
	benchmarkUnknownCollection(100, b)
}

func TestMain(m *testing.M) {
	flag.Parse()

	// reduce logging from grpc library
	var (
		infoW    = ioutil.Discard
		warningW = ioutil.Discard
		errorW   = os.Stderr
	)
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(infoW, warningW, errorW))

	// reduce logging from MCP library
	o := log.DefaultOptions()
	o.SetOutputLevel("mcp", log.NoneLevel)
	o.SetOutputLevel("default", log.NoneLevel)
	log.Configure(o)

	fmt.Println("Generating common dataset ...")
	initDataset()
	fmt.Println("Finished generating common dataset")

	// call flag.Parse() here if TestMain uses flags
	os.Exit(m.Run())
}
