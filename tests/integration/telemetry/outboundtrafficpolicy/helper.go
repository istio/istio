//go:build integ
// +build integ

// Copyright Istio Authors. All Rights Reserved.
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

package outboundtrafficpolicy

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	tmpl "istio.io/istio/pkg/test/util/tmpl"
	promtest "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

const (
	// ServiceEntry is used to create conflicts on various ports
	// As defined below, the tcp-conflict and https-conflict ports are 9443 and 9091
	ServiceEntry = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: http
spec:
  hosts:
  - istio.io
  location: MESH_EXTERNAL
  ports:
  - name: http-for-https
    number: 9443
    protocol: HTTP
  - name: http-for-tcp
    number: 9091
    protocol: HTTP
  resolution: DNS
`
	SidecarScope = `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: restrict-to-service-entry-namespace
spec:
  egress:
  - hosts:
    - "{{.ImportNamespace}}/*"
    - "istio-system/*"
  outboundTrafficPolicy:
    mode: "{{.TrafficPolicyMode}}"
`

	Gateway = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: istio-egressgateway
spec:
  selector:
    istio: egressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "some-external-site.com"
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route-via-egressgateway
spec:
  hosts:
    - "some-external-site.com"
  gateways:
  - istio-egressgateway
  - mesh
  http:
    - match:
      - gateways:
        - mesh # from sidecars, route to egress gateway service
        port: 80
      route:
      - destination:
          host: istio-egressgateway.istio-system.svc.cluster.local
          port:
            number: 80
        weight: 100
    - match:
      - gateways:
        - istio-egressgateway
        port: 80
      route:
      - destination:
          host: some-external-site.com
      headers:
        request:
          add:
            handled-by-egress-gateway: "true"
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: ext-service-entry
spec:
  hosts:
  - "some-external-site.com"
  location: MESH_EXTERNAL
  endpoints:
  - address: destination.{{.AppNamespace}}.svc.cluster.local
    network: external
  ports:
  - number: 80
    name: http
  resolution: DNS
`
)

// TestCase represents what is being tested
type TestCase struct {
	Name     string
	PortName string
	HTTP2    bool
	Host     string
	Expected Expected
}

// Expected contains the metric and query to run against
// prometheus to validate that expected telemetry information was gathered;
// as well as the http response code
type Expected struct {
	Query          prometheus.Query
	StatusCode     int
	Protocol       string
	RequestHeaders map[string]string
}

// TrafficPolicy is the mode of the outbound traffic policy to use
// when configuring the sidecar for the client
type TrafficPolicy string

const (
	AllowAny     TrafficPolicy = "ALLOW_ANY"
	RegistryOnly TrafficPolicy = "REGISTRY_ONLY"
)

// String implements fmt.Stringer
func (t TrafficPolicy) String() string {
	return string(t)
}

// We want to test "external" traffic. To do this without actually hitting an external endpoint,
// we can import only the service namespace, so the apps are not known
func createSidecarScope(t framework.TestContext, tPolicy TrafficPolicy, appsNamespace namespace.Instance, serviceNamespace namespace.Instance) {
	args := map[string]string{"ImportNamespace": serviceNamespace.Name(), "TrafficPolicyMode": tPolicy.String()}
	if err := t.ConfigIstio().Eval(args, SidecarScope).Apply(appsNamespace.Name()); err != nil {
		t.Errorf("failed to apply service entries: %v", err)
	}
}

