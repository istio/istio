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

package inject

import (
	"istio.io/pkg/monitoring"
)

var (
	totalInjections = monitoring.NewSum(
		"sidecar_injection_requests_total",
		"Total number of sidecar injection requests.",
	)

	totalSuccessfulInjections = monitoring.NewSum(
		"sidecar_injection_success_total",
		"Total number of successful sidecar injection requests.",
	)

	totalFailedInjections = monitoring.NewSum(
		"sidecar_injection_failure_total",
		"Total number of failed sidecar injection requests.",
	)

	totalSkippedInjections = monitoring.NewSum(
		"sidecar_injection_skip_total",
		"Total number of skipped sidecar injection requests.",
	)
)

func init() {
	monitoring.MustRegister(
		totalInjections,
		totalSuccessfulInjections,
		totalFailedInjections,
		totalSkippedInjections,
	)
}
