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
	"time"

	"k8s.io/utils/clock"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/monitoring"
)

var (
	queueIDTag   = monitoring.CreateLabel("queueID")
	enableMetric = monitoring.WithEnabled(func() bool {
		return features.EnableControllerQueueMetrics
	})
	depth = monitoring.NewGauge("pilot_worker_queue_depth", "Depth of the controller queues", enableMetric)

	latency = monitoring.NewDistribution("pilot_worker_queue_latency",
		"Latency before the item is processed", []float64{.01, .1, .2, .5, 1, 3, 5}, enableMetric)

	workDuration = monitoring.NewDistribution("pilot_worker_queue_duration",
		"Time taken to process an item", []float64{.01, .1, .2, .5, 1, 3, 5}, enableMetric)
)

type queueMetrics struct {
	depth        monitoring.Metric
	latency      monitoring.Metric
	workDuration monitoring.Metric
	id           string
	clock        clock.WithTicker
}

// Gets the time since the specified start in seconds.
func (m *queueMetrics) sinceInSeconds(start time.Time) float64 {
	return m.clock.Since(start).Seconds()
}

func newQueueMetrics(id string) *queueMetrics {
	return &queueMetrics{
		id:           id,
		depth:        depth.With(queueIDTag.Value(id)),
		workDuration: workDuration.With(queueIDTag.Value(id)),
		latency:      latency.With(queueIDTag.Value(id)),
		clock:        clock.RealClock{},
	}
}
