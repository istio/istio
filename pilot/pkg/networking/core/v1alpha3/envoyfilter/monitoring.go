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

	envoyFilterStatus = monitoring.NewGauge(
		"pilot_envoy_filter_status",
		"Status of Envoy filters whether it was applied or errored.",
		monitoring.WithLabels(nameType, patchType, resultType),
	)
)

func init() {
	if features.EnableEnvoyFilterMetrics {
		monitoring.MustRegister(envoyFilterStatus)
	}
}

// IncrementEnvoyFilterMetric increments filter metric.
func IncrementEnvoyFilterMetric(name string, pt PatchType, applied bool) {
	if !features.EnableEnvoyFilterMetrics {
		return
	}
	// Only set the Gauge when the filter is atleast applied once.
	if applied {
		envoyFilterStatus.With(nameType.Value(name)).With(patchType.Value(string(pt))).
			With(resultType.Value(string(Applied))).Record(1)
	}
}

// IncrementEnvoyFilterErrorMetric increments filter metric for errors.
func IncrementEnvoyFilterErrorMetric(pt PatchType) {
	if !features.EnableEnvoyFilterMetrics {
		return
	}
	envoyFilterStatus.With(patchType.Value(string(pt))).With(resultType.Value(string(Error))).Record(1)
}
