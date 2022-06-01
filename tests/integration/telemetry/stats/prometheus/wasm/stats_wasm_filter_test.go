//go:build integ
// +build integ

// Copyright Istio Authors. All Rights Reserved.
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

package wasm

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	common "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

// TestWasmStatsFilter verifies the stats filter could emit expected client and server side
// metrics when running with Wasm runtime.
// This test focuses on stats filter and metadata exchange filter could work coherently with
// proxy bootstrap config with Wasm runtime. To avoid flake, it does not verify correctness
// of metrics, which should be covered by integration test in proxy repo.
func TestWasmStatsFilter(t *testing.T) {
	common.TestStatsFilter(t, "observability.telemetry.stats.prometheus.http.wasm")
}

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35915
		Setup(istio.Setup(common.GetIstioInstance(), setupConfig)).
		Setup(common.TestSetup).
		Setup(registrySetup).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	// enable telemetry v2 with Wasm
	cfg.Values["telemetry.v2.metadataExchange.wasmEnabled"] = "true"
	cfg.Values["telemetry.v2.prometheus.wasmEnabled"] = "true"
}
