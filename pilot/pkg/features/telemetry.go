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

package features

import (
	"math"
	"strconv"
	"strings"

	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

// Define telemetry related features here.
var (
	ProxyConvergenceTimeBuckets = parseProxyConvergenceTimeBuckets(env.Register(
		"PILOT_PROXY_CONVERGENCE_TIME_BUCKETS_SECONDS",
		"0.1,0.5,1,3,5,10,20,30",
		"Comma-separated histogram bucket boundaries in seconds for pilot_proxy_convergence_time. "+
			"Values must be finite, nonnegative, and strictly increasing. Empty or invalid values use the default boundaries. "+
			"Changes require an istiod restart.",
	).Get())

	traceSamplingVar = env.Register(
		"PILOT_TRACE_SAMPLING",
		1.0,
		"Sets the mesh-wide trace sampling percentage. Should be 0.0 - 100.0. Precision to 0.01. "+
			"Default is 1.0.",
	)

	TraceSampling = func() float64 {
		f := traceSamplingVar.Get()
		if f < 0.0 || f > 100.0 {
			log.Warnf("PILOT_TRACE_SAMPLING out of range: %v", f)
			return 1.0
		}
		return f
	}()

	EnableTelemetryLabel = env.Register("PILOT_ENABLE_TELEMETRY_LABEL", true,
		"If true, pilot will add telemetry related metadata to cluster and endpoint resources, which will be consumed by telemetry filter.",
	).Get()

	EndpointTelemetryLabel = env.Register("PILOT_ENDPOINT_TELEMETRY_LABEL", true,
		"If true, pilot will add telemetry related metadata to Endpoint resource, which will be consumed by telemetry filter.",
	).Get()

	MetadataExchange = env.Register("PILOT_ENABLE_METADATA_EXCHANGE", true,
		"If true, pilot will add metadata exchange filters, which will be consumed by telemetry filter.",
	).Get()

	MetadataExchangeAdditionalLabels = func() []any {
		v := env.Register("PILOT_MX_ADDITIONAL_LABELS", "",
			"Comma separated list of additional labels to be added to metadata exchange filter.",
		).Get()
		if v == "" {
			return nil
		}
		labels := sets.SortedList(sets.New(strings.Split(v, ",")...))
		res := make([]any, 0, len(labels))
		for _, lb := range labels {
			res = append(res, lb)
		}
		return res
	}()

	EnableControllerQueueMetrics = env.Register("ISTIO_ENABLE_CONTROLLER_QUEUE_METRICS", false,
		"If enabled, publishes metrics for queue depth, latency and processing times.").Get()

	AgentMergeEnvoyStats = env.Register("PILOT_AGENT_MERGE_ENVOY_STATS", true,
		"If false, pilot agent will not merge Envoy stats in the agent stats endpoint.").Get()
)

func parseProxyConvergenceTimeBuckets(value string) []float64 {
	defaults := []float64{.1, .5, 1, 3, 5, 10, 20, 30}
	if strings.TrimSpace(value) == "" {
		return defaults
	}
	entries := strings.Split(value, ",")
	bounds := make([]float64, 0, len(entries))
	for _, entry := range entries {
		bound, err := strconv.ParseFloat(strings.TrimSpace(entry), 64)
		if err != nil || math.IsNaN(bound) || math.IsInf(bound, 0) || bound < 0 ||
			(len(bounds) > 0 && bound <= bounds[len(bounds)-1]) {
			log.Warnf("Invalid PILOT_PROXY_CONVERGENCE_TIME_BUCKETS_SECONDS %q: expected finite, nonnegative, strictly increasing boundaries; using defaults %v",
				value, defaults)
			return defaults
		}
		bounds = append(bounds, bound)
	}
	return bounds
}
