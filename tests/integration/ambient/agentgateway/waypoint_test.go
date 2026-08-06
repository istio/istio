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

package agentgateway

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ilabel "istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/util/retry"
)

const agentgatewayWaypointName = "agentgateway-waypoint"

// TestAgentgatewayWaypointTraffic deploys an agentgateway as a waypoint (via the
// istio-agentgateway-waypoint GatewayClass), binds a service to it, and verifies that captured
// traffic is redirected through the waypoint. Traversal is proven the same way the istio waypoint
// tests do it: an HTTPRoute injects a marker response header, and the request is confirmed to have
// been processed at L7.
func TestAgentgatewayWaypointTraffic(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			if !t.Settings().Agentgateway {
				t.Skip("Only run agentgateway tests when explicitly enabled")
			}
			// pre-req setup instead of using the full echo suite
			crd.DeployGatewayAPIOrSkip(t)
			testNs, client, server := setupSmallTrafficTest(t)

			// Deploy the agentgateway waypoint into the same namespace as the server.
			t.ConfigIstio().YAML(testNs.Name(), fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: %s
  namespace: %s
  annotations:
    networking.istio.io/service-type: ClusterIP
spec:
  gatewayClassName: istio-agentgateway-waypoint
  listeners:
  - allowedRoutes:
      namespaces:
        from: All
    name: mesh
    port: 15008
    protocol: HBONE
`, agentgatewayWaypointName, testNs.Name())).ApplyOrFail(t)

			retry.UntilSuccessOrFail(t, func() error {
				return checkWaypointIsReady(t, testNs.Name(), agentgatewayWaypointName)
			}, retry.Timeout(2*time.Minute))

			// Apply a route that adds a custom response header when the request goes through the
			// waypoint. This prevents the test from prematurely succeeding due to the configuration
			// not being processed before the request is sent.
			t.ConfigIstio().YAML(testNs.Name(), fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: httproute
  namespace: %s
spec:
  parentRefs:
    - name: server
      kind: Service
      group: ""
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: server
          port: 80
      filters:
        - type: ResponseHeaderModifier
          responseHeaderModifier:
            add:
              - name: X-Custom-Traversed-Waypoint
                value: %s
`, testNs.Name(), agentgatewayWaypointName)).ApplyOrFail(t)

			// Bind the server service to the agentgateway waypoint.
			svc, err := t.Clusters().Default().Kube().CoreV1().Services(server.NamespaceName()).Get(context.Background(), server.ServiceName(), metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			svc.Labels[ilabel.IoIstioUseWaypoint.Name] = agentgatewayWaypointName
			_, err = t.Clusters().Default().Kube().CoreV1().Services(server.NamespaceName()).Update(context.Background(), svc, metav1.UpdateOptions{})
			if err != nil {
				t.Fatal(err)
			}

			// Wait for the waypoint binding to be successfully applied to the service.
			retry.UntilSuccessOrFail(t, func() error {
				s, err := t.Clusters().Default().Kube().CoreV1().Services(server.NamespaceName()).Get(context.Background(), server.ServiceName(), metav1.GetOptions{})
				if err != nil {
					return err
				}
				for _, cond := range s.Status.Conditions {
					if cond.Type == string(model.WaypointBound) {
						if cond.Status == metav1.ConditionTrue {
							return nil
						}
						return fmt.Errorf("waypoint bound status does not equal true")
					}
				}
				return fmt.Errorf("waypoint condition not found on service")
			}, retry.Timeout(1*time.Minute))

			// Send a request from within the mesh and confirm it traversed the agentgateway waypoint.
			client.CallOrFail(t, echo.CallOptions{
				To:      server,
				Address: fmt.Sprintf("%s.%s.svc.cluster.local", server.ServiceName(), server.NamespaceName()),
				Port:    server.PortForName("http"),
				Scheme:  scheme.HTTP,
				Count:   5,
				Check: check.And(
					check.OK(),
					check.ResponseHeader("X-Custom-Traversed-Waypoint", agentgatewayWaypointName),
				),
			})
		})
}
