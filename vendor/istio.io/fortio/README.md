[![Awesome Go](docs/mentioned-badge.svg)](https://github.com/avelino/awesome-go#networking)
[![Go Report Card](https://goreportcard.com/badge/istio.io/fortio)](https://goreportcard.com/report/istio.io/fortio)
[![GoDoc](https://godoc.org/istio.io/fortio?status.svg)](https://godoc.org/istio.io/fortio)
[![codecov](https://codecov.io/gh/istio/fortio/branch/master/graph/badge.svg)](https://codecov.io/gh/istio/fortio)
[![CircleCI](https://circleci.com/gh/istio/fortio.svg?style=svg)](https://circleci.com/gh/istio/fortio)

# Φορτίο
<img src="https://github.com/istio/fortio/blob/master/docs/fortio-logo-color.png" height=141 width=141 align=right>

Φορτίο (fortio) is [Istio](https://istio.io/)'s load testing tool. Fortio runs at a specified query per second (qps) and records an histogram of execution time and calculates percentiles (e.g. p99 ie the response time such as 99% of the requests take less than that number (in seconds, SI unit)). It can run for a set duration, for a fixed number of calls, or until interrupted (at a constant target QPS, or max speed/load per connection/thread).

The name fortio comes from greek φορτίο which is load/burden.

Fortio is a reusable, embeddable go library as well as a command line tool and server process, the server includes a simple web UI and graphical representation of the results (both a single latency graph and a multiple results comparative min, max, avg and percentiles graphs).

## Installation

1. [Install go](https://golang.org/doc/install) (golang 1.8 or later)
2. `go get -u istio.io/fortio`
3. you can now run `fortio` (from your gopath bin/ directory)

Or use docker, for instance:

```shell
docker run -p 8080:8080 -p 8079:8079 istio/fortio server & # For the server
docker run istio/fortio load http://www.google.com/ # For a test run
```

Or download the binary distribution, for instance:

```shell
curl -L https://github.com/istio/fortio/releases/download/0.4.0/fortio-linux_x64-0.4.0.tgz \
 | sudo tar -C / -xvzpf -
```

Once `fortio server` is running, you can visit its web UI at http://localhost:8080/fortio/

You can get a preview of the reporting/graphing UI at https://fortio.istio.io/

## Command line arguments

Fortio can be an http or grpc load generator, gathering statistics using the `load` subcommand, or start simple http and grpc ping servers, as well as a basic web UI, result graphing and https redirector, with the `server` command or issue grpc ping messages using the `grpcping` command. It can also fetch a single URL's for debugging when using the `curl` command (or the `-curl` flag to the load command). You can run just the redirector with `redirect`. Lastly if you saved JSON results (using the web UI or directly from the command line), you can browse and graph those results using the `report` command.

```
$ fortio
Φορτίο 0.6.8 usage:
	fortio command [flags] target
where command is one of: load (load testing), server (starts grpc ping and
http echo/ui/redirect servers), grpcping (grpc client), report (report only UI
server), redirect (redirect only server), or curl (single URL debug).
where target is a url (http load tests) or host:port (grpc health test)
and flags are:
  -H value
    	Additional Header(s)
  -a	Automatically save JSON result with filename based on labels & timestamp
  -allow-initial-errors
    	Allow and don't abort on initial warmup errors
  -c int
    	Number of connections/goroutine/threads (default 4)
  -compression
    	Enable http compression
  -curl
    	Just fetch the content once
  -data-dir string
    	Directory where JSON results are stored/read (default ".")
  -echo-debug-path string
    	http echo server URI for debug, empty turns off that part (more secure)
    	(default "/debug")
  -gomaxprocs int
    	Setting for runtime.GOMAXPROCS, <1 doesn't change the default
  -grpc
    	Use GRPC (health check) for load testing
  -grpc-port string
    	grpc server port. Can take the form of host:port, ip:port or port (default 8079)
  -halfclose
    	When not keepalive, whether to half close the connection (only for fast
    	http)
  -health
    	client mode: use health instead of ping
  -healthservice string
    	which service string to pass to health check
  -http-port string
    	http echo server port. Can take the form of host:port, ip:port or port (default 8080)
  -http1.0
    	Use http1.0 (instead of http 1.1)
  -httpbufferkb int
    	Size of the buffer (max data size) for the optimized http client in kbytes
    	(default 128)
  -httpccch
    	Check for Connection: Close Header
  -httpreqtimeout duration
    	Http request timeout value (default 15s)
  -json string
    	Json output to provided file or '-' for stdout (empty = no json output,
    	unless -a is used)
  -keepalive
    	Keep connection alive (only for fast http 1.1) (default true)
  -labels string
    	Additional config data/labels to add to the resulting JSON, defaults to
    	target URL and hostname
  -logcaller
    	Logs filename and line number of callers to log (default true)
  -loglevel value
    	loglevel, one of [Debug Verbose Info Warning Error Critical Fatal]
      (default Info)
  -logprefix string
    	Prefix to log lines before logged messages (default "> ")
  -n int
    	Run for exactly this number of calls instead of duration. Default (0) is
    	to use duration (-t). Default is 1 when used as grpc ping count.
  -p string
    	List of pXX to calculate (default "50,75,99,99.9")
  -payload string
    	Payload string to send along
  -profile string
    	write .cpu and .mem profiles to file
  -qps float
    	Queries Per Seconds or 0 for no wait/max qps (default 8)
  -quiet
    	Quiet mode: sets the loglevel to Error and reduces the output.
  -r float
    	Resolution of the histogram lowest buckets in seconds (default 0.001)
  -redirect-port int
    	Redirect all incoming traffic to https URL (need ingress to work properly)
    	-1 means off. (default 8081)
  -static-dir string
    	Absolute path to the dir containing the static files dir
  -stdclient
    	Use the slower net/http standard client (works for TLS)
  -sync string
      index.tsv or s3/gcs bucket xml URL to fetch at startup for server modes.
  -t duration
    	How long to run the test or 0 to run until ^C (default 5s)
  -ui-path string
    	http server URI for UI, empty turns off that part (more secure)
    	(default "/fortio/")
```

## Example use and output

* Start the internal servers:
```
$ fortio server &
Https redirector running on :8081
UI starting - visit:
http://localhost:8080/fortio/   (or any host/ip reachable on this server)
Fortio 0.6.8 grpc ping server listening on port :8079
Fortio 0.6.8 echo server listening on port :8080
```

* By default, Fortio's web/echo servers listen on port 8080 on all interfaces.
Use the `-http-port` flag to change this behavior:
```
$ fortio server -http-port 10.10.10.10:8088
UI starting - visit:
http://10.10.10.10:8088/fortio/
Https redirector running on :8081
Fortio 0.6.8 grpc ping server listening on port :8079
Fortio 0.6.8 echo server listening on port 10.10.10.10:8088
```
* Simple grpc ping:
```
$ fortio grpcping localhost
02:29:27 I pingsrv.go:116> Ping RTT 305334 (avg of 342970, 293515, 279517 ns) clock skew -2137
Clock skew histogram usec : count 1 avg -2.137 +/- 0 min -2.137 max -2.137 sum -2.137
# range, mid point, percentile, count
>= -4 < -2 , -3 , 100.00, 1
# target 50% -2.137
RTT histogram usec : count 3 avg 305.334 +/- 27.22 min 279.517 max 342.97 sum 916.002
# range, mid point, percentile, count
>= 250 < 300 , 275 , 66.67, 2
>= 300 < 350 , 325 , 100.00, 1
# target 50% 294.879
```
* The value of `-grpc-port` (default 8079) is used when specifying a hostname
or an IP address in `grpcping`. Add `:port` to the `grpcping` destination to
change this behavior:
```
$ fortio grpcping 10.10.10.100:8078 # Connects to gRPC server 10.10.10.100 listening on port 8078
02:29:27 I pingsrv.go:116> Ping RTT 305334 (avg of 342970, 293515, 279517 ns) clock skew -2137
Clock skew histogram usec : count 1 avg -2.137 +/- 0 min -2.137 max -2.137 sum -2.137
# range, mid point, percentile, count
>= -4 < -2 , -3 , 100.00, 1
# target 50% -2.137
RTT histogram usec : count 3 avg 305.334 +/- 27.22 min 279.517 max 342.97 sum 916.002
# range, mid point, percentile, count
>= 250 < 300 , 275 , 66.67, 2
>= 300 < 350 , 325 , 100.00, 1
# target 50% 294.879
```
* Load (low default qps/threading) test:
```
$ fortio load http://www.google.com
Fortio 0.3.6 running at 8 queries per second, 8->8 procs, for 5s: http://www.google.com
19:10:33 I httprunner.go:84> Starting http test for http://www.google.com with 4 threads at 8.0 qps
Starting at 8 qps with 4 thread(s) [gomax 8] for 5s : 10 calls each (total 40)
19:10:39 I periodic.go:314> T002 ended after 5.056753279s : 10 calls. qps=1.9775534712220633
19:10:39 I periodic.go:314> T001 ended after 5.058085991s : 10 calls. qps=1.9770324224999916
19:10:39 I periodic.go:314> T000 ended after 5.058796046s : 10 calls. qps=1.9767549252963101
19:10:39 I periodic.go:314> T003 ended after 5.059557593s : 10 calls. qps=1.9764573910247019
Ended after 5.059691387s : 40 calls. qps=7.9056
Sleep times : count 36 avg 0.49175757 +/- 0.007217 min 0.463508712 max 0.502087879 sum 17.7032725
Aggregated Function Time : count 40 avg 0.060587641 +/- 0.006564 min 0.052549016 max 0.089893269 sum 2.42350566
# range, mid point, percentile, count
>= 0.052549 < 0.06 , 0.0562745 , 47.50, 19
>= 0.06 < 0.07 , 0.065 , 92.50, 18
>= 0.07 < 0.08 , 0.075 , 97.50, 2
>= 0.08 <= 0.0898933 , 0.0849466 , 100.00, 1
# target 50% 0.0605556
# target 75% 0.0661111
# target 99% 0.085936
# target 99.9% 0.0894975
Code 200 : 40
Response Header Sizes : count 40 avg 690.475 +/- 15.77 min 592 max 693 sum 27619
Response Body/Total Sizes : count 40 avg 12565.2 +/- 301.9 min 12319 max 13665 sum 502608
All done 40 calls (plus 4 warmup) 60.588 ms avg, 7.9 qps
```

* Curl like (single request) mode

```
$ fortio load -curl -H Foo:Bar http://localhost:8080/debug
14:26:26 I http.go:133> Setting regular extra header Foo: Bar
HTTP/1.1 200 OK
Content-Type: text/plain; charset=UTF-8
Date: Mon, 08 Jan 2018 22:26:26 GMT
Content-Length: 230

Φορτίο version 0.6.8 echo debug server up for 39s on ldemailly-macbookpro - request from [::1]:65055

GET /debug HTTP/1.1

headers:

Host: localhost:8080
User-Agent: istio/fortio-0.6.8
Foo: Bar

body:

```

* Report only UI

If you have json files saved from running the full UI, you can serve just the reports:

```
$ fortio report
Browse only UI starting - visit:
http://localhost:8080/
Https redirector running on :8081
```

## Server URLs and features

Fortio `server` - has the following feature - http listening on 8080 (all paths and ports are configurable through flags above):
- A simple echo server which will echo back posted data (for any path not mentioned below).
For instance `curl -d abcdef http://localhost:8080/` returns `abcdef` back. It supports the following optional query argument parameters:

| Parameter | Usage, example |
| ----------|----------------|
| delay     | duration to delay the response by. Can be a single value or a coma separated list of probabilities, e.g `delay=150us:10,2ms:5,0.5s:1` for 10% of chance of a 150 us delay, 5% of a 2ms delay and 1% of a 1/2 second delay |
| status    | http status to return instead of 200. Can be a single value or a coma separated list of probabilities, e.g `status=404:10,503:5,429:1` for 10% of chance of a 404 status, 5% of a 503 status and 1% of a 429 status |
- `/debug` will echo back the request in plain text for human debugging.
- `/fortio/` A UI to
  - Run/Trigger tests and graph the results.
  - A UI to browse saved results and single graph or multi graph them (comparative graph of min,avg, median, p75, p99, p99.9 and max).
  - Proxy/fetch other URLs
  - `/fortio/data/index.tsv` an tab separated value file conforming to Google cloud storage [URL list data transfer format](https://cloud.google.com/storage/transfer/create-url-list) so you can export/backup local results to the cloud.
  - Download/sync peer to peer JSON results files from other Fortio servers (using their `index.tsv` URLs)
  - Download/sync from an Amazon S3 or Google Cloud compatible bucket listings [XML URLs](https://docs.aws.amazon.com/AmazonS3/latest/API/RESTBucketGET.html)

The `report` mode is a readonly subset of the above directly on `/`.

## Implementation details

Fortio is written in the [Go](https://golang.org) language and includes a scalable semi log histogram in [stats.go](stats/stats.go) and a periodic runner engine in [periodic.go](periodic/periodic.go) with specializations for [http](http/httprunner.go) and [grpc](fortiogrpc/grpcrunner.go).
The [http/](http/) package includes a very high performance specialized http 1.1 client.
You may find fortio's [logger](log/logger.go) useful as well.

You can run the histogram code standalone as a command line in [histogram/](histogram/), a basic echo http server in [echosrv/](echosrv/), or both the http echo and GRPC ping server through `fortio server`, the fortio command line interface lives in this top level directory [fortio_main.go](fortio_main.go)

## Another example output

With 5k qps: (includes envoy and mixer in the calls)
```
$ time fortio load -qps 5000 -t 60s -c 8 -r 0.0001 -H "Host: perf-cluster" http://benchmark-2:9090/echo
2017/07/09 02:31:05 Will be setting special Host header to perf-cluster
Fortio running at 5000 queries per second for 1m0s: http://benchmark-2:9090/echo
Starting at 5000 qps with 8 thread(s) [gomax 4] for 1m0s : 37500 calls each (total 300000)
2017/07/09 02:32:05 T004 ended after 1m0.000907812s : 37500 calls. qps=624.9905437680746
2017/07/09 02:32:05 T000 ended after 1m0.000922222s : 37500 calls. qps=624.9903936684861
2017/07/09 02:32:05 T005 ended after 1m0.00094454s : 37500 calls. qps=624.9901611965524
2017/07/09 02:32:05 T006 ended after 1m0.000944816s : 37500 calls. qps=624.9901583216429
2017/07/09 02:32:05 T001 ended after 1m0.00102094s : 37500 calls. qps=624.9893653892883
2017/07/09 02:32:05 T007 ended after 1m0.001096292s : 37500 calls. qps=624.9885805003184
2017/07/09 02:32:05 T003 ended after 1m0.001045342s : 37500 calls. qps=624.9891112105419
2017/07/09 02:32:05 T002 ended after 1m0.001044416s : 37500 calls. qps=624.9891208560392
Ended after 1m0.00112695s : 300000 calls. qps=4999.9
Aggregated Sleep Time : count 299992 avg 8.8889218e-05 +/- 0.002326 min -0.03490402 max 0.001006041 sum 26.6660543
# range, mid point, percentile, count
< 0 , 0 , 8.58, 25726
>= 0 < 0.001 , 0.0005 , 100.00, 274265
>= 0.001 < 0.002 , 0.0015 , 100.00, 1
# target 50% 0.000453102
WARNING 8.58% of sleep were falling behind
Aggregated Function Time : count 300000 avg 0.00094608764 +/- 0.0007901 min 0.000510522 max 0.029267604 sum 283.826292
# range, mid point, percentile, count
>= 0.0005 < 0.0006 , 0.00055 , 0.15, 456
>= 0.0006 < 0.0007 , 0.00065 , 3.25, 9295
>= 0.0007 < 0.0008 , 0.00075 , 24.23, 62926
>= 0.0008 < 0.0009 , 0.00085 , 62.73, 115519
>= 0.0009 < 0.001 , 0.00095 , 85.68, 68854
>= 0.001 < 0.0011 , 0.00105 , 93.11, 22293
>= 0.0011 < 0.0012 , 0.00115 , 95.38, 6792
>= 0.0012 < 0.0014 , 0.0013 , 97.18, 5404
>= 0.0014 < 0.0016 , 0.0015 , 97.94, 2275
>= 0.0016 < 0.0018 , 0.0017 , 98.34, 1198
>= 0.0018 < 0.002 , 0.0019 , 98.60, 775
>= 0.002 < 0.0025 , 0.00225 , 98.98, 1161
>= 0.0025 < 0.003 , 0.00275 , 99.21, 671
>= 0.003 < 0.0035 , 0.00325 , 99.36, 449
>= 0.0035 < 0.004 , 0.00375 , 99.47, 351
>= 0.004 < 0.0045 , 0.00425 , 99.57, 290
>= 0.0045 < 0.005 , 0.00475 , 99.66, 280
>= 0.005 < 0.006 , 0.0055 , 99.79, 380
>= 0.006 < 0.007 , 0.0065 , 99.82, 92
>= 0.007 < 0.008 , 0.0075 , 99.83, 15
>= 0.008 < 0.009 , 0.0085 , 99.83, 5
>= 0.009 < 0.01 , 0.0095 , 99.83, 1
>= 0.01 < 0.012 , 0.011 , 99.83, 8
>= 0.012 < 0.014 , 0.013 , 99.84, 35
>= 0.014 < 0.016 , 0.015 , 99.92, 231
>= 0.016 < 0.018 , 0.017 , 99.94, 65
>= 0.018 < 0.02 , 0.019 , 99.95, 26
>= 0.02 < 0.025 , 0.0225 , 100.00, 139
>= 0.025 < 0.03 , 0.0275 , 100.00, 14
# target 50% 0.000866935
# target 75% 0.000953452
# target 99% 0.00253875
# target 99.9% 0.0155152
Code 200 : 300000
Response Body Sizes : count 300000 avg 0 +/- 0 min 0 max 0 sum 0
```

Or you can get the data in [JSON format](https://github.com/istio/fortio/wiki/Sample-JSON-output) (using `-json result.json`)

### Web/Graphical UI

Or graphically (through the [http://localhost:8080/fortio/](http://localhost:8080/fortio/) web UI):

Simple form/UI:

Sample requests with responses delayed by 250us and 0.5% of 503 and 1.5% of 429 simulated http errors.

![Web UI form screenshot](https://user-images.githubusercontent.com/3664595/34192808-1983be12-e505-11e7-9c16-2ee9f101f2ce.png)

Run result:

![Graphical result](https://user-images.githubusercontent.com/3664595/34192806-16f1740a-e505-11e7-9534-3e703222c1d3.png)

```
Code 200 : 2939 (98.0 %)
Code 429 : 47 (1.6 %)
Code 503 : 14 (0.5 %)
```
## Contributing
Contributions whether through issues, documentation, bug fixes, or new features
are most welcome !

Please also see [Contributing to Istio](https://github.com/istio/community/blob/master/CONTRIBUTING.md#contributing-to-istio)
and [Getting started contributing to Fortio](https://github.com/istio/fortio/wiki/FAQ#how-do-i-get-started-contributing-to-fortio) in the FAQ.

And make sure to go format and run those commands successfully before sending your PRs:
```
make test
make lint
make webtest
```

When modifying Javascript, check with [standard](https://github.com/standard/standard):
```
standard --fix ui/static/js/fortio_chart.js
```

## See also

Our wiki and the [Fortio FAQ](https://github.com/istio/fortio/wiki/FAQ) (including for instance differences between `fortio` and `wrk` or `httpbin`)
