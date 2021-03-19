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

package common

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	echoclient "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common/scheme"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

func httpGateway(host string) string {
	return fmt.Sprintf(`apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "%s"
---
`, host)
}

func httpVirtualService(gateway, host string, port int) string {
	return tmpl.MustEvaluate(`apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: {{.Host}}
spec:
  gateways:
  - {{.Gateway}}
  hosts:
  - {{.Host}}
  http:
  - route:
    - destination:
        host: {{.Host}}
        port:
          number: {{.Port}}
---
`, struct {
		Gateway string
		Host    string
		Port    int
	}{gateway, host, port})
}

func virtualServiceCases(apps *EchoDeployments) []TrafficTestCase {
	var cases []TrafficTestCase
	// Send the same call from all different clusters

	cases = append(cases,
		TrafficTestCase{
			name: "added header",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - {{ (index .dst 0).Config.Service }}
  http:
  - route:
    - destination:
        host: {{ (index .dst 0).Config.Service }}
    headers:
      request:
        add:
          istio-custom-header: user-defined-value`,
			opts: echo.CallOptions{
				PortName: "http",
				Validator: echo.ValidatorFunc(
					func(response echoclient.ParsedResponses, _ error) error {
						return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
							return ExpectString(response.RawResponse["Istio-Custom-Header"], "user-defined-value", "request header")
						})
					}),
			},
			workloadAgnostic: true,
		},
		TrafficTestCase{
			name: "redirect",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - {{ (index .dst 0).Config.Service }}
  http:
  - match:
    - uri:
        exact: /foo
    redirect:
      uri: /new/path
  - match:
    - uri:
        exact: /new/path
    route:
    - destination:
        host: {{ (index .dst 0).Config.Service }}`,
			opts: echo.CallOptions{
				PortName:        "http",
				Path:            "/foo?key=value",
				FollowRedirects: true,
				Validator: echo.ValidatorFunc(
					func(response echoclient.ParsedResponses, _ error) error {
						return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
							return ExpectString(response.URL, "/new/path?key=value", "URL")
						})
					}),
			},
			workloadAgnostic: true,
		},
		TrafficTestCase{
			name: "rewrite uri",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - {{ (index .dst 0).Config.Service }}
  http:
  - match:
    - uri:
        exact: /foo
    rewrite:
      uri: /new/path
    route:
    - destination:
        host: {{ (index .dst 0).Config.Service }}`,
			opts: echo.CallOptions{
				PortName: "http",
				Path:     "/foo?key=value#hash",
				Validator: echo.ValidatorFunc(
					func(response echoclient.ParsedResponses, _ error) error {
						return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
							return ExpectString(response.URL, "/new/path?key=value", "URL")
						})
					}),
			},
			workloadAgnostic: true,
		},
		TrafficTestCase{
			name: "rewrite authority",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - {{ (index .dst 0).Config.Service }}
  http:
  - match:
    - uri:
        exact: /foo
    rewrite:
      authority: new-authority
    route:
    - destination:
        host: {{ (index .dst 0).Config.Service }}`,
			opts: echo.CallOptions{
				PortName: "http",
				Path:     "/foo",
				Validator: echo.ValidatorFunc(
					func(response echoclient.ParsedResponses, _ error) error {
						return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
							return ExpectString(response.Host, "new-authority", "authority")
						})
					}),
			},
			workloadAgnostic: true,
		},
		TrafficTestCase{
			name: "cors",
			// TODO https://github.com/istio/istio/issues/31532
			targetFilters: []echotest.SimpleFilter{echotest.Not(echotest.VirtualMachines)},
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - {{ (index .dst 0).Config.Service }}
  http:
  - corsPolicy:
      allowOrigins:
      - exact: cors.com
      allowMethods:
      - POST
      - GET
      allowCredentials: false
      allowHeaders:
      - X-Foo-Bar
      - X-Foo-Baz
      maxAge: "24h"
    route:
    - destination:
        host: {{ (index .dst 0).Config.Service }}
`,
			children: []TrafficCall{
				{
					name: "preflight",
					opts: func() echo.CallOptions {
						header := http.Header{}
						header.Add("Origin", "cors.com")
						header.Add("Access-Control-Request-Method", "DELETE")
						return echo.CallOptions{
							PortName: "http",
							Method:   "OPTIONS",
							Headers:  header,
							Validator: echo.ValidatorFunc(
								func(response echoclient.ParsedResponses, _ error) error {
									return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
										if err := ExpectString(response.RawResponse["Access-Control-Allow-Origin"],
											"cors.com", "preflight CORS origin"); err != nil {
											return err
										}
										if err := ExpectString(response.RawResponse["Access-Control-Allow-Methods"],
											"POST,GET", "preflight CORS method"); err != nil {
											return err
										}
										if err := ExpectString(response.RawResponse["Access-Control-Allow-Headers"],
											"X-Foo-Bar,X-Foo-Baz", "preflight CORS headers"); err != nil {
											return err
										}
										if err := ExpectString(response.RawResponse["Access-Control-Max-Age"],
											"86400", "preflight CORS max age"); err != nil {
											return err
										}
										return nil
									})
								}),
						}
					}(),
				},
				{
					name: "get",
					opts: func() echo.CallOptions {
						header := http.Header{}
						header.Add("Origin", "cors.com")
						return echo.CallOptions{
							PortName: "http",
							Headers:  header,
							Validator: echo.ValidatorFunc(
								func(response echoclient.ParsedResponses, _ error) error {
									return ExpectString(response[0].RawResponse["Access-Control-Allow-Origin"],
										"cors.com", "GET CORS origin")
								}),
						}
					}(),
				},
				{
					// GET without matching origin
					name: "get no origin match",
					opts: echo.CallOptions{
						PortName: "http",
						Validator: echo.ValidatorFunc(
							func(response echoclient.ParsedResponses, _ error) error {
								return ExpectString(response[0].RawResponse["Access-Control-Allow-Origin"], "", "mismatched CORS origin")
							}),
					},
				},
			},
			workloadAgnostic: true,
		},
	)

	// TODO make shifting test workload agnostic
	for _, podA := range apps.PodA {
		podA := podA
		splits := []map[string]int{
			{
				PodBSvc:  50,
				VMSvc:    25,
				NakedSvc: 25,
			},
			{
				PodBSvc:  80,
				VMSvc:    10,
				NakedSvc: 10,
			},
		}
		if len(apps.VM) == 0 {
			splits = []map[string]int{
				{
					PodBSvc:  67,
					NakedSvc: 33,
				},
				{
					PodBSvc:  88,
					NakedSvc: 12,
				},
			}
		}

		for _, split := range splits {
			split := split
			cases = append(cases, TrafficTestCase{
				name: fmt.Sprintf("shifting-%d from %s", split["b"], podA.Config().Cluster.StableName()),
				config: fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - b
  http:
  - route:
    - destination:
        host: b
      weight: %d
    - destination:
        host: naked
      weight: %d
    - destination:
        host: vm
      weight: %d
`, split[PodBSvc], split[NakedSvc], split[VMSvc]),
				call: podA.CallWithRetryOrFail,
				opts: echo.CallOptions{
					Target:   apps.PodB[0],
					PortName: "http",
					Count:    100,
					Validator: echo.And(
						echo.ExpectOK(),
						echo.ValidatorFunc(
							func(responses echoclient.ParsedResponses, _ error) error {
								errorThreshold := 10
								for host, exp := range split {
									hostResponses := responses.Match(func(r *echoclient.ParsedResponse) bool {
										return strings.HasPrefix(r.Hostname, host)
									})
									if !AlmostEquals(len(hostResponses), exp, errorThreshold) {
										return fmt.Errorf("expected %v calls to %q, got %v", exp, host, len(hostResponses))
									}

									// TODO fix flakes where 1 cluster is not hit (https://github.com/istio/istio/issues/28834)
									//hostDestinations := apps.All.Match(echo.Service(host))
									//if host == NakedSvc {
									//	// only expect to hit same-network clusters for nakedSvc
									//	hostDestinations = apps.All.Match(echo.Service(host)).Match(echo.InNetwork(podA.Config().Cluster.NetworkName()))
									//}
									// since we're changing where traffic goes, make sure we don't break cross-cluster load balancing
									//if err := hostResponses.CheckReachedClusters(hostDestinations.Clusters()); err != nil {
									//	return fmt.Errorf("did not reach all clusters for %s: %v", host, err)
									//}
								}
								return nil
							})),
				},
			})
		}
	}

	// reduce the total # of subtests that don't give valuable coverage or just don't work
	for i, tc := range cases {
		noNakedHeadless := func(instances echo.Instances) echo.Instances {
			return instances.Match(echo.Not(echo.IsNaked()).And(echo.Not(echo.IsHeadless())))
		}
		tc.sourceFilters = append(tc.sourceFilters, noNakedHeadless)
		tc.targetFilters = append(tc.targetFilters, noNakedHeadless)
		cases[i] = tc
	}

	return cases
}

