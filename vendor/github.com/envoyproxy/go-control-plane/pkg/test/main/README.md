# End-to-end testing script

This directory contains a basic end-to-end testing script.
The script sets up a configuration cache, stands up a configuration server,
and starts up Envoy with the server as either ADS or xDS discovery option. The
configuration is periodically refreshed with new routes and new clusters. In
parallel, the test sends echo requests one after another through Envoy,
exercising the pushed configuration.

## Requirements

* Envoy binary `envoy` available: set `ENVOY` environment variable to the
  location of the binary, or use the default value `/usr/local/bin/envoy`
* `go-control-plane` builds successfully

## Steps

To run the script with a single ADS server:

    make integration.ads

To run the script with a single server configured as different xDS servers:

    make integration.xds

To run the script with a single server configured to use `Fetch` through HTTP:

    make integration.rest

You should see runs of configuration push events and request batch reports. The
test executes batches of requests to exercise multiple listeners, routes, and
clusters, and records the number of successful and failed requests. The test is
successful if at least one batch passes through all requests (e.g. Envoy
eventually converges to use the latest pushed configuration) for each run.

## Customizing the test driver

The test driver has the following options: 

```
Usage of bin/test:
  -als uint
    	Accesslog server port (default 18090)
  -base uint
    	Listener port (default 9000)
  -clusters int
    	Number of clusters (default 2)
  -debug
    	Use debug logging
  -delay duration
    	Interval between request batch retries (default 500ms)
  -gateway uint
    	Management server port for HTTP gateway (default 18001)
  -http int
    	Number of HTTP listeners (and RDS configs) (default 1)
  -nodeID string
    	Node ID (default "test-id")
  -port uint
    	Management server port (default 18000)
  -r int
    	Number of requests between snapshot updates (default 5)
  -tcp int
    	Number of TCP pass-through listeners (default 1)
  -u int
    	Number of snapshot updates (default 3)
  -upstream uint
    	Upstream HTTP/1.1 port (default 18080)
  -xds string
    	Management server type (ads, xds, rest) (default "ads")
```
