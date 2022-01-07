//go:build integ
// +build integ

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

package nullvm

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	common "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

func TestAccessLogs(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.logging").
		Run(func(t framework.TestContext) {
			t.NewSubTest("enabled").Run(func(t framework.TestContext) {
				applyTelemetryResource(t, true)
				common.RunAccessLogsTests(t, true)
				deleteTelemetryResource(t, true)
			})
			t.NewSubTest("disabled").Run(func(t framework.TestContext) {
				applyTelemetryResource(t, false)
				common.RunAccessLogsTests(t, false)
				deleteTelemetryResource(t, false)
			})
		})
}

func TestAccessLogsDefaultProvider(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.logging.defaultprovider").
		Run(func(t framework.TestContext) {
			t.NewSubTest("disabled").Run(func(t framework.TestContext) {
				cfg := `
accessLogFile: "/dev/null"
`
				ist := *(common.GetIstioInstance())
				istio.PatchMeshConfig(t, ist.Settings().IstioNamespace, t.Clusters(), cfg)
				common.RunAccessLogsTests(t, false)
				cfg = `
accessLogFile: "/dev/stdout"
`
				istio.PatchMeshConfig(t, ist.Settings().IstioNamespace, t.Clusters(), cfg)
			})
			t.NewSubTest("enabled").Run(func(t framework.TestContext) {
				cfg := `
accessLogFile: "/dev/null"
defaultProviders:
  accessLogging:
  - envoy
`
				ist := *(common.GetIstioInstance())
				istio.PatchMeshConfig(t, ist.Settings().IstioNamespace, t.Clusters(), cfg)
				time.Sleep(5 * time.Second)
				common.RunAccessLogsTests(t, true)
				cfg = `
accessLogFile: "/dev/stdout"
defaultProviders:
  accessLogging: []
`
				istio.PatchMeshConfig(t, ist.Settings().IstioNamespace, t.Clusters(), cfg)
			})
		})
}

func applyTelemetryResource(t framework.TestContext, expectLogs bool) {
	config := fmt.Sprintf(`apiVersion: telemetry.istio.io/v1alpha1
kind: Telemetry
metadata:
  name: logs
spec:
  accessLogging:
  - disabled: %v
`, !expectLogs)
	t.ConfigIstio().ApplyYAMLOrFail(t, common.GetAppNamespace().Name(), config)
}

func deleteTelemetryResource(t framework.TestContext, expectLogs bool) {
	config := fmt.Sprintf(`apiVersion: telemetry.istio.io/v1alpha1
kind: Telemetry
metadata:
  name: logs
spec:
  accessLogging:
  - disabled: %v
`, !expectLogs)
	t.ConfigIstio().ApplyYAMLOrFail(t, common.GetAppNamespace().Name(), config)
}