func HostHeader(header string) http.Header {
	h := http.Header{}
	h["Host"] = []string{header}
	return h
}

// tlsOriginationCases contains tests TLS origination from DestinationRule
func tlsOriginationCases(apps *EchoDeployments) []TrafficTestCase {
	tc := TrafficTestCase{
		name: "",
		config: fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: external
spec:
  host: %s
  trafficPolicy:
    tls:
      mode: SIMPLE
`, apps.External[0].Config().DefaultHostHeader),
		children: []TrafficCall{},
	}
	expects := []struct {
		port int
		alpn string
	}{
		{8888, "http/1.1"},
		{8882, "h2"},
	}
	for _, c := range apps.PodA {
		for _, e := range expects {
			c := c
			e := e

			tc.children = append(tc.children, TrafficCall{
				name: fmt.Sprintf("%s: %s", c.Config().Cluster.StableName(), e.alpn),
				opts: echo.CallOptions{
					Port:      &echo.Port{ServicePort: e.port, Protocol: protocol.HTTP},
					Address:   apps.External[0].Address(),
					Headers:   HostHeader(apps.External[0].Config().DefaultHostHeader),
					Scheme:    scheme.HTTP,
					Validator: echo.And(echo.ExpectOK(), echo.ExpectKey("Alpn", e.alpn)),
				},
				call: c.CallWithRetryOrFail,
			})
		}
	}
	return []TrafficTestCase{tc}
}

// trafficLoopCases contains tests to ensure traffic does not loop through the sidecar
func trafficLoopCases(apps *EchoDeployments) []TrafficTestCase {
	cases := []TrafficTestCase{}
	for _, c := range apps.PodA {
		for _, d := range apps.PodB {
			for _, port := range []string{"15001", "15006"} {
				c, d, port := c, d, port
				cases = append(cases, TrafficTestCase{
					name: port,
					call: func(t test.Failer, options echo.CallOptions, retryOptions ...retry.Option) echoclient.ParsedResponses {
						dwl := d.WorkloadsOrFail(t)[0]
						cwl := c.WorkloadsOrFail(t)[0]
						resp, err := cwl.ForwardEcho(context.Background(), &epb.ForwardEchoRequest{
							Url:   fmt.Sprintf("http://%s:%s", dwl.Address(), port),
							Count: 1,
						})
						// Ideally we would actually check to make sure we do not blow up the pod,
						// but I couldn't find a way to reliably detect this.
						if err == nil {
							t.Fatalf("expected request to fail, but it didn't: %v", resp)
						}
						return nil
					},
				})
			}
		}
	}
	return cases
}

func gatewayCases(apps *EchoDeployments) []TrafficTestCase {
	cases := []TrafficTestCase{}

	destinationSets := []echo.Instances{
		apps.PodA,
		apps.VM,
		apps.Naked,
		apps.Headless,
		apps.External,
	}

	for _, d := range destinationSets {
		d := d
		if len(d) == 0 {
			continue
		}
		fqdn := d[0].Config().FQDN()
		cases = append(cases, TrafficTestCase{
			name:   d[0].Config().Service,
			config: httpGateway("*") + httpVirtualService("gateway", fqdn, d[0].Config().PortByName("http").ServicePort),
			// TODO call ingress in each cluster & fix flakes calling "external" (https://github.com/istio/istio/issues/28834)
			skip: apps.External.Contains(d[0]) && d.Clusters().IsMulticluster(),
			call: apps.Ingress.CallEchoWithRetryOrFail,
			opts: echo.CallOptions{
				Port: &echo.Port{
					Protocol: protocol.HTTP,
				},
				Headers: map[string][]string{
					"Host": {fqdn},
				},
				Validator: echo.ExpectOK(),
			},
		})
	}
	cases = append(cases,
		TrafficTestCase{
			name:   "404",
			config: httpGateway("*"),
			call:   apps.Ingress.CallEchoWithRetryOrFail,
			opts: echo.CallOptions{
				Port: &echo.Port{
					Protocol: protocol.HTTP,
				},
				Headers: map[string][]string{
					"Host": {"foo.bar"},
				},
				Validator: echo.ExpectCode("404"),
			},
		},
		TrafficTestCase{
			name: "https redirect",
			config: `apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
    tls:
      httpsRedirect: true
---
`,
			call: apps.Ingress.CallEchoWithRetryOrFail,
			opts: echo.CallOptions{
				Port: &echo.Port{
					Protocol: protocol.HTTP,
				},
				Validator: echo.ExpectCode("301"),
			},
		},
		TrafficTestCase{
			// See https://github.com/istio/istio/issues/27315
			name: "https with x-forwarded-proto",
			config: `apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
    tls:
      httpsRedirect: true
---
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: ingressgateway-redirect-config
  namespace: istio-system
spec:
  configPatches:
  - applyTo: NETWORK_FILTER
    match:
      context: GATEWAY
      listener:
        filterChain:
          filter:
            name: envoy.filters.network.http_connection_manager
    patch:
      operation: MERGE
      value:
        typed_config:
          '@type': type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
          xff_num_trusted_hops: 1
          normalize_path: true
  workloadSelector:
    labels:
      istio: ingressgateway
---
` + httpVirtualService("gateway", apps.PodA[0].Config().FQDN(), apps.PodA[0].Config().PortByName("http").ServicePort),
			call: apps.Ingress.CallEchoWithRetryOrFail,
			opts: echo.CallOptions{
				Port: &echo.Port{
					Protocol: protocol.HTTP,
				},
				Headers: map[string][]string{
					// In real world, this may be set by a downstream LB that terminates the TLS
					"X-Forwarded-Proto": {"https"},
					"Host":              {apps.PodA[0].Config().FQDN()},
				},
				Validator: echo.ExpectOK(),
			},
		})
	return cases
}

func XFFGatewayCase(apps *EchoDeployments) []TrafficTestCase {
	cases := []TrafficTestCase{}

	destinationSets := []echo.Instances{
		apps.PodA,
	}

	for _, d := range destinationSets {
		d := d
		if len(d) == 0 {
			continue
		}
		fqdn := d[0].Config().FQDN()
		cases = append(cases, TrafficTestCase{
			name:   d[0].Config().Service,
			config: httpGateway("*") + httpVirtualService("gateway", fqdn, d[0].Config().PortByName("http").ServicePort),
			skip:   false,
			call:   apps.Ingress.CallEchoWithRetryOrFail,
			opts: echo.CallOptions{
				Port: &echo.Port{
					Protocol: protocol.HTTP,
				},
				Headers: map[string][]string{
					"X-Forwarded-For": {"56.5.6.7, 72.9.5.6, 98.1.2.3"},
					"Host":            {fqdn},
				},
				Validator: echo.ValidatorFunc(
					func(response echoclient.ParsedResponses, _ error) error {
						return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
							externalAddress, ok := response.RawResponse["X-Envoy-External-Address"]
							if !ok {
								return fmt.Errorf("missing X-Envoy-External-Address Header")
							}
							if err := ExpectString(externalAddress, "72.9.5.6", "envoy-external-address header"); err != nil {
								return err
							}
							xffHeader, ok := response.RawResponse["X-Forwarded-For"]
							if !ok {
								return fmt.Errorf("missing X-Forwarded-For Header")
							}

							xffIPs := strings.Split(xffHeader, ",")
							if len(xffIPs) != 4 {
								return fmt.Errorf("did not receive expected 4 hosts in X-Forwarded-For header")
							}

							return ExpectString(strings.TrimSpace(xffIPs[1]), "72.9.5.6", "ip in xff header")
						})
					}),
			},
		})
	}
	return cases
}

// serviceCases tests overlapping Services. There are a few cases.
// Consider we have our base service B, with service port P and target port T
// 1) Another service, B', with P -> T. In this case, both the listener and the cluster will conflict.
//    Because everything is workload oriented, this is not a problem unless they try to make them different
//    protocols (this is explicitly called out as "not supported") or control inbound connectionPool settings
//    (which is moving to Sidecar soon)
// 2) Another service, B', with P -> T'. In this case, the listener will be distinct, since its based on the target.
//    The cluster, however, will be shared, which is broken, because we should be forwarding to T when we call B, and T' when we call B'.
// 3) Another service, B', with P' -> T. In this case, the listener is shared. This is fine, with the exception of different protocols
//    The cluster is distinct.
// 4) Another service, B', with P' -> T'. There is no conflicts here at all.
func serviceCases(apps *EchoDeployments) []TrafficTestCase {
	cases := []TrafficTestCase{}
	for _, c := range apps.PodA {
		c := c

		// Case 1
		// Identical to port "http" or service B, just behind another service name
		svc := fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: b-alt-1
  labels:
    app: b
spec:
  ports:
  - name: http
    port: %d
    targetPort: %d
  selector:
    app: b`, FindPortByName("http").ServicePort, FindPortByName("http").InstancePort)
		cases = append(cases, TrafficTestCase{
			name:   "case 1 both match",
			config: svc,
			call:   c.CallWithRetryOrFail,
			opts: echo.CallOptions{
				Address:   "b-alt-1",
				Port:      &echo.Port{ServicePort: FindPortByName("http").ServicePort, Protocol: protocol.HTTP},
				Timeout:   time.Millisecond * 100,
				Validator: echo.ExpectOK(),
			},
		})

		// Case 2
		// We match the service port, but forward to a different port
		// Here we make the new target tcp so the test would fail if it went to the http port
		svc = fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: b-alt-2
  labels:
    app: b
spec:
  ports:
  - name: tcp
    port: %d
    targetPort: %d
  selector:
    app: b`, FindPortByName("http").ServicePort, WorkloadPorts[0].Port)
		cases = append(cases, TrafficTestCase{
			name:   "case 2 service port match",
			config: svc,
			call:   c.CallWithRetryOrFail,
			opts: echo.CallOptions{
				Address:   "b-alt-2",
				Port:      &echo.Port{ServicePort: FindPortByName("http").ServicePort, Protocol: protocol.TCP},
				Scheme:    scheme.TCP,
				Timeout:   time.Millisecond * 100,
				Validator: echo.ExpectOK(),
			},
		})

		// Case 3
		// We match the target port, but front with a different service port
		svc = fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: b-alt-3
  labels:
    app: b
spec:
  ports:
  - name: http
    port: 12345
    targetPort: %d
  selector:
    app: b`, FindPortByName("http").InstancePort)
		cases = append(cases, TrafficTestCase{
			name:   "case 3 target port match",
			config: svc,
			call:   c.CallWithRetryOrFail,
			opts: echo.CallOptions{
				Address:   "b-alt-3",
				Port:      &echo.Port{ServicePort: 12345, Protocol: protocol.HTTP},
				Timeout:   time.Millisecond * 100,
				Validator: echo.ExpectOK(),
			},
		})

		// Case 4
		// Completely new set of ports
		svc = fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: b-alt-4
  labels:
    app: b
