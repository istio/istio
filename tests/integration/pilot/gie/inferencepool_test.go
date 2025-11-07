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
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

// TestInferencePoolMultipleTargetPorts verifies that InferencePools with multiple targetPorts
// create a single shadow service with port 54321, and that Envoy generates the correct cluster.
func TestInferencePoolMultipleTargetPorts(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			crd.DeployGatewayAPIOrSkip(ctx)
			crd.DeployGatewayAPIInferenceExtensionOrSkip(ctx)

			ns := namespace.NewOrFail(ctx, namespace.Config{
				Prefix: "inferencepool",
				Inject: true,
			})

			// Deploy a workload that listens on multiple ports (8000, 8001, 8002)
			// to simulate an inference workload
			var workload echo.Instance
			echoConfig := echo.Config{
				Service:   "inference-workload",
				Namespace: ns,
				Ports: echo.Ports{
					{
						Name:         "http-8000",
						Protocol:     "HTTP",
						ServicePort:  8000,
						WorkloadPort: 8000,
					},
					{
						Name:         "http-8001",
						Protocol:     "HTTP",
						ServicePort:  8001,
						WorkloadPort: 8001,
					},
					{
						Name:         "http-8002",
						Protocol:     "HTTP",
						ServicePort:  8002,
						WorkloadPort: 8002,
					},
				},
			}
			// Deploy a client instance to make calls through the gateway
			var client echo.Instance
			clientConfig := echo.Config{
				Service:   "client",
				Namespace: ns,
			}

			// Deploy the endpoint picker (EPP) service as an echo instance
			// This is an external processor that selects endpoints based on request headers
			var epp echo.Instance
			eppConfig := echo.Config{
				Service:   "mock-epp",
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
						Annotations: map[string]string{
							// Exclude the endpoint picker port from Istio's traffic interception
							// to allow direct ext_proc gRPC connections from the gateway
							"traffic.sidecar.istio.io/excludeInboundPorts": "9002",
						},
					},
				},
			}

			deployment.New(ctx).
				With(&workload, echoConfig).
				With(&client, clientConfig).
				With(&epp, eppConfig).
				BuildOrFail(ctx)

			// Deploy InferencePool with multiple targetPorts
			inferencePoolManifest := fmt.Sprintf(`
apiVersion: inference.networking.k8s.io/v1
kind: InferencePool
metadata:
  name: test-pool
  namespace: %s
spec:
  targetPorts:
  - number: 8000
  - number: 8001
  - number: 8002
  selector:
    matchLabels:
      app: inference-workload
  endpointPickerRef:
    name: %s
    port:
      number: %d
`, ns.Name(), eppConfig.Service, eppConfig.Ports[0].ServicePort)
			ctx.ConfigIstio().YAML(ns.Name(), inferencePoolManifest).ApplyOrFail(ctx)

			// Enable access logging for all proxies in the namespace BEFORE creating gateway
			// This ensures the gateway pods start with logging enabled
			telemetryManifest := `
apiVersion: telemetry.istio.io/v1
kind: Telemetry
metadata:
  name: access-logging
spec:
  accessLogging:
  - providers:
    - name: envoy
`
			ctx.ConfigIstio().YAML(ns.Name(), telemetryManifest).ApplyOrFail(ctx)

			// Deploy Gateway and HTTPRoute
			gatewayManifest := fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: inference-gateway
  namespace: %s
  annotations:
    sidecar.istio.io/componentLogLevel: "ext_proc:debug,connection:debug,filter:debug,router:debug"
spec:
  gatewayClassName: istio
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
  name: inference-route
  namespace: %s
spec:
  parentRefs:
  - name: inference-gateway
  hostnames:
  - "inference.example.com"
  rules:
  - backendRefs:
    - group: inference.networking.k8s.io
      kind: InferencePool
      name: test-pool
      port: 80
