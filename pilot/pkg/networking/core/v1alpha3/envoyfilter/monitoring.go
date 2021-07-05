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

type Result string

const (
	Error   Result = "error"
	Skipped Result = "skipped"
	Applied Result = "applied"
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
	Bootstrap   PatchType = "bootstrap"
)

var (
	patchType  = monitoring.MustCreateLabel("patch")
	resultType = monitoring.MustCreateLabel("result")
	nameType   = monitoring.MustCreateLabel("name")

	totalEnvoyFilters = monitoring.NewSum(
		"pilot_total_envoy_filter",
		"Total number of Envoy filters that were applied, skipped and errored.",
		monitoring.WithLabels(nameType, patchType, resultType),
	)
)

func init() {
	if features.EnableEnvoyFilterMetrics {
		monitoring.MustRegister(totalEnvoyFilters)
	}
}

// IncrementEnvoyFilterMetric increments filter metric.
func IncrementEnvoyFilterMetric(name string, pt PatchType, applied bool) {
	if !features.EnableEnvoyFilterMetrics {
		return
	}
	result := Applied
	if !applied {
		result = Skipped
	}
	totalEnvoyFilters.With(nameType.Value(name)).With(patchType.Value(string(pt))).
		With(resultType.Value(string(result))).Record(1)
}

// IncrementEnvoyFilterErrorMetric increments filter metric for errors.
func IncrementEnvoyFilterErrorMetric(pt PatchType) {
	if !features.EnableEnvoyFilterMetrics {
		return
	}
	totalEnvoyFilters.With(patchType.Value(string(pt))).With(resultType.Value(string(Error))).Record(1)
}

// RecordEnvoyFilterMetric increments the filter metric with the given value.
func RecordEnvoyFilterMetric(name string, pt PatchType, success bool, value float64) {
	if !features.EnableEnvoyFilterMetrics {
		return
	}
	result := Applied
	if !success {
		result = Skipped
	}
	totalEnvoyFilters.With(nameType.Value(name)).With(patchType.Value(string(pt))).With(resultType.Value(string(result))).Record(value)
}
