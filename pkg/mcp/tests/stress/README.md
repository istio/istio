# Overview

This MCP stress test suite provides a set of regression tests and
preconfigured benchmarks for analyzing cpu and memory usage. Most of
the testing parameters are exposed as flags to enable additional
experiments to be run.

## Methodology

A single source server periodically publishes snapshots from a
pre-generated dataset. The dataset is generated over a fixed set of
simulated collection types.  Each snapshot includes includes a random
set of added, updated, and deleted resources across the set of known
collections. Each collection version with a particular snapshot is
significant for verifying consistent end-to-end delivery.  These
version is a stable hash of all resource names and versions in the
full _desired_ snapshot before being encoding over MCP and pushed to
the client. These version is encoded in the SystemVersionInfo in the
source server's MCP response to the client. Clients can perform the
same stable hash over the _applied_ set of resources and verify that
their hash matches the received hash (via SystemVersionInfo).  The
hash check will fail if there are discrepancies between the server's
desired state and the client's applied state. This works for both full
state and incremental MCP variants.  It also works in cases where the
server's publish rate is faster than individual clients receive
rate. The MCP server may effectively coalesce intermediate snapshots
and only publish the most recent version to the client. This is
allowed by the protocol and should result in eventually consistent
state that is correct.

_configuration knobs_:

- The number of simulated collections in the dataset, along with the
number of snapshots and random changes within each snapshot are
configurable.

- The number of simulated clients and their NACK rate can be
  configured with adjusted.

- Random delay with min/max constraints can be inserted in both the
server-side publishing ({min,max}UpdateDelay) and client-side apply
path ({min,max)ApplyDelay).

## Regression test

Regression tests will be run as part of normal presubmit
testing. Total running time is tuned to complete on the order of tens
of seconds. Longer running experiments can be run by changing the
default settings via flags (see experiments below).

```golang
go test ./pkg/mcp/tests/stress/stress_test.go
```

## Benchmarks

```bash
$ go test ./pkg/mcp/tests/stress/stress_test.go -bench BenchmarkFullState -run ^Bench -cpuprofile cpu.out -memprofile mem.out
Generating common dataset ...
Finished generating common dataset
goos: linux
goarch: amd64
BenchmarkFullState-12            500     2318910 ns/op
PASS
ok    command-line-arguments     7.488s
```

This creates two pprof profile files.

```bash
$ file cpu.out mem.out
cpu.out: gzip compressed data, max speed, original size 54280
mem.out: gzip compressed data, max speed, original size 37536
```

Use `go tool pprof` to view the cpu and memory profiles.

```bash
go tool pprof -http=":8081" cpu.out
go tool pprof -http=":8081" mem.out
```

Note that these profiles include resources used by the test harness
itself. Analysis should account for resources used by the dataset
initialization and server and client test runner paths.

## Experiments

Several test parameters are exposed as flags. For example, the
following test runs the full state regression test with 100 clients
and a simulated NACK rate of 10%.

```bash
$ go test ./pkg/mcp/tests/stress/stress_test.go \
    -run ^TestFullState$ \
    -v -args \
    -iterations=100 \
    -num_clients=1000 \
    -nack_rate=0.10
Generating common dataset ...
Finished generating common dataset
=== RUN   TestFullState
updates published                          100
unique datasets                            1000
updates applied by client                  39564
inc                                        0
added                                      0
removed                                    0
acked                                      35724 (90.3%)
nacked                                     3840 (9.71%)
inconsistent changes detected by client    0
GoRoutines                                 7076
--- PASS: TestFullState (0.82s)
PASS
ok       command-line-arguments  5.469s
```

These can also be used to run benchmark experiments.

```bash
$ go test ./pkg/mcp/tests/stress/stress_test.go \
    -run ^Bench \
    -bench BenchmarkFullState \\\
    -v -args \\
    -iterations=100 \
    -num_clients=1000 \
    -nack_rate=0.10
Generating common dataset ...
Finished generating common dataset
goos: linux
goarch: amd64
BenchmarkFullState-12            500      2212815 ns/op
PASS
ok      command-line-arguments   7.682s
```

```bash
go tool pprof -http=":8081" cpu.out
# or ...
go tool pprof -http=":8081" mem.out
```

The current set of flags can be discovered by passed the `-args -h` to the test.

```bash
$ go test ./pkg/mcp/tests/stress/stress_test.go -args -h
  -client_inc_rate float
        percentate of clients that request incremental updates [0,1]
  -iterations int
        number of update iterations (default 1000)
  -max_apply_delay duration
        maximum extra delay inserted during per-client apply.
  -max_changes int
        maximum number of changes per commonDataset (default 10)
  -max_update_delay duration
        maximum extra delay inserted between snapshot updates
  -min_apply_delay duration
        minimum extra delay inserted during per-client apply.
  -min_update_delay duration
        minimum extra delay inserted between snapshot updates
  -nack_rate float
        rate at which clients nack updates. Should be in the range [0,1] (default 0.01)
  -num_clients int
        number of load numClients (default 20)
  -num_collections int
        number of collections (default 50)
  -num_datasets int
        number of datasets to pre-generate for the test (default 1000)
  -rand_seed int
        initial random seed for generating datasets
  -step_rate duration
        step rate for publishing new snapshot updates (default 1ms)
```
