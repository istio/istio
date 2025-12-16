//go:build integ

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

package ambientconformance

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/ambient"
	"istio.io/istio/tests/integration/security/util/cert"
)

var (
	i istio.Instance

	// Below are various preconfigured echo deployments. Whenever possible, tests should utilize these
	// to avoid excessive creation/tear down of deployments. In general, a test should only deploy echo if
	// its doing something unique to that specific test.
	apps = &ambient.EchoDeployments{}

	// used to validate telemetry in-cluster
	prom prometheus.Instance
)

const (
	ambientControlPlaneValues = `
values:
  pilot:
    env:
      # Note: support is alpha and env var is tightly scoped
      ENABLE_WILDCARD_HOST_SERVICE_ENTRIES_FOR_TLS: "true"
  cni:
    # The CNI repair feature is disabled for these tests because this is a controlled environment,
    # and it is important to catch issues that might otherwise be automatically fixed.
    # Refer to issue #49207 for more context.
    repair:
      enabled: false
  ztunnel:
    terminationGracePeriodSeconds: 5
    env:
      SECRET_TTL: 5m
    podLabels:
      networking.istio.io/tunnel: "http"
`
)

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireMinVersion(24).
		Setup(func(t resource.Context) error {
			t.Settings().Ambient = true
			return nil
		}).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			// can't deploy VMs without eastwest gateway
			ctx.Settings().SkipVMs()
			cfg.EnableCNI = true
			cfg.DeployEastWestGW = false
			cfg.ControlPlaneValues = ambientControlPlaneValues

			cfg.Values = map[string]string{
				"pilot.env.ENABLE_GATEWAY_API_INFERENCE_EXTENSION": "true",
			}
		}, cert.CreateCASecretAlt)).
		Setup(func(t resource.Context) error {
			gatewayInferenceConformanceInputs.Cluster = t.Clusters().Default()
			gatewayInferenceConformanceInputs.Client = t.Clusters().Default()
			gatewayInferenceConformanceInputs.Cleanup = !t.Settings().NoCleanup

			return nil
		}).
		SetupParallel(
			func(t resource.Context) error {
				return ambient.TestRegistrySetup(apps, t)
			},
			func(t resource.Context) error {
				return ambient.SetupApps(t, i, apps)
			},
			func(t resource.Context) (err error) {
				prom, err = prometheus.New(t, prometheus.Config{})
				if err != nil {
					return err
				}
				return err
			},
		).
		Run()
}
