// Copyright 2019 Istio Authors
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
	"istio.io/istio/pilot/pkg/monitoring"
)

var (
	totalInjections = monitoring.NewSum(
		"sidecar_total_injection_requests",
		"Total number of Side car injection requests.",
	)

	totalSuccessfulInjections = monitoring.NewSum(
		"sidecar_total_injection_success",
		"Total number of successful Side car injection requests.",
	)

	totalFailedInjections = monitoring.NewSum(
		"sidecar_total_injection_failure",
		"Total number of failed Side car injection requests.",
	)

	totalSkippedInjections = monitoring.NewSum(
		"sidecar_total_injection_skip",
		"Total number of skipped injection requests.",
	)
)

func init() {
	monitoring.MustRegisterViews(
		totalInjections,
		totalSuccessfulInjections,
		totalFailedInjections,
		totalSkippedInjections,
	)
}
