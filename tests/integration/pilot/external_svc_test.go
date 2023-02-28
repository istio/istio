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
	"net/http"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
)

const (
	SidecarScope = `apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: restrict-external-service
spec:
  workloadSelector:
    labels:
      app: a
  outboundTrafficPolicy:
    mode: "REGISTRY_ONLY"
`
)

// We want to test "external" traffic. To do this let us enable outboundTrafficPolicy REGISTRY_ONLY
// on one of the workloads, to verify selective external connectivity
func createSidecarScope(t framework.TestContext, appsNamespace string) {
	if err := t.ConfigIstio().YAML(appsNamespace, SidecarScope).Apply(apply.NoCleanup); err != nil {
		t.Errorf("failed to apply sidecar: %v", err)
	}
}

func TestExternalService(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.reachability").
		Run(func(t framework.TestContext) {
			testCases := []struct {
				name       string
				statusCode int
				from       echo.Instances
				to         string
				protocol   protocol.Instance
				port       int
			}{
				// Test connectivity to external service from outboundTrafficPolicy restricted pod.
				// The external service is exposed through a ServiceEntry, so the traffic should go through
				{
					name:       "traffic from outboundTrafficPolicy REGISTRY_ONLY to allowed host",
					statusCode: http.StatusOK,
					from:       apps.EchoNamespace.A,
					to:         "fake.external.com",
					protocol:   protocol.HTTPS,
					port:       443,
				},
				// Test connectivity to external service from outboundTrafficPolicy restricted pod.
				// Since there is no ServiceEntry created for example.com the traffic should fail.
				{
					name:       "traffic from outboundTrafficPolicy REGISTRY_ONLY to not allowed host",
					statusCode: http.StatusBadGateway,
					from:       apps.EchoNamespace.A,
					to:         "example.com",
					protocol:   protocol.HTTP,
					port:       80,
				},
				// Test connectivity to external service from outboundTrafficPolicy=PASS_THROUGH pod.
				// Traffic should go through without the need for any explicit ServiceEntry
				{
					name:       "traffic from outboundTrafficPolicy PASS_THROUGH to not allowed host",
					statusCode: http.StatusOK,
					from:       apps.EchoNamespace.B,
					to:         "example.com",
					protocol:   protocol.HTTP,
					port:       80,
				},
			}
			createSidecarScope(t, apps.EchoNamespace.Namespace.Name())
			for _, tc := range testCases {
				t.NewSubTestf("%v to host %v", tc.from[0].NamespacedName(), tc.to).Run(func(t framework.TestContext) {
					opts := echo.CallOptions{
						Address: tc.to,
						Port:    echo.Port{Protocol: tc.protocol, ServicePort: tc.port},
						Check: check.And(
							check.Status(tc.statusCode),
						),
					}
					tc.from[0].CallOrFail(t, opts)
				})
			}
		})
}
