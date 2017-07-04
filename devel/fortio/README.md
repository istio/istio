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

Fortio is written in the [Go](https://golang.org) and includes a scalable semi log histogram in [stats.go](fortioLib/stats.go) and a periodic runner engine in [periodic.go](fortioLib/periodic.go).

You can run the histogram code standalone as a command line in [histogram/](histogram/)