spec:
  ports:
  - name: http
    port: 12346
    targetPort: %d
  selector:
    app: b`, WorkloadPorts[1].Port)
		cases = append(cases, TrafficTestCase{
			name:   "case 4 no match",
			config: svc,
			call:   c.CallWithRetryOrFail,
			opts: echo.CallOptions{
				Address:   "b-alt-4",
				Port:      &echo.Port{ServicePort: 12346, Protocol: protocol.HTTP},
				Timeout:   time.Millisecond * 100,
				Validator: echo.ExpectOK(),
			},
		})
	}

	return cases
}

func flatten(clients ...[]echo.Instance) []echo.Instance {
	instances := []echo.Instance{}
	for _, c := range clients {
		instances = append(instances, c...)
	}
	return instances
}

// selfCallsCases checks that pods can call themselves
func selfCallsCases(apps *EchoDeployments) []TrafficTestCase {
	cases := []TrafficTestCase{}
	for _, cl := range flatten(apps.PodA, apps.VM, apps.PodTproxy) {
		cl := cl
		cases = append(cases,
			// Calls to the Service will go through envoy outbound and inbound, so we get envoy headers added
			TrafficTestCase{
				name: fmt.Sprintf("to service %v", cl.Config().Service),
				call: cl.CallWithRetryOrFail,
				opts: echo.CallOptions{
					Target:    cl,
					PortName:  "http",
					Validator: echo.And(echo.ExpectOK(), echo.ExpectKey("X-Envoy-Attempt-Count", "1")),
				},
			},
			// Localhost calls will go directly to localhost, bypassing Envoy. No envoy headers added.
			TrafficTestCase{
				name: fmt.Sprintf("to localhost %v", cl.Config().Service),
				call: cl.CallWithRetryOrFail,
				opts: echo.CallOptions{
					Address:   "localhost",
					Scheme:    scheme.HTTP,
					Port:      &echo.Port{ServicePort: 8080},
					Validator: echo.And(echo.ExpectOK(), echo.ExpectKey("X-Envoy-Attempt-Count", "")),
				},
			})
	}
	return cases
}

// Todo merge with security TestReachability code
func protocolSniffingCases() []TrafficTestCase {
	cases := []TrafficTestCase{}

	type protocolCase struct {
		// The port we call
		port string
		// The actual type of traffic we send to the port
		scheme scheme.Instance
	}
	protocols := []protocolCase{
		{"http", scheme.HTTP},
		{"auto-http", scheme.HTTP},
		{"tcp", scheme.TCP},
		{"auto-tcp", scheme.TCP},
		{"grpc", scheme.GRPC},
		{"auto-grpc", scheme.GRPC},
	}

	// so we can validate all clusters are hit
	for _, call := range protocols {
		call := call
		cases = append(cases, TrafficTestCase{
			// TODO(https://github.com/istio/istio/issues/26798) enable sniffing tcp
			skip: call.scheme == scheme.TCP,
			name: call.port,
			opts: echo.CallOptions{
				PortName: call.port,
				Scheme:   call.scheme,
				Timeout:  time.Second * 5,
			},
			validate: func(src echo.Instance, dst echo.Instances) echo.Validator {
				if call.scheme == scheme.TCP {
					// no host header for TCP
					return echo.ExpectOK()
				}
				return echo.And(
					echo.ExpectOK(),
					echo.ExpectHost(dst[0].Config().HostHeader()))
			},
			workloadAgnostic: true,
		})
	}
	return cases
}

// Todo merge with security TestReachability code
func instanceIPTests(apps *EchoDeployments) []TrafficTestCase {
	cases := []TrafficTestCase{}
	ipCases := []struct {
		name           string
		endpoint       string
		disableSidecar bool
		port           string
		code           int
	}{
		// instance IP bind
		{
			name:           "instance IP without sidecar",
			disableSidecar: true,
			port:           "http-instance",
			code:           200,
		},
		{
			name:     "instance IP with wildcard sidecar",
			endpoint: "0.0.0.0",
			port:     "http-instance",
			code:     200,
		},
		{
			name:     "instance IP with localhost sidecar",
			endpoint: "127.0.0.1",
			port:     "http-instance",
			code:     503,
		},
		{
			name:     "instance IP with empty sidecar",
			endpoint: "",
			port:     "http-instance",
			code:     200,
		},

		// Localhost bind
		{
			name:           "localhost IP without sidecar",
			disableSidecar: true,
			port:           "http-localhost",
			code:           503,
		},
		{
			name:     "localhost IP with wildcard sidecar",
			endpoint: "0.0.0.0",
			port:     "http-localhost",
			code:     503,
		},
		{
			name:     "localhost IP with localhost sidecar",
			endpoint: "127.0.0.1",
			port:     "http-localhost",
			code:     200,
		},
		{
			name:     "localhost IP with empty sidecar",
			endpoint: "",
			port:     "http-localhost",
			code:     503,
		},

		// Wildcard bind
		{
			name:           "wildcard IP without sidecar",
			disableSidecar: true,
			port:           "http",
			code:           200,
		},
		{
			name:     "wildcard IP with wildcard sidecar",
			endpoint: "0.0.0.0",
			port:     "http",
			code:     200,
		},
		{
			name:     "wildcard IP with localhost sidecar",
			endpoint: "127.0.0.1",
			port:     "http",
			code:     200,
		},
		{
			name:     "wildcard IP with empty sidecar",
			endpoint: "",
			port:     "http",
			code:     200,
		},
	}
	for _, ipCase := range ipCases {
		for _, client := range apps.PodA {
			ipCase := ipCase
			client := client
			destination := apps.PodB[0]
			// so we can validate all clusters are hit
			callCount := callsPerCluster * len(apps.PodB)
			var config string
			if !ipCase.disableSidecar {
				config = fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: sidecar
spec:
  workloadSelector:
    labels:
      app: b
  egress:
  - hosts:
    - "./*"
  ingress:
  - port:
      number: %d
      protocol: HTTP
    defaultEndpoint: %s:%d
`, FindPortByName(ipCase.port).InstancePort, ipCase.endpoint, FindPortByName(ipCase.port).InstancePort)
			}
			cases = append(cases,
				TrafficTestCase{
					name:   ipCase.name,
					call:   client.CallWithRetryOrFail,
					config: config,
					opts: echo.CallOptions{
						Target:    destination,
						PortName:  ipCase.port,
						Scheme:    scheme.HTTP,
						Count:     callCount,
						Timeout:   time.Second * 5,
						Validator: echo.ExpectCode(fmt.Sprint(ipCase.code)),
					},
				})
		}
	}
	return cases
}

