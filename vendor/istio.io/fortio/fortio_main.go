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
	"io"
	"os"
	"path"
	"runtime"
	"strings"
	"time"

	"istio.io/fortio/bincommon"
	"istio.io/fortio/fnet"

	"istio.io/fortio/fgrpc"
	"istio.io/fortio/fhttp"
	"istio.io/fortio/log"
	"istio.io/fortio/periodic"
	"istio.io/fortio/stats"
	"istio.io/fortio/ui"
	"istio.io/fortio/version"
)

// -- Support for multiple proxies (-P) flags on cmd line:
type proxiesFlagList struct {
}

func (f *proxiesFlagList) String() string {
	return ""
}
func (f *proxiesFlagList) Set(value string) error {
	proxies = append(proxies, value)
	return nil
}

// -- end of functions for -P support

// Usage to a writer
func usage(w io.Writer, msgs ...interface{}) {
	fmt.Fprintf(w, "Φορτίο %s usage:\n\t%s command [flags] target\n%s\n%s\n%s\n%s\n",
		version.Short(),
		os.Args[0],
		"where command is one of: load (load testing), server (starts grpc ping and",
		"http echo/ui/redirect/proxy servers), grpcping (grpc client), report (report",
		"only UI server), redirect (redirect only server), or curl (single URL debug).",
		"where target is a url (http load tests) or host:port (grpc health test).")
	bincommon.FlagsUsage(w, msgs...)
}

// Prints usage and error messages with StdErr writer
func usageErr(msgs ...interface{}) {
	usage(os.Stderr, msgs...)
	os.Exit(1)
}

// Attention: every flag that is common to http client goes to bincommon/
// for sharing between fortio and fcurl binaries

const (
	disabled = "disabled"
)

