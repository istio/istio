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

// Do not add any external dependencies we want to keep fortio minimal.

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path"
	"runtime"
	"strings"

	"istio.io/fortio/fgrpc"
	"istio.io/fortio/fhttp"
	"istio.io/fortio/log"
	"istio.io/fortio/periodic"
	"istio.io/fortio/stats"
	"istio.io/fortio/ui"
)

var httpOpts fhttp.HTTPOptions

// -- Support for multiple instances of -H flag on cmd line:
type flagList struct {
}

// Unclear when/why this is called and necessary
func (f *flagList) String() string {
	return ""
}
func (f *flagList) Set(value string) error {
	return httpOpts.AddAndValidateExtraHeader(value)
}

// -- end of functions for -H support

// Prints usage
func usage(msgs ...interface{}) {
	// nolint: gas
	fmt.Fprintf(os.Stderr, "Φορτίο %s usage:\n\t%s command [flags] target\n%s\n%s\n%s\n%s\n%s\n",
		periodic.Version,
		os.Args[0],
		"where command is one of: load (load testing), server (starts grpc ping and",
		"http echo/ui/redirect servers), grpcping (grpc client), report (report only UI",
		"server), redirect (redirect only server), or curl (single URL debug).",
		"where target is a url (http load tests) or host:port (grpc health test)",
		"and flags are:")
	flag.PrintDefaults()
	fmt.Fprint(os.Stderr, msgs...) // nolint: gas
	os.Stderr.WriteString("\n")    // nolint: gas, errcheck
	os.Exit(1)
}

var (
	defaults = &periodic.DefaultRunnerOptions
	// Very small default so people just trying with random URLs don't affect the target
	qpsFlag            = flag.Float64("qps", defaults.QPS, "Queries Per Seconds or 0 for no wait/max qps")
	numThreadsFlag     = flag.Int("c", defaults.NumThreads, "Number of connections/goroutine/threads")
	durationFlag       = flag.Duration("t", defaults.Duration, "How long to run the test or 0 to run until ^C")
	percentilesFlag    = flag.String("p", "50,75,99,99.9", "List of pXX to calculate")
	resolutionFlag     = flag.Float64("r", defaults.Resolution, "Resolution of the histogram lowest buckets in seconds")
	compressionFlag    = flag.Bool("compression", false, "Enable http compression")
	goMaxProcsFlag     = flag.Int("gomaxprocs", 0, "Setting for runtime.GOMAXPROCS, <1 doesn't change the default")
	profileFlag        = flag.String("profile", "", "write .cpu and .mem profiles to file")
	keepAliveFlag      = flag.Bool("keepalive", true, "Keep connection alive (only for fast http 1.1)")
	halfCloseFlag      = flag.Bool("halfclose", false, "When not keepalive, whether to half close the connection (only for fast http)")
	httpReqTimeoutFlag = flag.Duration("httpreqtimeout", fhttp.HTTPReqTimeOutDefaultValue, "Http request timeout value")
	stdClientFlag      = flag.Bool("stdclient", false, "Use the slower net/http standard client (works for TLS)")
	http10Flag         = flag.Bool("http1.0", false, "Use http1.0 (instead of http 1.1)")
	grpcFlag           = flag.Bool("grpc", false, "Use GRPC (health check) for load testing")
	echoPortFlag       = flag.String("http-port", "8080", "http echo server port. Can be in the form of host:port, ip:port or port.")
	grpcPortFlag       = flag.String("grpc-port", fgrpc.DefaultGRPCPort,
		"grpc server port. Can be in the form of host:port, ip:port or port.")
	echoDbgPathFlag = flag.String("echo-debug-path", "/debug",
		"http echo server URI for debug, empty turns off that part (more secure)")
	jsonFlag = flag.String("json", "",
		"Json output to provided file or '-' for stdout (empty = no json output, unless -a is used)")
	uiPathFlag = flag.String("ui-path", "/fortio/", "http server URI for UI, empty turns off that part (more secure)")
	curlFlag   = flag.Bool("curl", false, "Just fetch the content once")
	labelsFlag = flag.String("labels", "",
		"Additional config data/labels to add to the resulting JSON, defaults to target URL and hostname")
	staticDirFlag  = flag.String("static-dir", "", "Absolute path to the dir containing the static files dir")
	dataDirFlag    = flag.String("data-dir", defaultDataDir, "Directory where JSON results are stored/read")
	headersFlags   flagList
	percList       []float64
	err            error
	defaultDataDir = "."

	allowInitialErrorsFlag = flag.Bool("allow-initial-errors", false, "Allow and don't abort on initial warmup errors")
	autoSaveFlag           = flag.Bool("a", false, "Automatically save JSON result with filename based on labels & timestamp")
	redirectFlag           = flag.Int("redirect-port", 8081,
		"Redirect all incoming traffic to https URL (need ingress to work properly). -1 means off.")
	exactlyFlag = flag.Int64("n", 0,
		"Run for exactly this number of calls instead of duration. Default (0) is to use duration (-t). "+
			"Default is 1 when used as grpc ping count.")
	quietFlag   = flag.Bool("quiet", false, "Quiet mode: sets the loglevel to Error and reduces the output.")
	syncFlag    = flag.String("sync", "", "index.tsv or s3/gcs bucket xml URL to fetch at startup for server modes.")
	baseURLFlag = flag.String("base-url", "",
		"base URL used as prefix for data/index.tsv generation. (when empty, the url from the first request is used)")
)

