// Copyright Istio Authors
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

package krt

import (
	"sync"

	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/monitoring"
)

var (
	EnableKRTMetrics = env.Register("ISTIO_ENABLE_KRT_METRICS", true,
		"If enabled, publishes metrics for queue depth, latency and processing times.").Get()

	collectionTag = monitoring.CreateLabel("collection")
	dependencyTag = monitoring.CreateLabel("dependency")
	enableMetric  = monitoring.WithEnabled(func() bool {
		return EnableKRTMetrics
	})

	events           = monitoring.NewSum("krt_incoming_event_count", "Count of events", enableMetric)
	eventsSize       = monitoring.NewSum("krt_incoming_event_size", "Size of events", enableMetric)
	eventsDownstream = monitoring.NewSum("krt_outgoing_event_size", "Size of outgoing events", enableMetric)

	latency = monitoring.NewDistribution("pilot_worker_queue_latency",
		"Latency before the item is processed", []float64{.01, .1, .2, .5, 1, 3, 5}, enableMetric)

	workDuration = monitoring.NewDistribution("pilot_worker_queue_duration",
		"Time taken to process an item", []float64{.01, .1, .2, .5, 1, 3, 5}, enableMetric)
)

type metrics struct {
	events           *cachedMetric
	eventsSize       *cachedMetric
	eventsDownstream *cachedMetric
}

func newMetrics(collection string) *metrics {
	return &metrics{
		events:           newCachedMetric(events.With(collectionTag.Value(collection)), dependencyTag),
		eventsSize:       newCachedMetric(eventsSize.With(collectionTag.Value(collection)), dependencyTag),
		eventsDownstream: newCachedMetric(eventsDownstream.With(collectionTag.Value(collection)), dependencyTag),
	}
}

type cachedMetric struct {
	base    monitoring.Metric
	lbl     monitoring.Label
	mu      sync.RWMutex
	metrics map[string]monitoring.Metric
}

func newCachedMetric(base monitoring.Metric, lbl monitoring.Label) *cachedMetric {
	return &cachedMetric{
		base:    base,
		lbl:     lbl,
		metrics: make(map[string]monitoring.Metric),
	}
}

func (m *cachedMetric) Get(key string) monitoring.Metric {
	m.mu.RLock()
	e, f := m.metrics[key]
	m.mu.RUnlock()
	if f {
		return e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Check again to avoid TOCTOU
	e, f = m.metrics[key]
	if f {
		return e
	}
	n := m.base.With(dependencyTag.Value(key))
	m.metrics[key] = n
	return n
}

func (m *metrics) RecordEvent(dependency string, size int) {
	m.events.Get(dependency).Increment()
	m.eventsSize.Get(dependency).RecordInt(int64(size))
}

func (m *metrics) RecordDownstream(dependency string, size int) {
	m.eventsDownstream.Get(dependency).RecordInt(int64(size))
}
