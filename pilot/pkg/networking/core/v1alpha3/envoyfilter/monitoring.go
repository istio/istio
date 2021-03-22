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
package envoyfilter

import (
	"istio.io/istio/pilot/pkg/features"
	"istio.io/pkg/monitoring"
)

var (
	nodeTag = monitoring.MustCreateLabel("node")
	typeTag = monitoring.MustCreateLabel("type")

	totalEnvoyFiltersSkipped = monitoring.NewSum(
		"pilot_total_envoy_filters_skipped",
		"Total number of Envoy filters skipped for each proxy.",
		monitoring.WithLabels(typeTag, nodeTag),
	)

	totalEnvoyFilterErrors = monitoring.NewSum(
		"pilot_total_envoy_filter_errors",
		"Total number of Envoy filters errored out.",
		monitoring.WithLabels(typeTag, nodeTag),
	)
)

func init() {
	if features.EnableEnvoyFilterMetrics {
		monitoring.MustRegister(totalEnvoyFiltersSkipped)
		monitoring.MustRegister(totalEnvoyFilterErrors)
	}
}

// IncrementSkippedMetric increments skipped filter metric.
func IncrementSkippedMetric(t string, proxy string) {
	if features.EnableEnvoyFilterMetrics {
		totalEnvoyFiltersSkipped.With(typeTag.Value(t), nodeTag.Value(proxy)).Increment()
	}
}

// IncrementErrorMetric increments error filter metric.
func IncrementErrorMetric(t string, proxy string) {
	if features.EnableEnvoyFilterMetrics {
		totalEnvoyFilterErrors.With(typeTag.Value(t), nodeTag.Value(proxy)).Increment()
	}
}