func main() {
	flag.Var(&headersFlags, "H", "Additional Header(s)")
	flag.IntVar(&fhttp.BufferSizeKb, "httpbufferkb", fhttp.BufferSizeKb,
		"Size of the buffer (max data size) for the optimized http client in kbytes")
	flag.BoolVar(&fhttp.CheckConnectionClosedHeader, "httpccch", fhttp.CheckConnectionClosedHeader,
		"Check for Connection: Close Header")
	// Special case so `fortio -version` and `--version` and `version` and ... work
	if len(os.Args) == 2 && strings.Contains(os.Args[1], "version") {
		fmt.Println(periodic.Version)
		os.Exit(0)
	}
	if len(os.Args) < 2 {
		usage("Error: need at least 1 command parameter")
	}
	command := os.Args[1]
	os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
	flag.Parse()
	if *quietFlag {
		log.SetLogLevelQuiet(log.Error)
	}
	percList, err = stats.ParsePercentiles(*percentilesFlag)
	if err != nil {
		usage("Unable to extract percentiles from -p: ", err)
	}
	baseURL := strings.Trim(*baseURLFlag, " \t\n\r/") // remove trailing slash and other whitespace
	sync := strings.TrimSpace(*syncFlag)
	if sync != "" {
		if !ui.Sync(os.Stdout, sync, *dataDirFlag) {
			os.Exit(1)
		}
	}

	switch command {
	case "curl":
		fortioLoad(true)
	case "load":
		fortioLoad(*curlFlag)
	case "redirect":
		ui.RedirectToHTTPS(*redirectFlag)
	case "report":
		if *redirectFlag >= 0 {
			go ui.RedirectToHTTPS(*redirectFlag)
		}
		ui.Report(baseURL, *echoPortFlag, *staticDirFlag, *dataDirFlag)
	case "server":
		if *redirectFlag >= 0 {
			go ui.RedirectToHTTPS(*redirectFlag)
		}
		go ui.Serve(baseURL, *echoPortFlag, *echoDbgPathFlag, *uiPathFlag, *staticDirFlag, *dataDirFlag)
		pingServer(*grpcPortFlag)
	case "grpcping":
		grpcClient()
	default:
		usage("Error: unknown command ", command)
	}
}

func fetchURL(o *fhttp.HTTPOptions) {
	// keepAlive could be just false when making 1 fetch but it helps debugging
	// the http client when making a single request if using the flags
	client := fhttp.NewClient(o)
	if client == nil {
		return // error logged already
	}
	code, data, header := client.Fetch()
	log.LogVf("Fetch result code %d, data len %d, headerlen %d", code, len(data), header)
	os.Stdout.Write(data) //nolint: errcheck
	if code != http.StatusOK {
		log.Errf("Error status %d : %s", code, fhttp.DebugSummary(data, 512))
		os.Exit(1)
	}
}

