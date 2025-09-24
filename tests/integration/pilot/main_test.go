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

package pilot

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	i istio.Instance

	// Below are various preconfigured echo deployments. Whenever possible, tests should utilize these
	// to avoid excessive creation/tear down of deployments. In general, a test should only deploy echo if
	// its doing something unique to that specific test.
	apps = deployment.SingleNamespaceView{}
)

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			cfg.Values = map[string]string{
				"pilot.env.ENABLE_GATEWAY_API_INFERENCE_EXTENSION": "true",
			}
		})).
		Setup(deployment.SetupSingleNamespace(&apps, deployment.Config{})).
		Setup(func(t resource.Context) error {
			gatewayConformanceInputs.Client = t.Clusters().Default()
			gatewayConformanceInputs.Cleanup = !t.Settings().NoCleanup

			gatewayInferenceConformanceInputs.Client = t.Clusters().Default()
			gatewayInferenceConformanceInputs.Cleanup = !t.Settings().NoCleanup

			return nil
		}).
		Run()
}
