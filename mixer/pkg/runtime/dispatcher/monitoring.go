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

package dispatcher

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	countBuckets = []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 15, 20}

	destinationsPerRequest = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "mixer",
		Subsystem: "dispatcher",
		Name:      "destinations_per_request",
		Help:      "Histogram of destination handlers dispatched to per request, by Mixer.",
		Buckets:   countBuckets,
	})

	instancesPerRequest = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "mixer",
		Subsystem: "dispatcher",
		Name:      "instances_per_request",
		Help:      "Histogram of inputs dispatched per request, by Mixer.",
		Buckets:   countBuckets,
	})
)

func init() {
	prometheus.MustRegister(destinationsPerRequest)
	prometheus.MustRegister(instancesPerRequest)
}

// updateRequestCounters updates request related counters. Duration is the total request handling duration. Destinations
// is the number of destinations (i.a. handlers) that were dispatched to, during handling. Similarly, inputs is the
// total number of instances that got created and sent to the adapter.
func updateRequestCounters(destinations int, inputs int) {
	destinationsPerRequest.Observe(float64(destinations))
	instancesPerRequest.Observe(float64(inputs))
}
