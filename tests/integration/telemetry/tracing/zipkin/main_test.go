//go:build integ

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

package zipkin

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/telemetry/tracing"
)

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(tracing.GetIstioInstance(), setupConfig)).
		Setup(tracing.TestSetup).
		Run()
}

func setupConfig(ctx resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["meshConfig.enableTracing"] = "true"
	cfg.Values["pilot.traceSampling"] = "100.0"
	cfg.Values["global.proxy.tracer"] = "zipkin"
	cfg.Values["meshConfig.extensionProviders[0].zipkin.traceContextOption"] = "USE_B3_WITH_W3C_PROPAGATION"
	// Configure timeout and headers for testing
	cfg.Values["meshConfig.extensionProviders[0].zipkin.timeout"] = "10s"
	cfg.Values["meshConfig.extensionProviders[0].zipkin.headers[0].name"] = "X-Custom-Header"
	cfg.Values["meshConfig.extensionProviders[0].zipkin.headers[0].value"] = "test-value"
	cfg.Values["meshConfig.extensionProviders[0].zipkin.headers[1].name"] = "Authorization"
	cfg.Values["meshConfig.extensionProviders[0].zipkin.headers[1].value"] = "Bearer test-token"
}
