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

package queue

import (
	"istio.io/istio/pilot/pkg/features"
	"istio.io/pkg/monitoring"
)

type metrics struct {
	adds    monitoring.Metric
	size    monitoring.Metric
	done    monitoring.Metric
	active  monitoring.Metric
	qerrors monitoring.Metric
}

var (
	idTag = monitoring.MustCreateLabel("id")

	add = monitoring.NewSum(
		"pilot_queue_add_total",
		"Total number of entries to queue.",
		monitoring.WithLabels(idTag),
	)

	size = monitoring.NewGauge(
		"pilot_queue_size",
		"Total number of items in queue.",
		monitoring.WithLabels(idTag),
	)

	done = monitoring.NewSum(
		"pilot_queue_done_total",
		"Total number of items processing done.",
		monitoring.WithLabels(idTag),
	)

	active = monitoring.NewGauge(
		"pilot_queue_active",
		"Total number of items in progress.",
		monitoring.WithLabels(idTag),
	)

	qerrors = monitoring.NewSum(
		"pilot_queue_error_total",
		"Total number of tasks errored and retried.",
		monitoring.WithLabels(idTag),
	)
)

func init() {
	if features.EnableQUICListeners {
		monitoring.MustRegister(add)
		monitoring.MustRegister(size)
		monitoring.MustRegister(done)
		monitoring.MustRegister(active)
		monitoring.MustRegister(qerrors)
	}
}

func (m *metrics) push() {
	if features.EnableQueueMetrics {
		m.adds.Increment()
	}
}

func (m *metrics) complete() {
	if features.EnableQueueMetrics {
		m.done.Increment()
		m.active.Decrement()
	}
}

func (m *metrics) items(s int64) {
	if features.EnableQueueMetrics {
		m.size.RecordInt(s)
	}
}

func (m *metrics) inprogress() {
	if features.EnableQueueMetrics {
		m.active.Increment()
	}
}

func (m *metrics) error() {
	if features.EnableQueueMetrics {
		m.qerrors.Increment()
	}
}