type vmCase struct {
	name string
	from echo.Instance
	to   echo.Instances
	host string
}

func DNSTestCases(apps *EchoDeployments) []TrafficTestCase {
	makeSE := func(ips ...string) string {
		return tmpl.MustEvaluate(`
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: dns
spec:
  hosts:
  - "fake.service.local"
  addresses:
{{ range $ip := .IPs }}
  - "{{$ip}}"
{{ end }}
  resolution: STATIC
  endpoints: []
  ports:
  - number: 80
    name: http
    protocol: HTTP
`, map[string]interface{}{"IPs": ips})
	}
	tcases := []TrafficTestCase{}
	ipv4 := "1.2.3.4"
	ipv6 := "1234:1234:1234::1234:1234:1234"
	dummyLocalhostServer := "127.0.0.1"
	cases := []struct {
		name string
		// TODO(https://github.com/istio/istio/issues/30282) support multiple vips
		ips      string
		protocol string
		server   string
		expected []string
	}{
		{
			name:     "tcp ipv4",
			ips:      ipv4,
			expected: []string{ipv4},
			protocol: "tcp",
		},
		{
			name:     "udp ipv4",
			ips:      ipv4,
			expected: []string{ipv4},
			protocol: "udp",
		},
		{
			name:     "tcp ipv6",
			ips:      ipv6,
			expected: []string{ipv6},
			protocol: "tcp",
		},
		{
			name:     "udp ipv6",
			ips:      ipv6,
			expected: []string{ipv6},
			protocol: "udp",
		},
		{
			// We should only capture traffic to servers in /etc/resolv.conf nameservers
			// This checks we do not capture traffic to other servers.
			// This is important for cases like app -> istio dns server -> dnsmasq -> upstream
			// If we captured all DNS traffic, we would loop dnsmasq traffic back to our server.
			name:     "tcp localhost server",
			ips:      ipv4,
			expected: []string{},
			protocol: "tcp",
			server:   dummyLocalhostServer,
		},
		{
			name:     "udp localhost server",
			ips:      ipv4,
			expected: []string{},
			protocol: "udp",
			server:   dummyLocalhostServer,
		},
	}
	for _, client := range flatten(apps.VM, apps.PodA, apps.PodTproxy) {
		for _, tt := range cases {
			tt, client := tt, client
			address := "fake.service.local?"
			if tt.protocol != "" {
				address += "&protocol=" + tt.protocol
			}
			if tt.server != "" {
				address += "&server=" + tt.server
			}
			tcases = append(tcases, TrafficTestCase{
				name:   fmt.Sprintf("%s/%s", client.Config().Service, tt.name),
				config: makeSE(tt.ips),
				call:   client.CallWithRetryOrFail,
				opts: echo.CallOptions{
					Scheme:  scheme.DNS,
					Address: address,
					Validator: echo.ValidatorFunc(
						func(response echoclient.ParsedResponses, _ error) error {
							return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
								ips := []string{}
								for _, v := range response.RawResponse {
									ips = append(ips, v)
								}
								sort.Strings(ips)
								if !reflect.DeepEqual(ips, tt.expected) {
									return fmt.Errorf("unexpected dns response: wanted %v, got %v", tt.expected, ips)
								}
								return nil
							})
						}),
				},
			})
		}
	}
	svcCases := []struct {
		name     string
		protocol string
		server   string
	}{
		{
			name:     "tcp",
			protocol: "tcp",
		},
		{
			name:     "udp",
			protocol: "udp",
		},
	}
	for _, client := range flatten(apps.VM, apps.PodA, apps.PodTproxy) {
		for _, tt := range svcCases {
			tt, client := tt, client
			aInCluster := apps.PodA.Match(echo.InCluster(client.Config().Cluster))
			if len(aInCluster) == 0 {
				// The cluster doesn't contain A, but connects to a cluster containing A
				aInCluster = apps.PodA.Match(echo.InCluster(client.Config().Cluster.Primary()))
			}
			address := aInCluster[0].Config().FQDN() + "?"
			if tt.protocol != "" {
				address += "&protocol=" + tt.protocol
			}
			if tt.server != "" {
				address += "&server=" + tt.server
			}
			expected := aInCluster[0].Address()
			tcases = append(tcases, TrafficTestCase{
				name: fmt.Sprintf("svc/%s/%s", client.Config().Service, tt.name),
				call: client.CallWithRetryOrFail,
				opts: echo.CallOptions{
					Scheme:  scheme.DNS,
					Address: address,
					Validator: echo.ValidatorFunc(
						func(response echoclient.ParsedResponses, _ error) error {
							return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
								ips := []string{}
								for _, v := range response.RawResponse {
									ips = append(ips, v)
								}
								sort.Strings(ips)
								exp := []string{expected}
								if !reflect.DeepEqual(ips, exp) {
									return fmt.Errorf("unexpected dns response: wanted %v, got %v", exp, ips)
								}
								return nil
							})
						}),
				},
			})
		}
	}
	return tcases
}

