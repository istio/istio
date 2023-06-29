// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package queue

import (
	"istio.io/istio/pkg/monitoring"
	"k8s.io/utils/clock"
	"time"
)

var (
	queueIdTag = monitoring.MustCreateLabel("queueId")

	depth = monitoring.NewGauge("pilot_worker_queue_depth", "Depth of the controller queues", monitoring.WithLabels(queueIdTag))

	latency = monitoring.NewDistribution(
		"pilot_worker_queue_latency",
		"Latency before the item is processed",
		[]float64{.01, .1, 0.5, 1, 3, 5},
		monitoring.WithLabels(queueIdTag))

	workDuration = monitoring.NewDistribution("pilot_worker_queue_duration",
		"Time taken to process an item",
		[]float64{.01, .1, 0.5, 1, 3, 5},
		monitoring.WithLabels(queueIdTag))
)

type queueMetrics struct {
	depth                monitoring.Metric
	latency              monitoring.Metric
	workDuration         monitoring.Metric
	id                   string
	addTimes             map[*Task]time.Time
	processingStartTimes map[*Task]time.Time
	clock                clock.WithTicker
	queueDepth           int64
}

func (m *queueMetrics) add(item *Task) {
	if m == nil {
		return
	}
	m.queueDepth++
	m.depth.RecordInt(m.queueDepth)
	if _, exists := m.addTimes[item]; !exists {
		m.addTimes[item] = m.clock.Now()
	}
}

func (m *queueMetrics) get(item *Task) {
	if m == nil {
		return
	}
	m.queueDepth--
	m.processingStartTimes[item] = m.clock.Now()
	m.depth.RecordInt(m.queueDepth)
	if startTime, exists := m.addTimes[item]; exists {
		m.latency.Record(m.sinceInSeconds(startTime))
		delete(m.addTimes, item)
	}
}

func (m *queueMetrics) done(item *Task) {
	if m == nil {
		return
	}

	if startTime, exists := m.processingStartTimes[item]; exists {
		m.workDuration.Record(m.sinceInSeconds(startTime))
		delete(m.processingStartTimes, item)
	}
}

// Gets the time since the specified start in seconds.
func (m *queueMetrics) sinceInSeconds(start time.Time) float64 {
	return m.clock.Since(start).Seconds()
}

func NewQueueMetrics(id string) *queueMetrics {
	return &queueMetrics{
		id:                   id,
		depth:                depth.With(queueIdTag.Value(id)),
		workDuration:         workDuration.With(queueIdTag.Value(id)),
		latency:              latency.With(queueIdTag.Value(id)),
		clock:                clock.RealClock{},
		addTimes:             map[*Task]time.Time{},
		processingStartTimes: map[*Task]time.Time{},
	}
}

func init() {
	monitoring.MustRegister(depth, latency, workDuration)
}
