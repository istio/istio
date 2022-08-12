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

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestPiggyback(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).Features("usability.observability.proxy-status"). // TODO create new "agent-piggyback" feature
		RequiresSingleCluster().
		RequiresLocalControlPlane().
		RequireIstioVersion("1.10.0").
		Run(func(t framework.TestContext) {
			// Add retry loop to handle case when the pod has disconnected from Istio temporarily
			retry.UntilSuccessOrFail(t, func() error {
				out, _, err := t.Clusters()[0].PodExec(
					apps.A[0].WorkloadsOrFail(t)[0].PodName(),
					apps.A.Config().Namespace.Name(),
					"istio-proxy",
					"pilot-agent request --debug-port 15004 GET /debug/syncz")
				if err != nil {
					return fmt.Errorf("couldn't curl sidecar: %v", err)
				}
				dr := xdsapi.DiscoveryResponse{}
				if err := protomarshal.Unmarshal([]byte(out), &dr); err != nil {
					return fmt.Errorf("unmarshal: %v", err)
				}
				if dr.TypeUrl != "istio.io/debug/syncz" {
					return fmt.Errorf("the output doesn't contain expected typeURL: %s", out)
				}
				if len(dr.Resources) < 1 {
					return fmt.Errorf("the output didn't unmarshal as expected (no resources): %s", out)
				}
				if dr.Resources[0].TypeUrl != "type.googleapis.com/envoy.service.status.v3.ClientConfig" {
					return fmt.Errorf("resources[0] doesn't contain expected typeURL: %s", out)
				}
				return nil
			})
		})
}
