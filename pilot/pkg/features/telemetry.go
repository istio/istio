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
	"strings"
	"time"

	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

// Define telemetry related features here.
var (
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

	// This is an experimental feature flag, can be removed once it became stable, and should introduced to Telemetry API.
	MetricRotationInterval = env.Register("METRIC_ROTATION_INTERVAL", 0*time.Second,
		"Metric scope rotation interval, set to 0 to disable the metric scope rotation").Get()
	MetricGracefulDeletionInterval = env.Register("METRIC_GRACEFUL_DELETION_INTERVAL", 5*time.Minute,
		"Metric expiry graceful deletion interval. No-op if METRIC_ROTATION_INTERVAL is disabled.").Get()

	EnableControllerQueueMetrics = env.Register("ISTIO_ENABLE_CONTROLLER_QUEUE_METRICS", false,
		"If enabled, publishes metrics for queue depth, latency and processing times.").Get()

	// TODO: change this to default true and add compatibility profile in v1.27
	SpawnUpstreamSpanForGateway = env.Register("PILOT_SPAWN_UPSTREAM_SPAN_FOR_GATEWAY", false,
		"If true, separate tracing span for each upstream request for gateway.").Get()
)
