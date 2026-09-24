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

package revisions

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireMultiPrimary().
		Label(label.Full).
		// Requires two CPs with specific names to be configured.
		Label(label.CustomSetup).
		Setup(istio.Setup(nil, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
revision: stable
`
		})).
		Setup(istio.Setup(nil, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
profile: empty
revision: canary
components:
  pilot:
    enabled: true
`
		})).
		Run()
}

// TestMultiRevision Sets up a simple client -> server call, where the client and server
// belong to different control planes.
func TestMultiRevision(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			stable := namespace.NewOrFail(t, namespace.Config{
				Prefix:   "stable",
				Inject:   true,
				Revision: "stable",
			})
			canary := namespace.NewOrFail(t, namespace.Config{
				Prefix:   "canary",
				Inject:   true,
				Revision: "canary",
			})

			echos := deployment.New(t).
				WithClusters(t.Clusters()...).
				WithConfig(echo.Config{
					Service:   "client",
					Namespace: stable,
					Ports:     []echo.Port{},
				}).
				WithConfig(echo.Config{
					Service:   "server",
					Namespace: canary,
					Ports: []echo.Port{
						{
							Name:         "http",
							Protocol:     protocol.HTTP,
							WorkloadPort: 8090,
						},
					},
				}).
				WithConfig(echo.Config{
					Service:    "vm",
					Namespace:  canary,
					DeployAsVM: true,
					Ports:      []echo.Port{},
				}).
				BuildOrFail(t)

			echotest.New(t, echos).
				ConditionallyTo(echotest.ReachableDestinations).
				ToMatch(match.ServiceName(echo.NamespacedName{Name: "server", Namespace: canary})).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					if from.Config().IsVM() {
						systemNamespace := istio.DefaultConfigOrFail(t, t).SystemNamespace
						t.Cleanup(func() {
							if !t.Failed() {
								return
							}
							workloads, err := from.Workloads()
							if err != nil {
								t.Logf("Unable to collect VM discovery diagnostics: %v", err)
								return
							}
							for _, w := range workloads {
								// VM agent logs are files inside the container, rather than its stdout.
								stdout, stderr, err := w.Cluster().PodExec(w.PodName(), from.NamespaceName(), "istio-proxy",
									"tail -n 100 /var/log/istio/istio.log /var/log/istio/istio.err.log")
								t.Logf("VM discovery diagnostics for %s/%s in %s (error: %v):\n%s\n%s",
									from.NamespaceName(), w.PodName(), w.Cluster().Name(), err, stdout, stderr)
								service, err := w.Cluster().Kube().CoreV1().Services(systemNamespace).Get(context.Background(), "istio-eastwestgateway", v1.GetOptions{})
								t.Logf("East-west gateway service (error: %v): %+v", err, service)
								gateways, err := w.Cluster().PodsForSelector(context.Background(), systemNamespace, "istio=eastwestgateway")
								if err != nil {
									t.Logf("Unable to find east-west gateway: %v", err)
									continue
								}
								for _, gateway := range gateways.Items {
									stdout, stderr, err := w.Cluster().PodExec(gateway.Name, gateway.Namespace, "istio-proxy",
										"pilot-agent request GET config_dump")
									t.Logf("East-west gateway %s labels=%v (error: %v):\n%s\n%s",
										gateway.Name, gateway.Labels, err, stdout, stderr)
								}
								services, err := w.Cluster().Istio().NetworkingV1().VirtualServices(v1.NamespaceAll).List(context.Background(), v1.ListOptions{})
								if err != nil {
									t.Logf("Unable to collect Istiod routing configuration: %v", err)
								} else {
									for _, service := range services.Items {
										t.Logf("VirtualService %s/%s: %s", service.Namespace, service.Name, service.Spec.String())
									}
								}
								configs, err := w.Cluster().Istio().NetworkingV1().Gateways(v1.NamespaceAll).List(context.Background(), v1.ListOptions{})
								if err != nil {
									t.Logf("Unable to collect gateway configuration: %v", err)
								} else {
									for _, config := range configs.Items {
										t.Logf("Gateway %s/%s created=%v deleting=%v: %s", config.Namespace, config.Name,
											config.CreationTimestamp, config.DeletionTimestamp, config.Spec.String())
										ns, err := w.Cluster().Kube().CoreV1().Namespaces().Get(context.Background(), config.Namespace, v1.GetOptions{})
										if err != nil {
											t.Logf("Unable to collect namespace %s: %v", config.Namespace, err)
										} else {
											t.Logf("Namespace %s deleting=%v status=%+v", ns.Name, ns.DeletionTimestamp, ns.Status)
										}
									}
								}
							}
						})
					}
					retry.UntilSuccessOrFail(t, func() error {
						result, err := from.Call(echo.CallOptions{
							To: to,
							Port: echo.Port{
								Name: "http",
							},
							Retry: echo.Retry{
								NoRetry: true,
							},
							Check: check.And(
								check.OK(),
								check.ReachedTargetClusters(t),
							),
						})
						return check.And(
							check.NoError(),
							check.OK()).Check(result, err)
					}, retry.Delay(time.Millisecond*100))
				})
		})
}

