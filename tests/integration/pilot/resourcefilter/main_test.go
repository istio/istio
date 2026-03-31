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

package resourcefilter

import (
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	i    istio.Instance
	apps = deployment.SingleNamespaceView{}
)

// WARNING: Do not remove virtualservices and gateways from include list, as they are required
// for multicluster test to pass
func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			includeResources := []string{"wasmplugins.extensions.istio.io"}

			// On multicluster tests virtualservices and gateways are required to pass
			if ctx.Environment().IsMultiCluster() {
				includeResources = append(includeResources, "virtualservices.networking.istio.io", "gateways.networking.istio.io")
			}
			cfg.ControlPlaneValues = fmt.Sprintf(`
values:
  pilot:
    env:
      PILOT_INCLUDE_RESOURCES: "%s"
      PILOT_IGNORE_RESOURCES: "*.istio.io"
`, strings.Join(includeResources, ","))
		})).
		Setup(deployment.SetupSingleNamespace(&apps, deployment.Config{})).
		Run()
}
