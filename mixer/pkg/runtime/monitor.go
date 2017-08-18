package runtime

// Copyright 2016 Istio Authors
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

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	meshFunction = "meshFunction"
	handlerName  = "handler"
	adapterName  = "adapter"
	responseCode = "response_code"
	responseMsg  = "response_message"
	adapterError = "adapter_error"
)

var (
	promLabelNames  = []string{meshFunction, handlerName, adapterName, responseCode, adapterError}
	dispatchBuckets = []float64{.0001, .00025, .0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
	dispatchCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "mixer",
			Subsystem: "adapter",
			Name:      "dispatch_count",
			Help:      "Total number of adapter dispatches handled by Mixer.",
		}, promLabelNames)

	dispatchDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "mixer",
			Subsystem: "adapter",
			Name:      "dispatch_duration",
			Help:      "Histogram of times for adapter dispatches handled by Mixer.",
			Buckets:   dispatchBuckets,
		}, promLabelNames)
)
