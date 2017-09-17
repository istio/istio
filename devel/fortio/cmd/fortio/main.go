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
	"os"
	"runtime"

	"istio.io/istio/devel/fortio"
	"istio.io/istio/devel/fortio/fortiogrpc"
)

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
func usage(msgs ...interface{}) {
	fmt.Fprintf(os.Stderr, "Φορτίο %s usage:\n\t%s command [flags] target\n%s\n%s\n%s\n",
		fortio.Version,
		os.Args[0],
		"where command is one of: load (load testing), server (starts grpc ping and http echo servers), grpcping (grpc client)",
		"where target is a url (http load tests) or host:port (grpc health test)",
		"and flags are:") // nolint(gas)
	flag.PrintDefaults()
	fmt.Fprint(os.Stderr, msgs...)
	os.Stderr.WriteString("\n") // nolint(gas)
	os.Exit(1)
}

var (
	defaults = &fortio.DefaultRunnerOptions
	// Very small default so people just trying with random URLs don't affect the target
	qpsFlag         = flag.Float64("qps", 8.0, "Queries Per Seconds or 0 for no wait")
	numThreadsFlag  = flag.Int("c", defaults.NumThreads, "Number of connections/goroutine/threads")
	durationFlag    = flag.Duration("t", defaults.Duration, "How long to run the test")
	percentilesFlag = flag.String("p", "50,75,99,99.9", "List of pXX to calculate")
	resolutionFlag  = flag.Float64("r", defaults.Resolution, "Resolution of the histogram lowest buckets in seconds")
	compressionFlag = flag.Bool("compression", false, "Enable http compression")
	goMaxProcsFlag  = flag.Int("gomaxprocs", 0, "Setting for runtime.GOMAXPROCS, <1 doesn't change the default")
	profileFlag     = flag.String("profile", "", "write .cpu and .mem profiles to file")
	keepAliveFlag   = flag.Bool("keepalive", true, "Keep connection alive (only for fast http 1.1)")
	stdClientFlag   = flag.Bool("stdclient", false, "Use the slower net/http standard client (works for TLS)")
	http10Flag      = flag.Bool("http1.0", false, "Use http1.0 (instead of http 1.1)")
	grpcFlag        = flag.Bool("grpc", false, "Use GRPC (health check) for load testing")
	echoPortFlag    = flag.Int("http-port", 8080, "http echo server port")
	grpcPortFlag    = flag.Int("grpc-port", 8079, "grpc port")
	echoDbgPathFlag = flag.String("echo-debug-path", "/debug", "http echo server URI for debug, empty turns off that part (more secure)")

	headersFlags flagList
	percList     []float64
	err          error
)

func main() {
	flag.Var(&headersFlags, "H", "Additional Header(s)")
	flag.IntVar(&fortio.BufferSizeKb, "httpbufferkb", fortio.BufferSizeKb, "Size of the buffer (max data size) for the optimized http client in kbytes")
	flag.BoolVar(&fortio.CheckConnectionClosedHeader, "httpccch", fortio.CheckConnectionClosedHeader, "Check for Connection: Close Header")
	if len(os.Args) < 2 {
		usage("Error: need at least 1 command parameter")
	}
	command := os.Args[1]
	os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
	flag.Parse()
	percList, err = fortio.ParsePercentiles(*percentilesFlag)
	if err != nil {
		usage("Unable to extract percentiles from -p: ", err)
	}

	switch command {
	case "load":
		fortioLoad()
	case "server":
		go fortio.EchoServer(*echoPortFlag, *echoDbgPathFlag)
		pingServer(*grpcPortFlag)
	case "grpcping":
		grpcClient()
	default:
		usage("Error: unknown command ", command)
	}

}

func fortioLoad() {
	if len(flag.Args()) != 1 {
		usage("Error: fortio load needs a url or destination")
	}
	url := flag.Arg(0)
	prevGoMaxProcs := runtime.GOMAXPROCS(*goMaxProcsFlag)
	fmt.Printf("Fortio running at %g queries per second, %d->%d procs, for %v: %s\n",
		*qpsFlag, prevGoMaxProcs, runtime.GOMAXPROCS(0), *durationFlag, url)
	ro := fortio.RunnerOptions{
		QPS:         *qpsFlag,
		Duration:    *durationFlag,
		NumThreads:  *numThreadsFlag,
		Percentiles: percList,
		Resolution:  *resolutionFlag,
	}
	var res fortio.HasRunnerResult
	if *grpcFlag {
		o := fortiogrpc.GRPCRunnerOptions{
			RunnerOptions: ro,
			Destination:   url,
		}
		res, err = fortiogrpc.RunGRPCTest(&o)
	} else {
		o := fortio.HTTPRunnerOptions{
			RunnerOptions:     ro,
			URL:               url,
			HTTP10:            *http10Flag,
			DisableFastClient: *stdClientFlag,
			DisableKeepAlive:  !*keepAliveFlag,
			Profiler:          *profileFlag,
			Compression:       *compressionFlag,
		}
		res, err = fortio.RunHTTPTest(&o)
	}
	if err != nil {
		fmt.Printf("Aborting because %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("All done %d calls (plus %d warmup) %.3f ms avg, %.1f qps\n",
		res.Result().DurationHistogram.Count,
		*numThreadsFlag,
		1000.*res.Result().DurationHistogram.Avg(),
		res.Result().ActualQPS)
}
