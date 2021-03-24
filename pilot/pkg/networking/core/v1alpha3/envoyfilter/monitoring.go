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
	"istio.io/pkg/monitoring"
)

type ErrorType string

const (
	Error   ErrorType = "error"
	Skipped           = "skipped"
)

var (
	filterType = monitoring.MustCreateLabel("type")
	errorType  = monitoring.MustCreateLabel("error")

	totalEnvoyFilterFailures = monitoring.NewSum(
		"pilot_total_envoy_filter",
		"Total number of Envoy filters that were not applied to the configuration.",
		monitoring.WithLabels(filterType, errorType),
	)
)

func init() {
	monitoring.MustRegister(totalEnvoyFilterFailures)
}

// IncrementEnvoyFilterMetric increments skipped and errored filter metric.
func IncrementEnvoyFilterMetric(ft string, et ErrorType) {
	totalEnvoyFilterFailures.With(filterType.Value(ft)).With(errorType.Value(string(et))).Increment()
}
