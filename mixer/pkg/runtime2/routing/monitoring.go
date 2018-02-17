// Copyright 2018 Istio Authors
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

package routing

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	meshFunction = "meshFunction"
	handlerName  = "handler"
	adapterName  = "adapter"
	errorStr     = "error"
)

var (
	durationBuckets    = []float64{.0001, .00025, .0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
	dispatchLabelNames = []string{meshFunction, handlerName, adapterName, errorStr}

	dispatchCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "mixer",
			Subsystem: "runtime",
			Name:      "dispatch_count",
			Help:      "Total number of adapter dispatches handled by Mixer.",
		}, dispatchLabelNames)

	dispatchDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "mixer",
			Subsystem: "runtime",
			Name:      "dispatch_duration",
			Help:      "Histogram of durations for adapter dispatches handled by Mixer.",
			Buckets:   durationBuckets,
		}, dispatchLabelNames)
)

func init() {
	prometheus.MustRegister(dispatchCount)
	prometheus.MustRegister(dispatchDuration)
}

// DestinationCounters are used to track the total/failed dispatch counts and dispatch duration for a target destination,
// based on the template/handler/adapter label set.
type DestinationCounters struct {
	totalCount       prometheus.Counter
	failedTotalCount prometheus.Counter
	duration         prometheus.Observer
	failedDuration   prometheus.Observer
}

// newDestinationCounters returns a new set of DestinationCounters instance.
func newDestinationCounters(template string, handler string, adapter string) DestinationCounters {
	successLabels := prometheus.Labels{
		meshFunction: template,
		handlerName:  handler,
		adapterName:  adapter,
		errorStr:     "false",
	}

	failedLabels := prometheus.Labels{
		meshFunction: template,
		handlerName:  handler,
		adapterName:  adapter,
		errorStr:     "true",
	}

	return DestinationCounters{
		totalCount:       dispatchCount.With(successLabels),
		duration:         dispatchDuration.With(successLabels),
		failedTotalCount: dispatchCount.With(failedLabels),
		failedDuration:   dispatchDuration.With(failedLabels),
	}
}

// Update the counters. Duration is the total dispatch duration. Failed indicates whether the dispatch returned an error or not.
func (d DestinationCounters) Update(duration time.Duration, failed bool) {
	if failed {
		d.failedTotalCount.Inc()
		d.failedDuration.Observe(duration.Seconds())
	} else {
		d.totalCount.Inc()
		d.duration.Observe(duration.Seconds())
	}
}
