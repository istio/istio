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

package gie

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/echo/server/endpoint"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

const poolPort = 8000

// TestInferencePoolPerPoolPicker verifies that a rule weighting traffic across two
// InferencePools consults each pool's own endpoint picker for that pool's share.
//
// The ext_proc override used to be built once per rule and attached at route level, so the
// last backendRef's picker scored every request the rule served (#61594). The weighted split
// stays correct in that state and every endpoint stays healthy, because the picker's answer
// names a host outside the cluster that was selected and override_host quietly falls back to
// round robin - so neither the traffic distribution nor the response codes reveal it. Only
// asking which picker answered does.
func TestInferencePoolPerPoolPicker(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			cfg := istio.DefaultConfigOrFail(t, ctx)
			crd.DeployGatewayAPIOrSkip(ctx)
			crd.DeployGatewayAPIInferenceExtensionOrSkip(ctx)

			ns := namespace.NewOrFail(ctx, namespace.Config{
				Prefix: "inferencepool-pickers",
				Inject: true,
			})

			workload := func(name string) echo.Config {
				return echo.Config{
					Service:   name,
					Namespace: ns,
					Ports: echo.Ports{
						{
							Name:         "http",
							Protocol:     protocol.HTTP,
							ServicePort:  poolPort,
							WorkloadPort: poolPort,
						},
					},
				}
			}
			picker := func(name string) echo.Config {
				return echo.Config{
					Service:   name,
					Namespace: ns,
					Ports: echo.Ports{
						{
							Name:           "grpc",
							Protocol:       protocol.GRPC,
							ServicePort:    9002,
							WorkloadPort:   9002,
							EndpointPicker: true,
						},
					},
					Subsets: []echo.SubsetConfig{
						{
							Version: "v1",
							// Under GatewayAPIOnly mode the injection webhook is not deployed, so no
							// sidecar gets injected here.
							Annotations: map[string]string{
								"sidecar.istio.io/inject":                      "false",
								"traffic.sidecar.istio.io/excludeInboundPorts": "9002",
							},
						},
					},
				}
			}

			var workloadA, workloadB, pickerA, pickerB, client echo.Instance
			workloadAConfig, workloadBConfig := workload("pool-a-workload"), workload("pool-b-workload")
			pickerAConfig, pickerBConfig := picker("picker-a"), picker("picker-b")
			clientConfig := echo.Config{Service: "pools-client", Namespace: ns}

			deployment.New(ctx).
				With(&workloadA, workloadAConfig).
				With(&workloadB, workloadBConfig).
				With(&pickerA, pickerAConfig).
				With(&pickerB, pickerBConfig).
				With(&client, clientConfig).
				BuildOrFail(ctx)

			// picker that must answer for traffic served by each pool's workload
			pickerForWorkload := map[string]string{
				workloadAConfig.Service: pickerAConfig.Service,
				workloadBConfig.Service: pickerBConfig.Service,
			}

			poolManifest := func(pool, app, epp string) string {
				return fmt.Sprintf(`
apiVersion: inference.networking.k8s.io/v1
kind: InferencePool
metadata:
  name: %s
  namespace: %s
spec:
  targetPorts:
  - number: %d
  selector:
    matchLabels:
      app: %s
  endpointPickerRef:
    name: %s
    port:
      number: 9002
`, pool, ns.Name(), poolPort, app, epp)
			}
			ctx.ConfigIstio().
				YAML(ns.Name(), poolManifest("pool-a", workloadAConfig.Service, pickerAConfig.Service)).
				YAML(ns.Name(), poolManifest("pool-b", workloadBConfig.Service, pickerBConfig.Service)).
				ApplyOrFail(ctx)

			// One rule, both pools, equal weights: the rule is what carried a single picker for
			// every backendRef, so the two pools have to share a rule for this to mean anything.
			gatewayManifest := fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: pools-gateway
  namespace: %s
spec:
  gatewayClassName: %s
  listeners:
  - name: http
    port: 80
    protocol: HTTP
    allowedRoutes:
      namespaces:
        from: Same
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: pools-route
  namespace: %s
spec:
  parentRefs:
  - name: pools-gateway
  hostnames:
  - "pools.example.com"
  rules:
  - backendRefs:
    - group: inference.networking.k8s.io
      kind: InferencePool
      name: pool-a
      port: 80
      weight: 50
    - group: inference.networking.k8s.io
      kind: InferencePool
      name: pool-b
      port: 80
      weight: 50
`, ns.Name(), cfg.GatewayClassName, ns.Name())
			ctx.ConfigIstio().YAML(ns.Name(), gatewayManifest).ApplyOrFail(ctx)

			waitForGatewayProgrammed(ctx, ns.Name(), "pools-gateway")

			// Pod name to its app label, so a response and a picker header can both be resolved
			// back to the pool they belong to.
			appByPod := map[string]string{}
			retry.UntilSuccessOrFail(ctx, func() error {
				pods, err := ctx.Clusters().Default().Kube().CoreV1().Pods(ns.Name()).List(
					context.TODO(), metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to list pods: %v", err)
				}
				appByPod = map[string]string{}
				for _, pod := range pods.Items {
					if pod.Status.Phase == corev1.PodRunning {
						appByPod[pod.Name] = pod.Labels["app"]
					}
				}
				for _, want := range []string{
					workloadAConfig.Service, workloadBConfig.Service,
					pickerAConfig.Service, pickerBConfig.Service,
				} {
					found := false
					for _, app := range appByPod {
						if app == want {
							found = true
							break
						}
					}
					if !found {
						return fmt.Errorf("no running pod for %q yet", want)
					}
				}
				return nil
			}, retry.Timeout(60*time.Second))

			gatewayAddr := fmt.Sprintf("pools-gateway-%s.%s.svc.cluster.local", cfg.GatewayClassName, ns.Name())

			// No x-endpoint header: each picker falls back to its built-in default, which belongs
			// to neither pool, so every request round robins within whichever pool the weights
			// chose. Which picker ran is the only thing under test here.
			retry.UntilSuccessOrFail(ctx, func() error {
				result, err := client.Call(echo.CallOptions{
					Port: echo.Port{
						Name:        "http",
						Protocol:    protocol.HTTP,
						ServicePort: 80,
					},
					Scheme:  scheme.HTTP,
					Address: gatewayAddr,
					Count:   20,
					HTTP: echo.HTTP{
						Headers: headers.New().WithHost("pools.example.com").Build(),
					},
					Check: check.OK(),
				})
				if err != nil {
					return fmt.Errorf("failed to call gateway: %v", err)
				}

				served := map[string]int{}
				for _, r := range result.Responses {
					servedApp, ok := appByPod[r.Hostname]
					if !ok {
						return fmt.Errorf("response came from unknown pod %q", r.Hostname)
					}
					wantPicker, ok := pickerForWorkload[servedApp]
					if !ok {
						return fmt.Errorf("response came from unexpected workload %q (pod %q)", servedApp, r.Hostname)
					}
					served[servedApp]++

					pickerPod := r.ResponseHeaders.Get(endpoint.PickerIDHeader)
					if pickerPod == "" {
						return fmt.Errorf("no %s header on the response; no picker was consulted",
							endpoint.PickerIDHeader)
					}
					gotPicker, ok := appByPod[pickerPod]
					if !ok {
						return fmt.Errorf("unknown picker pod %q answered", pickerPod)
					}
					if gotPicker != wantPicker {
						return fmt.Errorf(
							"picker %q answered for a request served by %q (pod %q); each pool's own "+
								"picker must score the traffic routed to that pool, expected %q",
							gotPicker, servedApp, r.Hostname, wantPicker)
					}
				}

				// Both pools have to be exercised, or the attribution check above proves nothing.
				for workloadName := range pickerForWorkload {
					if served[workloadName] == 0 {
						return fmt.Errorf("no request reached %q across %d attempts; weights did not split",
							workloadName, len(result.Responses))
					}
				}
				return nil
			})
		})
}