func VMTestCases(vms echo.Instances, apps *EchoDeployments) []TrafficTestCase {
	var testCases []vmCase

	for _, vm := range vms {
		testCases = append(testCases,
			vmCase{
				name: "dns: VM to k8s cluster IP service name.namespace host",
				from: vm,
				to:   apps.PodA,
				host: PodASvc + "." + apps.Namespace.Name(),
			},
			vmCase{
				name: "dns: VM to k8s cluster IP service fqdn host",
				from: vm,
				to:   apps.PodA,
				host: apps.PodA[0].Config().FQDN(),
			},
			vmCase{
				name: "dns: VM to k8s cluster IP service short name host",
				from: vm,
				to:   apps.PodA,
				host: PodASvc,
			},
			vmCase{
				name: "dns: VM to k8s headless service",
				from: vm,
				to:   apps.Headless.Match(echo.InCluster(vm.Config().Cluster.Primary())),
				host: apps.Headless[0].Config().FQDN(),
			},
		)
	}
	for _, podA := range apps.PodA {
		testCases = append(testCases, vmCase{
			name: "k8s to vm",
			from: podA,
			to:   vms,
		})
	}
	cases := make([]TrafficTestCase, 0)
	for _, c := range testCases {
		c := c
		cases = append(cases, TrafficTestCase{
			name: fmt.Sprintf("%s from %s", c.name, c.from.Config().Cluster.StableName()),
			call: c.from.CallWithRetryOrFail,
			opts: echo.CallOptions{
				// assume that all echos in `to` only differ in which cluster they're deployed in
				Target:   c.to[0],
				PortName: "http",
				Address:  c.host,
				Count:    callsPerCluster * len(c.to),
				Validator: echo.And(
					echo.ExpectOK(),
					echo.ExpectReachedClusters(c.to.Clusters())),
			},
		})
	}
	return cases
}

