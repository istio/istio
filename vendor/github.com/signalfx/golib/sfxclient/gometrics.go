package sfxclient

import (
	"runtime"
	"time"

	"github.com/signalfx/golib/datapoint"
)

var startTime = time.Now()

// GoMetricsSource is a singleton Collector that collects basic go system stats.  It currently
// collects from runtime.ReadMemStats and adds a few extra metrics like uptime of the process
// and other runtime package functions.
var GoMetricsSource Collector = &goMetrics{}

type goMetrics struct{}

func (g *goMetrics) Datapoints() []*datapoint.Datapoint {
	mstat := runtime.MemStats{}
	runtime.ReadMemStats(&mstat)
	dims := map[string]string{
		"instance": "global_stats",
		"stattype": "golang_sys",
	}
	return []*datapoint.Datapoint{
		Gauge("Alloc", dims, int64(mstat.Alloc)),
		Cumulative("TotalAlloc", dims, int64(mstat.TotalAlloc)),
		Gauge("Sys", dims, int64(mstat.Alloc)),
		Cumulative("Lookups", dims, int64(mstat.Lookups)),
		Cumulative("Mallocs", dims, int64(mstat.Mallocs)),
		Cumulative("Frees", dims, int64(mstat.Frees)),
		Gauge("HeapAlloc", dims, int64(mstat.HeapAlloc)),
		Gauge("HeapSys", dims, int64(mstat.HeapSys)),
		Gauge("HeapIdle", dims, int64(mstat.HeapIdle)),
		Gauge("HeapInuse", dims, int64(mstat.HeapInuse)),
		Gauge("HeapReleased", dims, int64(mstat.HeapReleased)),
		Gauge("HeapObjects", dims, int64(mstat.HeapObjects)),
		Gauge("StackInuse", dims, int64(mstat.StackInuse)),
		Gauge("StackSys", dims, int64(mstat.StackSys)),
		Gauge("MSpanInuse", dims, int64(mstat.MSpanInuse)),
		Gauge("MSpanSys", dims, int64(mstat.MSpanSys)),
		Gauge("MCacheInuse", dims, int64(mstat.MCacheInuse)),
		Gauge("MCacheSys", dims, int64(mstat.MCacheSys)),
		Gauge("BuckHashSys", dims, int64(mstat.BuckHashSys)),
		Gauge("GCSys", dims, int64(mstat.GCSys)),
		Gauge("OtherSys", dims, int64(mstat.OtherSys)),
		Gauge("NextGC", dims, int64(mstat.NextGC)),
		Gauge("LastGC", dims, int64(mstat.LastGC)),
		Cumulative("PauseTotalNs", dims, int64(mstat.PauseTotalNs)),
		Gauge("NumGC", dims, int64(mstat.NumGC)),

		Gauge("GOMAXPROCS", dims, int64(runtime.GOMAXPROCS(0))),
		Gauge("process.uptime.ns", dims, time.Since(startTime).Nanoseconds()),
		Gauge("num_cpu", dims, int64(runtime.NumCPU())),

		Cumulative("num_cgo_call", dims, runtime.NumCgoCall()),

		Gauge("num_goroutine", dims, int64(runtime.NumGoroutine())),
	}
}
