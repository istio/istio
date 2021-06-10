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
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/tests/integration/pilot/common"
)

func TestXFFGateway(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ingress.topology").
		Run(func(t framework.TestContext) {
			istio.PatchMeshConfig(t, i.Settings().SystemNamespace, t.Clusters(), `
meshConfig:
  defaultConfig:
    gatewayTopology:
      numTrustedProxies: 2`)
			for _, tt := range common.XFFGatewayCase(apps) {
				tt.Run(t, apps.Namespace.Name())
			}
		})
}
