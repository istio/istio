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

type ResultType string

const (
	Error   ResultType = "error"
	Skipped ResultType = "skipped"
	Applied ResultType = "applied"
)

type PatchType string

const (
	Cluster       PatchType = "cluster"
	Listener      PatchType = "listener"
	FilterChain   PatchType = "filterchain"
	NetworkFilter PatchType = "networkfilter"
	// nolint
	HttpFilter  PatchType = "httpfilter"
	Route       PatchType = "route"
	VirtualHost PatchType = "vhost"
)

var (
	patchType = monitoring.MustCreateLabel("patch")
	errorType = monitoring.MustCreateLabel("type")

	totalEnvoyFilters = monitoring.NewSum(
		"pilot_total_envoy_filter",
		"Total number of Envoy filters that were applied, skipped and errored.",
		monitoring.WithLabels(patchType, errorType),
	)
)

func init() {
	monitoring.MustRegister(totalEnvoyFilters)
}

// IncrementEnvoyFilterMetric increments filter metric.
func IncrementEnvoyFilterMetric(pt PatchType, et ResultType) {
	totalEnvoyFilters.With(patchType.Value(string(pt))).With(errorType.Value(string(et))).Increment()
}
