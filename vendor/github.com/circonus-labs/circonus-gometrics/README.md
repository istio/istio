# Circonus metrics tracking for Go applications

This library supports named counters, gauges and histograms. It also provides convenience wrappers for registering latency instrumented functions with Go's builtin http server.

Initializing only requires setting an [API Token](https://login.circonus.com/user/tokens) at a minimum.

## Options

See [OPTIONS.md](OPTIONS.md) for information on all of the available cgm options.

## Example

### Bare bones minimum

A working cut-n-past example. Simply set the required environment variable `CIRCONUS_API_TOKEN` and run.

```go
package main

import (
    "log"
    "math/rand"
    "os"
    "os/signal"
    "syscall"
    "time"

    cgm "github.com/circonus-labs/circonus-gometrics"
)

func main() {

    logger := log.New(os.Stdout, "", log.LstdFlags)

    logger.Println("Configuring cgm")

    cmc := &cgm.Config{}
    cmc.Debug = false // set to true for debug messages
    cmc.Log = logger

    // Circonus API Token key (https://login.circonus.com/user/tokens)
    cmc.CheckManager.API.TokenKey = os.Getenv("CIRCONUS_API_TOKEN")

    logger.Println("Creating new cgm instance")

    metrics, err := cgm.NewCirconusMetrics(cmc)
    if err != nil {
        logger.Println(err)
        os.Exit(1)
    }

    src := rand.NewSource(time.Now().UnixNano())
    rnd := rand.New(src)

    logger.Println("Adding ctrl-c trap")
    c := make(chan os.Signal, 2)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    go func() {
        <-c
        logger.Println("Received CTRL-C, flushing outstanding metrics before exit")
        metrics.Flush()
        os.Exit(0)
    }()

    logger.Println("Starting to send metrics")

    // number of "sets" of metrics to send
    max := 60

    for i := 1; i < max; i++ {
        logger.Printf("\tmetric set %d of %d", i, 60)
        metrics.Timing("foo", rnd.Float64()*10)
        metrics.Increment("bar")
        metrics.Gauge("baz", 10)
        time.Sleep(time.Second)
    }

    metrics.SetText("fini", "complete")

    logger.Println("Flushing any outstanding metrics manually")
    metrics.Flush()
}
```

### A more complete example

A working, cut-n-paste example with all options available for modification. Also, demonstrates metric tagging.

```go
package main

import (
    "log"
    "math/rand"
    "os"
    "os/signal"
    "syscall"
    "time"

    cgm "github.com/circonus-labs/circonus-gometrics"
)

func main() {

    logger := log.New(os.Stdout, "", log.LstdFlags)

    logger.Println("Configuring cgm")

    cmc := &cgm.Config{}

    // General

    cmc.Interval = "10s"
    cmc.Log = logger
    cmc.Debug = false
    cmc.ResetCounters = "true"
    cmc.ResetGauges = "true"
    cmc.ResetHistograms = "true"
    cmc.ResetText = "true"

    // Circonus API configuration options
    cmc.CheckManager.API.TokenKey = os.Getenv("CIRCONUS_API_TOKEN")
    cmc.CheckManager.API.TokenApp = os.Getenv("CIRCONUS_API_APP")
    cmc.CheckManager.API.URL = os.Getenv("CIRCONUS_API_URL")
    cmc.CheckManager.API.TLSConfig = nil

    // Check configuration options
    cmc.CheckManager.Check.SubmissionURL = os.Getenv("CIRCONUS_SUBMISSION_URL")
    cmc.CheckManager.Check.ID = os.Getenv("CIRCONUS_CHECK_ID")
    cmc.CheckManager.Check.InstanceID = ""
    cmc.CheckManager.Check.DisplayName = ""
    cmc.CheckManager.Check.TargetHost = ""
    //  if hn, err := os.Hostname(); err == nil {
    //      cmc.CheckManager.Check.TargetHost = hn
    //  }
    cmc.CheckManager.Check.SearchTag = ""
    cmc.CheckManager.Check.Secret = ""
    cmc.CheckManager.Check.Tags = ""
    cmc.CheckManager.Check.MaxURLAge = "5m"
    cmc.CheckManager.Check.ForceMetricActivation = "false"

    // Broker configuration options
    cmc.CheckManager.Broker.ID = ""
    cmc.CheckManager.Broker.SelectTag = ""
    cmc.CheckManager.Broker.MaxResponseTime = "500ms"
    cmc.CheckManager.Broker.TLSConfig = nil

    logger.Println("Creating new cgm instance")

    metrics, err := cgm.NewCirconusMetrics(cmc)
    if err != nil {
        logger.Println(err)
        os.Exit(1)
    }

    src := rand.NewSource(time.Now().UnixNano())
    rnd := rand.New(src)

    logger.Println("Adding ctrl-c trap")
    c := make(chan os.Signal, 2)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    go func() {
        <-c
        logger.Println("Received CTRL-C, flushing outstanding metrics before exit")
        metrics.Flush()
        os.Exit(0)
    }()

    // Add metric tags (append to any existing tags on specified metric)
    metrics.AddMetricTags("foo", []string{"cgm:test"})
    metrics.AddMetricTags("baz", []string{"cgm:test"})

    logger.Println("Starting to send metrics")

    // number of "sets" of metrics to send
    max := 60

    for i := 1; i < max; i++ {
        logger.Printf("\tmetric set %d of %d", i, 60)

        metrics.Timing("foo", rnd.Float64()*10)
        metrics.Increment("bar")
        metrics.Gauge("baz", 10)

        if i == 35 {
            // Set metric tags (overwrite current tags on specified metric)
            metrics.SetMetricTags("baz", []string{"cgm:reset_test", "cgm:test2"})
        }

        time.Sleep(time.Second)
    }

    logger.Println("Flushing any outstanding metrics manually")
    metrics.Flush()

}
```

### HTTP Handler wrapping

```go
http.HandleFunc("/", metrics.TrackHTTPLatency("/", handler_func))
```

### HTTP latency example

```go
package main

import (
    "os"
    "fmt"
    "net/http"
    cgm "github.com/circonus-labs/circonus-gometrics"
)

func main() {
    cmc := &cgm.Config{}
    cmc.CheckManager.API.TokenKey = os.Getenv("CIRCONUS_API_TOKEN")

    metrics, err := cgm.NewCirconusMetrics(cmc)
    if err != nil {
        panic(err)
    }

    http.HandleFunc("/", metrics.TrackHTTPLatency("/", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "Hello, %s!", r.URL.Path[1:])
    }))
    http.ListenAndServe(":8080", http.DefaultServeMux)
}

```

Unless otherwise noted, the source files are distributed under the BSD-style license found in the [LICENSE](LICENSE) file.
