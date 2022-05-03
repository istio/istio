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
	"fmt"
	"testing"

	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/pilot/common"
)

var (
	i istio.Instance

	// Below are various preconfigured echo deployments. Whenever possible, tests should utilize these
	// to avoid excessive creation/tear down of deployments. In general, a test should only deploy echo if
	// its doing something unique to that specific test.
	apps = deployment.SingleNamespaceView{}
)

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireMultiPrimary().
		RequireMinVersion(17).
		Setup(istio.Setup(&i, func(t resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = fmt.Sprintf(`
values:
  pilot:
    env:
      PILOT_USE_ENDPOINT_SLICE: "%v"`,
				// for k8s 1.21+, this suite should test disabling EndpointSlice mode
				kubelib.IsLessThanVersion(t.Clusters().Kube().Default(), 21))
		})).
		Setup(deployment.SetupSingleNamespace(&apps, deployment.Config{})).
		Run()
}

func TestTraffic(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.routing", "traffic.reachability", "traffic.shifting").
		Run(func(t framework.TestContext) {
			common.RunAllTrafficTests(t, i, &apps)
		})
}
