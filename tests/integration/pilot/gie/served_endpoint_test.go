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
	"net"
	"strconv"
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

const (
	servedPoolPort = 8000
	// unservedPort is a port no pool endpoint listens on, so an endpoint address built from it
	// is absent from the cluster's endpoint set. It stands in for an endpoint belonging to a
	// different InferencePool - the case where a picker is consulted but its answer cannot be
	// used.
	unservedPort = 9999
)

// TestInferencePoolServedEndpoint verifies that the endpoint reported back to the endpoint picker
// on the response path is the endpoint that actually answered, not the one the picker asked for.
//
// Envoy's override_host policy skips an override address that is not in the target cluster's
// endpoint set and falls back to round robin. The picker has no other way to learn that its
// choice was discarded, so a served value that merely echoed the request would make any
// selected-vs-served assertion - including the one in the gateway-api-inference-extension
// conformance suite - vacuous: it could not fail on a broken control plane or a fixed one.
func TestInferencePoolServedEndpoint(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			cfg := istio.DefaultConfigOrFail(t, ctx)
			crd.DeployGatewayAPIOrSkip(ctx)
			crd.DeployGatewayAPIInferenceExtensionOrSkip(ctx)

			ns := namespace.NewOrFail(ctx, namespace.Config{
				Prefix: "inferencepool-served",
				Inject: true,
			})

			// Two replicas: with a single pod the endpoint the picker names and the endpoint that
			// answers are always the same address, so the two cases below cannot be told apart.
			var workload echo.Instance
			workloadConfig := echo.Config{
				Service:   "served-workload",
				Namespace: ns,
				Ports: echo.Ports{
					{
						Name:         "http-8000",
						Protocol:     protocol.HTTP,
						ServicePort:  servedPoolPort,
						WorkloadPort: servedPoolPort,
					},
				},
				Subsets: []echo.SubsetConfig{
					{
						Version:  "v1",
						Replicas: 2,
					},
				},
			}

			var client echo.Instance
			clientConfig := echo.Config{
				Service:   "served-client",
				Namespace: ns,
			}

			var epp echo.Instance
			eppConfig := echo.Config{
				Service:   "served-epp",
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

			deployment.New(ctx).
				With(&workload, workloadConfig).
				With(&client, clientConfig).
				With(&epp, eppConfig).
				BuildOrFail(ctx)

			inferencePoolManifest := fmt.Sprintf(`
apiVersion: inference.networking.k8s.io/v1
kind: InferencePool
metadata:
  name: served-pool
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
      number: %d
`, ns.Name(), servedPoolPort, workloadConfig.Service, eppConfig.Service, eppConfig.Ports[0].ServicePort)
			ctx.ConfigIstio().YAML(ns.Name(), inferencePoolManifest).ApplyOrFail(ctx)

			gatewayManifest := fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: served-gateway
  namespace: %s
  annotations:
    sidecar.istio.io/componentLogLevel: "ext_proc:debug,upstream:debug,router:debug"
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
  name: served-route
  namespace: %s
spec:
  parentRefs:
  - name: served-gateway
  hostnames:
  - "served.example.com"
  rules:
  - backendRefs:
    - group: inference.networking.k8s.io
      kind: InferencePool
      name: served-pool
      port: 80
`, ns.Name(), cfg.GatewayClassName, ns.Name())
			ctx.ConfigIstio().YAML(ns.Name(), gatewayManifest).ApplyOrFail(ctx)

			waitForGatewayProgrammed(ctx, ns.Name(), "served-gateway")

			// Pod name to endpoint address, so a response can be mapped back to the address the
			// served metadata should be reporting.
			endpoints := map[string]string{}
			retry.UntilSuccessOrFail(ctx, func() error {
				pods, err := ctx.Clusters().Default().Kube().CoreV1().Pods(ns.Name()).List(context.TODO(),
					metav1.ListOptions{LabelSelector: "app=" + workloadConfig.Service})
				if err != nil {
					return fmt.Errorf("failed to list workload pods: %v", err)
				}
				endpoints = map[string]string{}
				for _, pod := range pods.Items {
					if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
						endpoints[pod.Name] = net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(servedPoolPort))
					}
				}
				if len(endpoints) != 2 {
					return fmt.Errorf("expected 2 running workload pods with an IP, got %d", len(endpoints))
				}
				return nil
			}, retry.Timeout(60*time.Second))

			gatewayAddr := fmt.Sprintf("served-gateway-%s.%s.svc.cluster.local", cfg.GatewayClassName, ns.Name())

			var chosenPod, chosenEndpoint string
			for pod, ep := range endpoints {
				chosenPod, chosenEndpoint = pod, ep
				break
			}

			// The picker names an endpoint the pool has. override_host honours it, so the served
			// endpoint is the requested one and every response comes from that pod - enough
			// requests that landing there by round-robin luck is not a plausible explanation.
			ctx.NewSubTest("picker choice honoured").Run(func(ctx framework.TestContext) {
				retry.UntilSuccessOrFail(ctx, func() error {
					result, err := callGateway(client, gatewayAddr, chosenEndpoint)
					if err != nil {
						return err
					}
					for _, r := range result.Responses {
						served := r.ResponseHeaders.Get(endpoint.ServedEndpointHeader)
						if served != chosenEndpoint {
							return fmt.Errorf("served endpoint %q, want the requested %q", served, chosenEndpoint)
						}
						if r.Hostname != chosenPod {
							return fmt.Errorf("request served by pod %q, want %q", r.Hostname, chosenPod)
						}
					}
					return nil
				})
			})

			// The picker names an endpoint the pool does not have. override_host discards it and
			// falls back to round robin, so the served endpoint must be the endpoint that actually
			// answered - never the address that was asked for.
			ctx.NewSubTest("picker choice discarded").Run(func(ctx framework.TestContext) {
				host, _, err := net.SplitHostPort(chosenEndpoint)
				if err != nil {
					ctx.Fatalf("failed to split endpoint %q: %v", chosenEndpoint, err)
				}
				absent := net.JoinHostPort(host, strconv.Itoa(unservedPort))

				retry.UntilSuccessOrFail(ctx, func() error {
					result, err := callGateway(client, gatewayAddr, absent)
					if err != nil {
						return err
					}
					for _, r := range result.Responses {
						served := r.ResponseHeaders.Get(endpoint.ServedEndpointHeader)
						if served == "" {
							return fmt.Errorf("no %s header on the response; nothing reported the dialled host",
								endpoint.ServedEndpointHeader)
						}
						if served == absent {
							return fmt.Errorf("served endpoint %q echoes the discarded request; "+
								"the endpoint that answered was pod %q at %q", served, r.Hostname, endpoints[r.Hostname])
						}
						want, ok := endpoints[r.Hostname]
						if !ok {
							return fmt.Errorf("response came from unknown pod %q", r.Hostname)
						}
						if served != want {
							return fmt.Errorf("served endpoint %q, but pod %q at %q answered", served, r.Hostname, want)
						}
					}
					return nil
				})
			})
		})
}

// callGateway sends a batch of requests through the gateway, asking the picker for targetEndpoint.
// A batch rather than a single request so that a per-response assertion holds across whichever
// pod round robin lands on.
func callGateway(client echo.Instance, gatewayAddr, targetEndpoint string) (echo.CallResult, error) {
	result, err := client.Call(echo.CallOptions{
		Port: echo.Port{
			Name:        "http",
			Protocol:    protocol.HTTP,
			ServicePort: 80,
		},
		Scheme:  scheme.HTTP,
		Address: gatewayAddr,
		Count:   5,
		HTTP: echo.HTTP{
			Headers: headers.New().
				WithHost("served.example.com").
				With("x-endpoint", targetEndpoint).
				Build(),
		},
		Check: check.OK(),
	})
	if err != nil {
		return result, fmt.Errorf("failed to call gateway: %v", err)
	}
	if len(result.Responses) == 0 {
		return result, fmt.Errorf("no response received")
	}
	return result, nil
}
