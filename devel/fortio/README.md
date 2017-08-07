# Φορτίο

Φορτίο (fortio) is [Istio](https://istio.io/)'s load testing tool. Fortio runs at a specified query per second (qps) and records an histogram of execution time and calculates percentiles (e.g. p99 ie the response time such as 99% of the requests take less than that number (in seconds, SI unit))

The name fortio comes from greek φορτίο which is load/burden.

## Command line arguments

```
$ fortio
Φορτίο 0.2.0 usage:
	fortio [flags] target
where target is a url (http load tests) or host:port (grpc health test)
and flags are:
  -H value
    	Additional Header(s)
  -c int
    	Number of connections/goroutine/threads (default 4)
  -compression
    	Enable http compression
  -gomaxprocs int
    	Setting for runtime.GOMAXPROCS, <1 doesn't change the default
  -grpc
    	Use GRPC health check
  -http1.0
    	Use http1.0 (instead of http 1.1)
  -httpbufferkb int
    	Size of the buffer (max data size) for the optimized http client in kbytes (default 32)
  -httpccch
    	Check for Connection: Close Header
  -keepalive
    	Keep connection alive (only for fast http 1.1) (default true)
  -logcaller
    	Logs filename and line number of callers to log (default true)
  -loglevel value
    	loglevel, one of [Debug Verbose Info Warning Error Critical Fatal] (default Info)
  -logprefix string
    	Prefix to log lines before logged messages (default "> ")
  -p string
    	List of pXX to calculate (default "50,75,99,99.9")
  -profile string
    	write .cpu and .mem profiles to file
  -qps float
    	Queries Per Seconds or 0 for no wait (default 8)
  -r float
    	Resolution of the histogram lowest buckets in seconds (default 0.001)
  -stdclient
    	Use the slower net/http standard client (works for TLS)
  -t duration
    	How long to run the test (default 5s)
```

## Example output

```
$ fortio http://www.google.com
Fortio running at 8 queries per second, 8->8 procs, for 5s: http://www.google.com
20:27:53 I httprunner.go:75> Starting http test for http://www.google.com with 4 threads at 8.0 qps
Starting at 8 qps with 4 thread(s) [gomax 8] for 5s : 10 calls each (total 40)
20:27:58 I periodic.go:253> T002 ended after 5.089296613s : 10 calls. qps=1.964908072847669
20:27:58 I periodic.go:253> T001 ended after 5.089267291s : 10 calls. qps=1.9649193937375378
20:27:58 I periodic.go:253> T000 ended after 5.091488477s : 10 calls. qps=1.964062188331257
20:27:58 I periodic.go:253> T003 ended after 5.096503315s : 10 calls. qps=1.9621295978692013
Ended after 5.09654226s : 40 calls. qps=7.8485
Sleep times : count 36 avg 0.44925533 +/- 0.06566 min 0.304979917 max 0.510428143 sum 16.1731919
Aggregated Function Time : count 40 avg 0.10259885 +/- 0.06195 min 0.044784609 max 0.246461646 sum 4.10395381
# range, mid point, percentile, count
>= 0.04 < 0.045 , 0.0425 , 2.50, 1
>= 0.045 < 0.05 , 0.0475 , 15.00, 5
>= 0.05 < 0.06 , 0.055 , 37.50, 9
>= 0.06 < 0.07 , 0.065 , 40.00, 1
>= 0.07 < 0.08 , 0.075 , 42.50, 1
>= 0.08 < 0.09 , 0.085 , 57.50, 6
>= 0.09 < 0.1 , 0.095 , 70.00, 5
>= 0.1 < 0.12 , 0.11 , 72.50, 1
>= 0.12 < 0.14 , 0.13 , 77.50, 2
>= 0.14 < 0.16 , 0.15 , 80.00, 1
>= 0.18 < 0.2 , 0.19 , 90.00, 4
>= 0.2 < 0.25 , 0.225 , 100.00, 4
# target 50% 0.085
# target 75% 0.13
# target 99% 0.241815
# target 99.9% 0.245997
Code 200 : 40
Response Header Sizes : count 40 avg 5303.075 +/- 44.45 min 5199 max 5418 sum 212123
Response Body/Total Sizes : count 40 avg 15804.05 +/- 383.6 min 15499 max 17026 sum 632162
All done 40 calls (plus 4 warmup) 102.599 ms avg, 7.8 qps
```

## Implementation details

Fortio is written in the [Go](https://golang.org) language and includes a scalable semi log histogram in [stats.go](stats.go) and a periodic runner engine in [periodic.go](periodic.go) with specializations for [http](httprunner.go) and [grpc](grpcrunner.go).

You can run the histogram code standalone as a command line in [cmd/histogram/](cmd/histogram/), a basic echo http server in [cmd/echosrv/](cmd/echosrv/), a basic GRPC ping server in [cmd/grpcping/](cmd/grpcping/) and the main [cmd/fortio/](cmd/fortio/)

## Another example output

With 5k qps: (includes envoy and mixer in the calls)
```
$ time fortio -qps 5000 -t 60s -c 8 -r 0.0001 -H "Host: perf-cluster" http://benchmark-2:9090/echo
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

Or graphically:

![Chart](https://user-images.githubusercontent.com/3664595/27990803-490a618c-6417-11e7-9773-12e0d051128f.png)