func fortioLoad(justCurl bool) {
	if len(flag.Args()) != 1 {
		usage("Error: fortio load/curl needs a url or destination")
	}
	url := strings.TrimLeft(flag.Arg(0), " \t\r\n")
	httpOpts.URL = url
	httpOpts.HTTP10 = *http10Flag
	httpOpts.DisableFastClient = *stdClientFlag
	httpOpts.DisableKeepAlive = !*keepAliveFlag
	httpOpts.AllowHalfClose = *halfCloseFlag
	httpOpts.Compression = *compressionFlag
	httpOpts.HTTPReqTimeOut = *httpReqTimeoutFlag
	if justCurl {
		fetchURL(&httpOpts)
		return
	}
	prevGoMaxProcs := runtime.GOMAXPROCS(*goMaxProcsFlag)
	out := os.Stderr
	qps := *qpsFlag // TODO possibly use translated <=0 to "max" from results/options normalization in periodic/
	fmt.Fprintf(out, "Fortio %s running at %g queries per second, %d->%d procs",
		periodic.Version, qps, prevGoMaxProcs, runtime.GOMAXPROCS(0))
	if *exactlyFlag > 0 {
		fmt.Fprintf(out, ", for %d calls: %s\n", *exactlyFlag, url)
	} else {
		if *durationFlag <= 0 {
			// Infinite mode is determined by having a negative duration value
			*durationFlag = -1
			fmt.Fprintf(out, ", until interrupted: %s\n", url)
		} else {
			fmt.Fprintf(out, ", for %v: %s\n", *durationFlag, url)
		}
	}
	if qps <= 0 {
		qps = -1 // 0==unitialized struct == default duration, -1 (0 for flag) is max
	}
	labels := *labelsFlag
	if labels == "" {
		hname, _ := os.Hostname()
		shortURL := url
		for _, p := range []string{"https://", "http://"} {
			if strings.HasPrefix(url, p) {
				shortURL = url[len(p):]
				break
			}
		}
		labels = shortURL + " , " + strings.SplitN(hname, ".", 2)[0]
		log.LogVf("Generated Labels: %s", labels)
	}
	ro := periodic.RunnerOptions{
		QPS:         qps,
		Duration:    *durationFlag,
		NumThreads:  *numThreadsFlag,
		Percentiles: percList,
		Resolution:  *resolutionFlag,
		Out:         out,
		Labels:      labels,
		Exactly:     *exactlyFlag,
	}
	var res periodic.HasRunnerResult
	if *grpcFlag {
		o := fgrpc.GRPCRunnerOptions{
			RunnerOptions: ro,
			Destination:   url,
		}
		res, err = fgrpc.RunGRPCTest(&o)
	} else {
		o := fhttp.HTTPRunnerOptions{
			HTTPOptions:        httpOpts,
			RunnerOptions:      ro,
			Profiler:           *profileFlag,
			AllowInitialErrors: *allowInitialErrorsFlag,
		}
		res, err = fhttp.RunHTTPTest(&o)
	}
	if err != nil {
		fmt.Fprintf(out, "Aborting because %v\n", err)
		os.Exit(1)
	}
	rr := res.Result()
	warmup := *numThreadsFlag
	if ro.Exactly > 0 {
		warmup = 0
	}
	fmt.Fprintf(out, "All done %d calls (plus %d warmup) %.3f ms avg, %.1f qps\n",
		rr.DurationHistogram.Count,
		warmup,
		1000.*rr.DurationHistogram.Avg,
		rr.ActualQPS)
	jsonFileName := *jsonFlag
	if *autoSaveFlag || len(jsonFileName) > 0 {
		j, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			log.Fatalf("Unable to json serialize result: %v", err)
		}
		var f *os.File
		if jsonFileName == "-" {
			f = os.Stdout
			jsonFileName = "stdout"
		} else {
			if len(jsonFileName) == 0 {
				jsonFileName = path.Join(*dataDirFlag, rr.ID()+".json")
			}
			f, err = os.Create(jsonFileName)
			if err != nil {
				log.Fatalf("Unable to create %s: %v", jsonFileName, err)
			}
		}
		n, err := f.Write(append(j, '\n'))
		if err != nil {
			log.Fatalf("Unable to write json to %s: %v", jsonFileName, err)
		}
		if f != os.Stdout {
			err := f.Close()
			if err != nil {
				log.Fatalf("Close error for %s: %v", jsonFileName, err)
			}
		}
		fmt.Fprintf(out, "Successfully wrote %d bytes of Json data to %s\n", n, jsonFileName)
	}
}