var (
	defaults = &periodic.DefaultRunnerOptions
	// Very small default so people just trying with random URLs don't affect the target
	qpsFlag           = flag.Float64("qps", defaults.QPS, "Queries Per Seconds or 0 for no wait/max qps")
	numThreadsFlag    = flag.Int("c", defaults.NumThreads, "Number of connections/goroutine/threads")
	durationFlag      = flag.Duration("t", defaults.Duration, "How long to run the test or 0 to run until ^C")
	percentilesFlag   = flag.String("p", "50,75,90,99,99.9", "List of pXX to calculate")
	resolutionFlag    = flag.Float64("r", defaults.Resolution, "Resolution of the histogram lowest buckets in seconds")
	goMaxProcsFlag    = flag.Int("gomaxprocs", 0, "Setting for runtime.GOMAXPROCS, <1 doesn't change the default")
	profileFlag       = flag.String("profile", "", "write .cpu and .mem profiles to `file`")
	grpcFlag          = flag.Bool("grpc", false, "Use GRPC (health check by default, add -ping for ping) for load testing")
	httpsInsecureFlag = flag.Bool("https-insecure", false, "Long form of the -k flag")
	certFlag          = flag.String("cert", "", "`Path` to the certificate file to be used for GRPC server TLS")
	keyFlag           = flag.String("key", "", "`Path` to the key file used for GRPC server TLS")
	caCertFlag        = flag.String("cacert", "",
		"`Path` to a custom CA certificate file to be used for the GRPC client TLS, "+
			"if empty, use https:// prefix for standard internet CAs TLS")
	echoPortFlag = flag.String("http-port", "8080",
		"http echo server port. Can be in the form of host:port, ip:port, port or /unix/domain/path.")
	grpcPortFlag = flag.String("grpc-port", fnet.DefaultGRPCPort,
		"grpc server port. Can be in the form of host:port, ip:port or port or /unix/domain/path or \""+disabled+
			"\" to not start the grpc server.")
	echoDbgPathFlag = flag.String("echo-debug-path", "/debug",
		"http echo server URI for debug, empty turns off that part (more secure)")
	jsonFlag = flag.String("json", "",
		"Json output to provided file `path` or '-' for stdout (empty = no json output, unless -a is used)")
	uiPathFlag = flag.String("ui-path", "/fortio/", "http server URI for UI, empty turns off that part (more secure)")
	curlFlag   = flag.Bool("curl", false, "Just fetch the content once")
	labelsFlag = flag.String("labels", "",
		"Additional config data/labels to add to the resulting JSON, defaults to target URL and hostname")
	staticDirFlag = flag.String("static-dir", "", "Absolute `path` to the dir containing the static files dir")
	dataDirFlag   = flag.String("data-dir", defaultDataDir, "`Directory` where JSON results are stored/read")
	proxiesFlags  proxiesFlagList
	proxies       = make([]string, 0)

	defaultDataDir = "."

	allowInitialErrorsFlag = flag.Bool("allow-initial-errors", false, "Allow and don't abort on initial warmup errors")
	abortOnFlag            = flag.Int("abort-on", 0, "Http code that if encountered aborts the run. e.g. 503 or -1 for socket errors.")
	autoSaveFlag           = flag.Bool("a", false, "Automatically save JSON result with filename based on labels & timestamp")
	redirectFlag           = flag.String("redirect-port", "8081", "Redirect all incoming traffic to https URL"+
		" (need ingress to work properly). Can be in the form of host:port, ip:port, port or \""+disabled+"\" to disable the feature.")
	exactlyFlag = flag.Int64("n", 0,
		"Run for exactly this number of calls instead of duration. Default (0) is to use duration (-t). "+
			"Default is 1 when used as grpc ping count.")
	syncFlag         = flag.String("sync", "", "index.tsv or s3/gcs bucket xml URL to fetch at startup for server modes.")
	syncIntervalFlag = flag.Duration("sync-interval", 0, "Refresh the url every given interval (default, no refresh)")

	baseURLFlag = flag.String("base-url", "",
		"base URL used as prefix for data/index.tsv generation. (when empty, the url from the first request is used)")
	newMaxPayloadSizeKb = flag.Int("maxpayloadsizekb", fnet.MaxPayloadSize/1024,
		"MaxPayloadSize is the maximum size of payload to be generated by the EchoHandler size= argument. In Kbytes.")

	// GRPC related flags
	// To get most debugging/tracing:
	// GODEBUG="http2debug=2" GRPC_GO_LOG_VERBOSITY_LEVEL=99 GRPC_GO_LOG_SEVERITY_LEVEL=info fortio grpcping -loglevel debug
	doHealthFlag   = flag.Bool("health", false, "grpc ping client mode: use health instead of ping")
	doPingLoadFlag = flag.Bool("ping", false, "grpc load test: use ping instead of health")
	healthSvcFlag  = flag.String("healthservice", "", "which service string to pass to health check")
	pingDelayFlag  = flag.Duration("grpc-ping-delay", 0, "grpc ping delay in response")
	streamsFlag    = flag.Int("s", 1, "Number of streams per grpc connection")

	maxStreamsFlag = flag.Uint("grpc-max-streams", 0,
		"MaxConcurrentStreams for the grpc server. Default (0) is to leave the option unset.")
)