func mustReadCert(t framework.TestContext, f string) string {
	t.Helper()
	b, err := os.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs", f))
	if err != nil {
		t.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}

// We want to test "external" traffic. To do this without actually hitting an external endpoint,
// we can import only the service namespace, so the apps are not known
func createGateway(t framework.TestContext, appsNamespace namespace.Instance, serviceNamespace namespace.Instance) {
	t.Helper()
	b := tmpl.EvaluateOrFail(t, Gateway, map[string]string{"AppNamespace": appsNamespace.Name()})
	if err := t.ConfigIstio().YAML(b).Apply(serviceNamespace.Name()); err != nil {
		t.Fatalf("failed to apply gateway: %v. template: %v", err, b)
	}
}

// TODO support native environment for registry only/gateway. Blocked by #13177 because the listeners for native use static
// routes and this test relies on the dynamic routes sent through pilot to allow external traffic.

func RunExternalRequest(t *testing.T, cases []*TestCase, prometheus prometheus.Instance, mode TrafficPolicy) {
	t.Helper()
	// Testing of Blackhole and Passthrough clusters:
	// Setup of environment:
	// 1. client and destination are deployed to app-1-XXXX namespace
	// 2. client is restricted to talk to destination via Sidecar scope where outbound policy is set (ALLOW_ANY, REGISTRY_ONLY)
	//    and clients' egress can only be to service-2-XXXX/* and istio-system/*
	// 3. a namespace service-2-YYYY is created
	// 4. A gateway is put in service-2-YYYY where its host is set for some-external-site.com on port 80 and 443
	// 3. a VirtualService is also created in service-2-XXXX to:
	//    a) route requests for some-external-site.com to the istio-egressgateway
	//       * if the request on port 80, then it will add an http header `handled-by-egress-gateway`
	//    b) from the egressgateway it will forward the request to the destination pod deployed in the app-1-XXX
	//       namespace

	// Test cases:
	// 1. http case:
	//    client -------> Hits listener 0.0.0.0_80 cluster
	//    Metric is istio_requests_total i.e. HTTP
	//
	// 2. https case:
	//    client ----> Hits no listener -> 0.0.0.0_150001 -> ALLOW_ANY/REGISTRY_ONLY
	//    Metric is istio_tcp_connections_closed_total i.e. TCP
	//
	// 3. https conflict case:
	//    client ----> Hits listener 0.0.0.0_9443
	//    Metric is istio_tcp_connections_closed_total i.e. TCP
	//
	// 4. http_egress
	//    client ) ---HTTP request (Host: some-external-site.com----> Hits listener 0.0.0.0_80 ->
	//      VS Routing (add Egress Header) --> Egress Gateway --> destination
	//    Metric is istio_requests_total i.e. HTTP with destination as destination
	//
	// 5. TCP
	//    client ---TCP request at port 9090----> Matches no listener -> 0.0.0.0_150001 -> ALLOW_ANY/REGISTRY_ONLY
	//    Metric is istio_tcp_connections_closed_total i.e. TCP
	//
	// 5. TCP conflict
	//    client ---TCP request at port 9091 ----> Hits listener 0.0.0.0_9091 ->  ALLOW_ANY/REGISTRY_ONLY
	//    Metric is istio_tcp_connections_closed_total i.e. TCP
	//
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			client, to := setupEcho(t, mode)

			for _, tc := range cases {
				t.NewSubTest(tc.Name).Run(func(t framework.TestContext) {
					client.CallOrFail(t, echo.CallOptions{
						To: to,
						Port: echo.Port{
							Name: tc.PortName,
						},
						HTTP: echo.HTTP{
							HTTP2:   tc.HTTP2,
							Headers: headers.New().WithHost(tc.Host).Build(),
						},
						Check: func(rs echoClient.Responses, err error) error {
							// the expected response from a blackhole test case will have err
							// set; use the length of the expected code to ignore this condition
							if err != nil && tc.Expected.StatusCode > 0 {
								return fmt.Errorf("request failed: %v", err)
							}
							codeStr := strconv.Itoa(tc.Expected.StatusCode)
							for i, r := range rs {
								if codeStr != r.Code {
									return fmt.Errorf("response[%d] received status code %s, expected %d", i, r.Code, tc.Expected.StatusCode)
								}
								for k, v := range tc.Expected.RequestHeaders {
									if got := r.RequestHeaders.Get(k); got != v {
										return fmt.Errorf("expected metadata %v=%v, got %q", k, v, got)
									}
								}
							}
							return nil
						},
					})

					if tc.Expected.Query.Metric != "" {
						promtest.ValidateMetric(t, t.Clusters().Default(), prometheus, tc.Expected.Query, 1)
					}
				})
			}
		})
}

func setupEcho(t framework.TestContext, mode TrafficPolicy) (echo.Instance, echo.Target) {
	t.Helper()
	appsNamespace := namespace.NewOrFail(t, t, namespace.Config{
		Prefix: "app",
		Inject: true,
	})
	serviceNamespace := namespace.NewOrFail(t, t, namespace.Config{
		Prefix: "service",
		Inject: true,
	})

	// External traffic should work even if we have service entries on the same ports
	createSidecarScope(t, mode, appsNamespace, serviceNamespace)

	var client, dest echo.Instance
	deployment.New(t).
		With(&client, echo.Config{
			Service:   "client",
			Namespace: appsNamespace,
			Subsets:   []echo.SubsetConfig{{}},
		}).
		With(&dest, echo.Config{
			Service:   "destination",
			Namespace: appsNamespace,
			Subsets:   []echo.SubsetConfig{{Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false)}},
			Ports: []echo.Port{
				{
					// Plain HTTP port, will match no listeners and fall through
					Name:         "http",
					Protocol:     protocol.HTTP,
					ServicePort:  80,
					WorkloadPort: 8080,
				},
				{
					// HTTPS port, will match no listeners and fall through
					Name:         "https",
					Protocol:     protocol.HTTPS,
					ServicePort:  443,
					WorkloadPort: 8443,
					TLS:          true,
				},
				{
					// HTTPS port, there will be an HTTP service defined on this port that will match
					Name:        "https-conflict",
					Protocol:    protocol.HTTPS,
					ServicePort: 9443,
					TLS:         true,
				},
				{
					// TCP port, will match no listeners and fall through
					Name:        "tcp",
					Protocol:    protocol.TCP,
					ServicePort: 9090,
				},
				{
					// TCP port, there will be an HTTP service defined on this port that will match
					Name:        "tcp-conflict",
					Protocol:    protocol.TCP,
					ServicePort: 9091,
				},
			},
			TLSSettings: &common.TLSSettings{
				// Echo has these test certs baked into the docker image
				ClientCert: mustReadCert(t, "cert.crt"),
				Key:        mustReadCert(t, "cert.key"),
			},
		}).BuildOrFail(t)

	if err := t.ConfigIstio().YAML(ServiceEntry).Apply(serviceNamespace.Name()); err != nil {
		t.Errorf("failed to apply service entries: %v", err)
	}

	if _, isKube := t.Environment().(*kube.Environment); isKube {
		createGateway(t, appsNamespace, serviceNamespace)
	}
	return client, dest
}