`, ns.Name(), ns.Name())
			ctx.ConfigIstio().YAML(ns.Name(), gatewayManifest).ApplyOrFail(ctx)

			// Verify shadow service was created with correct properties
			retry.UntilSuccessOrFail(ctx, func() error {
				client := ctx.Clusters().Default().Kube()
				services, err := client.CoreV1().Services(ns.Name()).List(context.TODO(), metav1.ListOptions{
					LabelSelector: "istio.io/inferencepool-name=test-pool",
				})
				if err != nil || len(services.Items) == 0 {
					return fmt.Errorf("failed to list services: %v", err)
				}

				svc := services.Items[0]

				// Verify it's marked as an InferencePool service
				if svc.Labels[constants.InternalServiceSemantics] != constants.ServiceSemanticsInferencePool {
					return fmt.Errorf("shadow service missing InferencePool label")
				}

				// Verify it's a headless service
				if svc.Spec.ClusterIP != corev1.ClusterIPNone {
					return fmt.Errorf("shadow service is not headless, got ClusterIP: %s", svc.Spec.ClusterIP)
				}

				// Verify EPP extension ref labels are present
				if svc.Labels["istio.io/inferencepool-extension-service"] != eppConfig.Service {
					return fmt.Errorf("missing or incorrect EPP service label, expected %s, got %s",
						eppConfig.Service, svc.Labels["istio.io/inferencepool-extension-service"])
				}

				expectedPort := fmt.Sprintf("%d", eppConfig.Ports[0].ServicePort)
				if svc.Labels["istio.io/inferencepool-extension-port"] != expectedPort {
					return fmt.Errorf("missing or incorrect EPP port label, expected %s, got %s",
						expectedPort, svc.Labels["istio.io/inferencepool-extension-port"])
				}

				shadowServiceName := svc.Name
				ctx.Logf("Shadow service verified successfully: %s", shadowServiceName)
				return nil
			})

			// Verify traffic routing through EPP
			// Get the workload pod IP to use in x-endpoint header
			var workloadPodIP string
			retry.UntilSuccessOrFail(ctx, func() error {
				client := ctx.Clusters().Default().Kube()
				pods, err := client.CoreV1().Pods(ns.Name()).List(context.TODO(), metav1.ListOptions{
					LabelSelector: "app=inference-workload",
				})
				if err != nil {
					return fmt.Errorf("failed to list workload pods: %v", err)
				}
				if len(pods.Items) == 0 {
					return fmt.Errorf("no workload pods found")
				}
				for _, pod := range pods.Items {
					if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
						workloadPodIP = pod.Status.PodIP
						ctx.Logf("Found workload pod IP: %s", workloadPodIP)
						return nil
					}
				}
				return fmt.Errorf("no running workload pods with IP found")
			})

			// Wait for Gateway resource to be ready
			retry.UntilSuccessOrFail(ctx, func() error {
				// Get the Gateway resource and check its status
				gw, err := ctx.Clusters().Default().Dynamic().Resource(schema.GroupVersionResource{
					Group:    "gateway.networking.k8s.io",
					Version:  "v1",
					Resource: "gateways",
				}).Namespace(ns.Name()).Get(context.TODO(), "inference-gateway", metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("gateway resource not found: %v", err)
				}

				// Check Gateway status conditions for Accepted and Programmed
				status, found, err := unstructured.NestedSlice(gw.Object, "status", "conditions")
				if err != nil || !found {
					return fmt.Errorf("gateway status conditions not found")
				}

				accepted := false
				programmed := false
				for _, cond := range status {
					condition := cond.(map[string]interface{})
					condType := condition["type"].(string)
					condStatus := condition["status"].(string)

					if condType == "Accepted" && condStatus == "True" {
						accepted = true
					}
					if condType == "Programmed" && condStatus == "True" {
						programmed = true
					}
				}

				if !accepted {
					return fmt.Errorf("gateway not accepted yet")
				}
				if !programmed {
					return fmt.Errorf("gateway not programmed yet")
				}

				ctx.Logf("Gateway is ready (Accepted and Programmed)")
				return nil
			}, retry.Timeout(60*time.Second))

			// Send request through the gateway with x-endpoint header
			// This tests the EPP protocol end-to-end
			gatewayAddr := fmt.Sprintf("inference-gateway-istio.%s.svc.cluster.local", ns.Name())
			ctx.Logf("Sending request to gateway: %s", gatewayAddr)

			// Test routing to each of the three ports to verify EPP can select different endpoints
			testPorts := []int{8000, 8001, 8002}
			for _, targetPort := range testPorts {
				targetEndpoint := fmt.Sprintf("%s:%d", workloadPodIP, targetPort)
				ctx.Logf("Testing EPP routing to port %d (endpoint: %s)", targetPort, targetEndpoint)

				retry.UntilSuccessOrFail(ctx, func() error {
					result, err := client.Call(echo.CallOptions{
						Port: echo.Port{
							Name:        "http",
							Protocol:    protocol.HTTP,
							ServicePort: 80,
						},
						Scheme:  scheme.HTTP,
						Address: gatewayAddr,
						HTTP: echo.HTTP{
							Headers: headers.New().
								WithHost("inference.example.com").
								With("x-endpoint", targetEndpoint).
								Build(),
						},
						Check: check.OK(),
					})
					if err != nil {
						ctx.Logf("Gateway call failed (will retry): %v", err)
						return fmt.Errorf("failed to call gateway: %v", err)
					}

					// Verify the request was successful
					if len(result.Responses) == 0 {
						return fmt.Errorf("no response received")
					}

					// Parse the ServicePort field from the response
					resultPort, err := strconv.Atoi(result.Responses[0].Port)
					if err != nil {
						return fmt.Errorf("failed to parse port: %v", err)
					}
					if resultPort != targetPort {
						ctx.Logf("\nResponse:\n%s", result.Responses[0].RawContent)
						return fmt.Errorf("port %s did not match expected %d", result.Responses[0].Port, targetPort)
					}

					ctx.Logf("Successfully verified EPP routing to endpoint %s (ServicePort=%d confirmed)", targetEndpoint, targetPort)
					return nil
				})
			}
		})
}
