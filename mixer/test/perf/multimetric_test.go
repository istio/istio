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

// Package test supplies a fake Mixer server for use in testing. It should NOT
// be used outside of testing contexts.
package perftests

import (
	"testing"

	"istio.io/istio/mixer/pkg/perf"
)

// Basic reporting setup using multiple reported metric values in a request.
//
// This is a step-up from singlereport, as builds and instance from expressions, thus exercising generated template code.

var baseMultiMetricSetup = perf.Setup{
	Config: perf.Config{
		Global:         minimalServiceConfig,
		Service:        joinConfigs(h1Noop, i3Metric, i4Metric, i5Metric, i6Metric, i7Metric, r6UsingH1AndI3To7),
		SingleThreaded: true,
	},

	Loads: []perf.Load{{
		Multiplier: 1,
		Requests: []perf.Request{
			perf.BuildBasicReport(map[string]interface{}{
				"source.service":      "AcmeService",
				"source.labels":       map[string]string{"version": "23"},
				"destination.service": "DevNullService",
				"destination.labels":  map[string]string{"version": "42"},
				"response.code":       int64(200),
				"request.size":        int64(666),
			}),
		},
	}},
}

func Benchmark_Multi_Metric(b *testing.B) {
	settings := baseSettings
	settings.RunMode = perf.InProcessBypassGrpc

	setup := baseMultiMetricSetup

	perf.Run(b, &setup, settings)
}

func Benchmark_Multi_Metric_Rpc(b *testing.B) {
	settings := baseSettings
	settings.RunMode = perf.InProcess

	setup := baseMultiMetricSetup

	perf.Run(b, &setup, settings)
}