func main() {
	flag.Var(&proxiesFlags, "P", "Proxies to run, e.g -P \"localport1 dest_host1:dest_port1\" -P \"[::1]:0 www.google.com:443\" ...")
	bincommon.SharedMain(usage)
	if len(os.Args) < 2 {
		usageErr("Error: need at least 1 command parameter")
	}
	command := os.Args[1]
	os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
	flag.Parse()
	fnet.ChangeMaxPayloadSize(*newMaxPayloadSizeKb * 1024)
	if *bincommon.QuietFlag {
		log.SetLogLevelQuiet(log.Error)
	}
	percList, err := stats.ParsePercentiles(*percentilesFlag)
	if err != nil {
		usageErr("Unable to extract percentiles from -p: ", err)
	}
	baseURL := strings.Trim(*baseURLFlag, " \t\n\r/") // remove trailing slash and other whitespace
	sync := strings.TrimSpace(*syncFlag)
	if sync != "" {
		if !ui.Sync(os.Stdout, sync, *dataDirFlag) {
			os.Exit(1)
		}
	}
	isServer := false
	switch command {
	case "curl":
		fortioLoad(true, nil)
	case "load":
		fortioLoad(*curlFlag, percList)
	case "redirect":
		isServer = true
		fhttp.RedirectToHTTPS(*redirectFlag)
	case "report":
		isServer = true
		if *redirectFlag != disabled {
			fhttp.RedirectToHTTPS(*redirectFlag)
		}
		if !ui.Report(baseURL, *echoPortFlag, *staticDirFlag, *dataDirFlag) {
			os.Exit(1) // error already logged
		}
	case "server":
		isServer = true
		if *grpcPortFlag != disabled {
			fgrpc.PingServer(*grpcPortFlag, *certFlag, *keyFlag, fgrpc.DefaultHealthServiceName, uint32(*maxStreamsFlag))
		}
		if *redirectFlag != disabled {
			fhttp.RedirectToHTTPS(*redirectFlag)
		}
		if !ui.Serve(baseURL, *echoPortFlag, *echoDbgPathFlag, *uiPathFlag, *staticDirFlag, *dataDirFlag, percList) {
			os.Exit(1) // error already logged
		}
		for _, proxy := range proxies {
			s := strings.SplitN(proxy, " ", 2)
			if len(s) != 2 {
				log.Errf("Invalid syntax for proxy \"%s\", should be \"localAddr destHost:destPort\"", proxy)
			}
			fnet.ProxyToDestination(s[0], s[1])
		}
	case "grpcping":
		grpcClient()
	default:
		usageErr("Error: unknown command ", command)
	}
	if isServer {
		// To get a start time log/timestamp in the logs
		log.Infof("All fortio %s servers started!", version.Long())
		d := *syncIntervalFlag
		if sync != "" && d > 0 {
			log.Infof("Will re-sync data dir every %s", d)
			ticker := time.NewTicker(d)
			defer ticker.Stop()
			for range ticker.C {
				ui.Sync(os.Stdout, sync, *dataDirFlag)
			}
		} else {
			select {}
		}
	}
}

func fortioLoad(justCurl bool, percList []float64) {
	if len(flag.Args()) != 1 {
		usageErr("Error: fortio load/curl needs a url or destination")
	}
	httpOpts := bincommon.SharedHTTPOptions()
	if *httpsInsecureFlag {
		httpOpts.Insecure = true
	}
	if justCurl {
		bincommon.FetchURL(httpOpts)
		return
	}
	url := httpOpts.URL
	prevGoMaxProcs := runtime.GOMAXPROCS(*goMaxProcsFlag)
	out := os.Stderr
	qps := *qpsFlag // TODO possibly use translated <=0 to "max" from results/options normalization in periodic/
	fmt.Fprintf(out, "Fortio %s running at %g queries per second, %d->%d procs",
		version.Short(), qps, prevGoMaxProcs, runtime.GOMAXPROCS(0))
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
	var err error
	if *grpcFlag {
		o := fgrpc.GRPCRunnerOptions{
			RunnerOptions:      ro,
			Destination:        url,
			CACert:             *caCertFlag,
			Service:            *healthSvcFlag,
			Streams:            *streamsFlag,
			AllowInitialErrors: *allowInitialErrorsFlag,
			Payload:            httpOpts.PayloadString(),
			Delay:              *pingDelayFlag,
			UsePing:            *doPingLoadFlag,
			UnixDomainSocket:   httpOpts.UnixDomainSocket,
		}
		res, err = fgrpc.RunGRPCTest(&o)
	} else {
		o := fhttp.HTTPRunnerOptions{
			HTTPOptions:        *httpOpts,
			RunnerOptions:      ro,
			Profiler:           *profileFlag,
			AllowInitialErrors: *allowInitialErrorsFlag,
			AbortOn:            *abortOnFlag,
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
		var j []byte
		j, err = json.MarshalIndent(res, "", "  ")
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

func grpcClient() {
	if len(flag.Args()) != 1 {
		usageErr("Error: fortio grpcping needs host argument in the form of host, host:port or ip:port")
	}
	host := flag.Arg(0)
	count := int(*exactlyFlag)
	if count <= 0 {
		count = 1
	}
	cert := *caCertFlag
	var err error
	if *doHealthFlag {
		_, err = fgrpc.GrpcHealthCheck(host, cert, *healthSvcFlag, count)
	} else {
		httpOpts := bincommon.SharedHTTPOptions()
		_, err = fgrpc.PingClientCall(host, cert, count, httpOpts.PayloadString(), *pingDelayFlag)
	}
	if err != nil {
		// already logged
		os.Exit(1)
	}
}
