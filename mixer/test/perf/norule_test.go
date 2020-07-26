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

// NoRule tests are for testing absolute minimum possible. The tests do not contain any handlers or rules at all.
// The code being tested is upto the dispatcher checking for destinations.
//
// This is useful for creating a baseline for the rest of the tests.
var baseNoRuleReportSetup = perf.Setup{
	Config: perf.Config{
		// Global setup is empty
		Global:         ``,
		Service:        minimalServiceConfig,
		SingleThreaded: true,
	},

	Loads: []perf.Load{{
		Multiplier: 1,
		Requests: []perf.Request{
			perf.BuildBasicReport(map[string]interface{}{}),
		},
	}},
}

var baseNoRuleCheckSetup = perf.Setup{
	Config: perf.Config{
		// Global setup is empty
		Global:         ``,
		Service:        minimalServiceConfig,
		SingleThreaded: true,
	},

	Loads: []perf.Load{{
		Multiplier: 1,
		Requests: []perf.Request{
			perf.BuildBasicCheck(map[string]interface{}{}, nil),
		},
	}},
}

func Benchmark_NoRule_Report(b *testing.B) {
	settings := baseSettings
	settings.RunMode = perf.InProcessBypassGrpc

	setup := baseNoRuleReportSetup

	perf.Run(b, &setup, settings)
}

func Benchmark_NoRule_Report_Rpc(b *testing.B) {
	settings := baseSettings
	settings.RunMode = perf.InProcess

	setup := baseNoRuleReportSetup

	perf.Run(b, &setup, settings)
}

func Benchmark_NoRule_Check(b *testing.B) {
	settings := baseSettings
	settings.RunMode = perf.InProcessBypassGrpc

	setup := baseNoRuleCheckSetup

	perf.Run(b, &setup, settings)
}

func Benchmark_NoRule_Check_Rpc(b *testing.B) {
	settings := baseSettings
	settings.RunMode = perf.InProcess

	setup := baseNoRuleCheckSetup

	perf.Run(b, &setup, settings)
}
