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

package monitoring_test

import "istio.io/istio/pkg/monitoring"

var pushLatency = monitoring.NewGauge(
	"push_latency_seconds",
	"Duration, measured in seconds, of the last push",
	monitoring.WithUnit(monitoring.Seconds),
)

func ExampleNewGauge() {
	// only the last recorded value (99.2) will be exported for this gauge
	pushLatency.Record(77.3)
	pushLatency.Record(22.8)
	pushLatency.Record(99.2)
}
