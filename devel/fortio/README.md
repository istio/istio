# Φορτίο

Φορτίο (fortio) is [Istio](https://istio.io/)'s load testing tool. Fortio runs at a specified query per second (qps) and records an histogram of execution time and calculates percentiles (e.g. p99 ie the response time such as 99% of the requests take less than that number (in seconds, SI unit))

The name fortio comes from greek φορτίο which is load/burden.

## Command line arguments

```
$ fortio
Φορτίο 0.1 usage:

fortio [flags] url
  -H value
    	Additional Header(s)
  -c int
    	Number of connections/goroutine/threads (0 doesn't change internal default)
  -p string
    	List of pXX to calculate (default "50,75,99,99.9")
  -qps float
    	Queries Per Seconds (default 8)
  -r float
    	Resolution of the histogram lowest buckets in seconds (default 0.001)
  -t duration
    	How long to run the test (default 5s)
  -v int
    	Verbosity level (0 is quiet)
```

## Example output

```
$ fortio https://www.google.com
Running at 8 queries per second for 5s: https://www.google.com
Starting at 8 qps with 4 thread(s) [gomax 8] for 5s : 10 calls each (total 40)
2017/07/04 15:49:17 T001 ended after 5.076985609s : 10 calls. qps=1.9696727093874258
2017/07/04 15:49:17 T002 ended after 5.096783364s : 10 calls. qps=1.9620217862569524
2017/07/04 15:49:17 T003 ended after 5.097467191s : 10 calls. qps=1.9617585803506157
2017/07/04 15:49:17 T000 ended after 5.098168545s : 10 calls. qps=1.9614887016254972
Ended after 5.098196115s : 40 calls. qps=7.8459
Sleep times : count 36 avg 0.46568594 +/- 0.0106 min 0.436353622 max 0.482935658 sum 16.7646938
Aggregated Function Time : count 40 avg 0.08815966 +/- 0.01111 min 0.071791544 max 0.118569671 sum 3.5263864
# range, mid point, percentile, count
>= 0.07 < 0.08 , 0.075 , 27.50, 11
>= 0.08 < 0.09 , 0.085 , 60.00, 13
>= 0.09 < 0.1 , 0.095 , 87.50, 11
>= 0.1 < 0.12 , 0.11 , 100.00, 5
# target 50% 0.0869231
# target 75% 0.0954545
# target 99% 0.117084
# target 99.9% 0.118421
Code 200 : 40
Response Body Sizes : count 40 avg 11500.55 +/- 443.3 min 11266 max 12770 sum 460022
```


## Implementation details

Fortio is written in the [Go](https://golang.org) and includes a scalable semi log histogram in [stats.go](stats.go) and a periodic runner engine in [periodic.go](periodic.go).

You can run the histogram code standalone as a command line in [cmd/histogram/](cmd/histogram/) and a basic echo http server in [cmd/echosrv/](cmd/echosrv/) and the main [cmd/fortio/](cmd/fortio/) 

## Another example output

With 5k qps: (includes envoy and mixer in the calls)
```
$ time fortio -qps 5000 -t 60s -c 8 -r 0.0001 -H "Host: perf-cluster" http://benchmark-2:9090/echo
2017/07/04 23:23:17 will be setting special Host header to perf-cluster
Running at 5000 queries per second for 1m0s: http://benchmark-2:9090/echo
Starting at 5000 qps with 8 thread(s) [gomax 4] for 1m0s : 37500 calls each (total 300000)
2017/07/04 23:24:18 T003 ended after 1m0.001167643s : 37500 calls. qps=624.9878372887783
2017/07/04 23:24:18 T005 ended after 1m0.001495277s : 37500 calls. qps=624.984424586076
2017/07/04 23:24:18 T002 ended after 1m0.001912089s : 37500 calls. qps=624.9800830409666
2017/07/04 23:24:18 T007 ended after 1m0.002169199s : 37500 calls. qps=624.9774049939678
2017/07/04 23:24:18 T004 ended after 1m0.005409967s : 37500 calls. qps=624.9436512578307
2017/07/04 23:24:18 T006 ended after 1m0.005477967s : 37500 calls. qps=624.9429430530179
2017/07/04 23:24:18 T001 ended after 1m0.006298151s : 37500 calls. qps=624.9344011462747
2017/07/04 23:24:18 T000 ended after 1m0.009879778s : 37500 calls. qps=624.8971025892263
Ended after 1m0.009899223s : 300000 calls. qps=4999.2
Aggregated Sleep Time : count 299992 avg -0.000109038 +/- 0.002748 min -0.04674458 max 0.000959315 sum -32.7105262
# range, mid point, percentile, count
< 0 , 0 , 10.14, 30428
>= 0 < 0.001 , 0.0005 , 100.00, 269564
# target 50% 0.000425514
WARNING 10.14% of sleep were falling behind
Aggregated Function Time : count 300000 avg 0.0010281581 +/- 0.0008435 min 0.000532125 max 0.031632753 sum 308.447442
# range, mid point, percentile, count
>= 0.0005 < 0.0006 , 0.00055 , 0.02, 54
>= 0.0006 < 0.0007 , 0.00065 , 0.98, 2873
>= 0.0007 < 0.0008 , 0.00075 , 8.23, 21753
>= 0.0008 < 0.0009 , 0.00085 , 33.09, 74599
>= 0.0009 < 0.001 , 0.00095 , 66.75, 100972
>= 0.001 < 0.0011 , 0.00105 , 86.81, 60193
>= 0.0011 < 0.0012 , 0.00115 , 93.41, 19790
>= 0.0012 < 0.0014 , 0.0013 , 96.54, 9400
>= 0.0014 < 0.0016 , 0.0015 , 97.67, 3373
>= 0.0016 < 0.0018 , 0.0017 , 98.19, 1574
>= 0.0018 < 0.002 , 0.0019 , 98.51, 936
>= 0.002 < 0.0025 , 0.00225 , 98.96, 1369
>= 0.0025 < 0.003 , 0.00275 , 99.19, 674
>= 0.003 < 0.0035 , 0.00325 , 99.33, 434
>= 0.0035 < 0.004 , 0.00375 , 99.43, 302
>= 0.004 < 0.0045 , 0.00425 , 99.52, 271
>= 0.0045 < 0.005 , 0.00475 , 99.62, 307
>= 0.005 < 0.006 , 0.0055 , 99.76, 395
>= 0.006 < 0.007 , 0.0065 , 99.81, 156
>= 0.007 < 0.008 , 0.0075 , 99.82, 25
>= 0.008 < 0.009 , 0.0085 , 99.82, 11
>= 0.009 < 0.01 , 0.0095 , 99.82, 7
>= 0.01 < 0.012 , 0.011 , 99.83, 10
>= 0.012 < 0.014 , 0.013 , 99.83, 8
>= 0.014 < 0.016 , 0.015 , 99.89, 186
>= 0.016 < 0.018 , 0.017 , 99.94, 141
>= 0.018 < 0.02 , 0.019 , 99.94, 22
>= 0.02 < 0.025 , 0.0225 , 99.98, 95
>= 0.025 < 0.03 , 0.0275 , 100.00, 64
>= 0.03 < 0.035 , 0.0325 , 100.00, 6
# target 50% 0.000950233
# target 75% 0.00104112
# target 99% 0.00258457
# target 99.9% 0.0163972
Code 200 : 300000
```

Or graphically:

![Chart](https://user-images.githubusercontent.com/3664595/27844778-3776e1e6-60db-11e7-99fa-8899e21be047.png)