func TestMultiRevisionRouteStatusHandling(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			crd.DeployGatewayAPIOrSkip(t)
			cfg := istio.DefaultConfigOrFail(t, t)
			stable := namespace.NewOrFail(t, namespace.Config{
				Prefix:   "stable",
				Inject:   true,
				Revision: "stable",
			})
			canary := namespace.NewOrFail(t, namespace.Config{
				Prefix:   "canary",
				Inject:   true,
				Revision: "canary",
			})

			_ = deployment.New(t).
				WithClusters(t.Clusters()...).
				WithConfig(echo.Config{
					Service:   "client",
					Namespace: stable,
					Ports:     []echo.Port{},
				}).
				WithConfig(echo.Config{
					Service:   "server",
					Namespace: canary,
					Ports: []echo.Port{
						{
							Name:         "http",
							Protocol:     protocol.HTTP,
							WorkloadPort: 8090,
						},
					},
				}).
				BuildOrFail(t)

			t.ConfigIstio().Eval(canary.Name(), map[string]string{
				"namespace":    canary.Name(),
				"gatewayClass": cfg.GatewayClassName,
			}, `
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: test-gateway
  namespace: {{.namespace}}
spec:
  gatewayClassName: {{.gatewayClass}}
  listeners:
  - name: http
    hostname: "test.example.com"
    port: 80
    protocol: HTTP
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: test-route
  namespace: {{.namespace}}
spec:
  parentRefs:
  - name: test-gateway
  hostnames:
  - "test.example.com"
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /
    backendRefs:
    - name: server
      port: 8090
`).ApplyOrFail(t)

			getRoute := func() (int, error) {
				route, err := t.Clusters().Default().GatewayAPI().GatewayV1beta1().HTTPRoutes(canary.Name()).Get(context.Background(), "test-route", v1.GetOptions{})
				if err != nil {
					return 0, err
				}
				return len(route.Status.Parents), nil
			}

			// Wait for the status handler to populate parent status.
			retry.UntilSuccessOrFail(t, func() error {
				parentCount, err := getRoute()
				if err != nil {
					return err
				}
				if parentCount == 0 {
					return fmt.Errorf("waiting for httproute status parents")
				}
				return nil
			}, retry.Timeout(30*time.Second), retry.Delay(100*time.Millisecond))

			// Verify parent status is not cleared after it appears.
			retry.UntilSuccessOrFail(t, func() error {
				parentCount, err := getRoute()
				if err != nil {
					return err
				}
				if parentCount == 0 {
					return fmt.Errorf("httproute status was incorrectly overwritten")
				}
				return nil
			}, retry.Converge(10), retry.Delay(10*time.Millisecond))
		})
}