func destinationRule(app, mode string) string {
	return fmt.Sprintf(`apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: %s
spec:
  host: %s
  trafficPolicy:
    tls:
      mode: %s
---
`, app, app, mode)
}

func peerAuthentication(app, mode string) string {
	return fmt.Sprintf(`apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: %s
spec:
  selector:
    matchLabels:
      app: %s
  mtls:
    mode: %s
---
`, app, app, mode)
}

func serverFirstTestCases(apps *EchoDeployments) []TrafficTestCase {
	cases := make([]TrafficTestCase, 0)
	clients := apps.PodA
	destination := apps.PodC[0]
	configs := []struct {
		port      string
		dest      string
		auth      string
		validator echo.Validator
	}{
		// TODO: All these cases *should* succeed (except the TLS mismatch cases) - but don't due to issues in our implementation

		// For auto port, outbound request will be delayed by the protocol sniffer, regardless of configuration
		{"auto-tcp-server", "DISABLE", "DISABLE", echo.ExpectError()},
		{"auto-tcp-server", "DISABLE", "PERMISSIVE", echo.ExpectError()},
		{"auto-tcp-server", "DISABLE", "STRICT", echo.ExpectError()},
		{"auto-tcp-server", "ISTIO_MUTUAL", "DISABLE", echo.ExpectError()},
		{"auto-tcp-server", "ISTIO_MUTUAL", "PERMISSIVE", echo.ExpectError()},
		{"auto-tcp-server", "ISTIO_MUTUAL", "STRICT", echo.ExpectError()},

		// These is broken because we will still enable inbound sniffing for the port. Since there is no tls,
		// there is no server-first "upgrading" to client-first
		{"tcp-server", "DISABLE", "DISABLE", echo.ExpectOK()},
		{"tcp-server", "DISABLE", "PERMISSIVE", echo.ExpectError()},

		// Expected to fail, incompatible configuration
		{"tcp-server", "DISABLE", "STRICT", echo.ExpectError()},
		{"tcp-server", "ISTIO_MUTUAL", "DISABLE", echo.ExpectError()},

		// In these cases, we expect success
		// There is no sniffer on either side
		{"tcp-server", "DISABLE", "DISABLE", echo.ExpectOK()},

		// On outbound, we have no sniffer involved
		// On inbound, the request is TLS, so its not server first
		{"tcp-server", "ISTIO_MUTUAL", "PERMISSIVE", echo.ExpectOK()},
		{"tcp-server", "ISTIO_MUTUAL", "STRICT", echo.ExpectOK()},
	}
	for _, client := range clients {
		for _, c := range configs {
			client, c := client, c
			cases = append(cases, TrafficTestCase{
				name:   fmt.Sprintf("%v:%v/%v", c.port, c.dest, c.auth),
				skip:   apps.IsMulticluster(), // TODO stabilize tcp connection breaks
				config: destinationRule(destination.Config().Service, c.dest) + peerAuthentication(destination.Config().Service, c.auth),
				call:   client.CallWithRetryOrFail,
				opts: echo.CallOptions{
					Target:   destination,
					PortName: c.port,
					Scheme:   scheme.TCP,
					// Inbound timeout is 1s. We want to test this does not hit the listener filter timeout
					Timeout:   time.Millisecond * 100,
					Validator: c.validator,
				},
			})
		}
	}

	return cases
}
