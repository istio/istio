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

package ctb

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	clustertbI    istio.Instance
	clustertbApps = deployment.SingleNamespaceView{}
)

// TestMain defines the entrypoint for ClusterTrustBundle tests using a custom Istio installation
// with ClusterTrustBundle support enabled for issues #56676 and #56675.
func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(&clustertbI, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  pilot:
    env:
      PILOT_ENABLE_CLUSTERTB_WORKLOAD_ENTRIES: true
      EXTERNAL_ISTIOD: true
components:
  pilot:
    k8s:
      env:
        - name: PILOT_ENABLE_CLUSTERTB_WORKLOAD_ENTRIES
          value: "true"
        - name: EXTERNAL_ISTIOD
          value: "true"
`
		})).
		Setup(deployment.SetupSingleNamespace(&clustertbApps, deployment.Config{})).
		Run()
}
