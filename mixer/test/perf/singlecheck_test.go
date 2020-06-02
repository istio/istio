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

// Basic check setup using a single check dispatch to a single adapter.
//
// Tests the most basic end-to-end check execution using the shortest possible path of execution. This does not
// include any expression evaluation.
var baseSingleCheckSetup = perf.Setup{
	Config: perf.Config{
		Global:         minimalServiceConfig,
		Service:        joinConfigs(h1Noop, i2CheckNothing, r2UsingH1AndI2),
		SingleThreaded: true,
	},

	Loads: []perf.Load{{
		Multiplier: 1,
		Requests: []perf.Request{
			perf.BuildBasicCheck(
				map[string]interface{}{
					"attr.bool":   true,
					"attr.string": "str1",
					"attr.double": float64(23.45),
					"attr.int64":  int64(42),
				}, nil),
		},
	}},
}

func Benchmark_Single_Check(b *testing.B) {
	settings := baseSettings
	settings.RunMode = perf.InProcessBypassGrpc

	setup := baseSingleCheckSetup

	perf.Run(b, &setup, settings)
}

func Benchmark_Single_Check_Rpc(b *testing.B) {
	settings := baseSettings
	settings.RunMode = perf.InProcess

	setup := baseSingleCheckSetup

	perf.Run(b, &setup, settings)
}
