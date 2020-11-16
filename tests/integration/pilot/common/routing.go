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
	"strings"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	echoclient "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common/scheme"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework/components/echo"
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
	callCount := callsPerCluster * len(apps.PodB)
	// Send the same call from all different clusters
	for _, podA := range apps.PodA {
		podA := podA
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
  - b
  http:
  - route:
    - destination:
        host: b
    headers:
      request:
        add:
          istio-custom-header: user-defined-value`,
				call: podA.CallWithRetryOrFail,
				opts: echo.CallOptions{
					Target:   apps.PodB[0],
					PortName: "http",
					Count:    callCount,
					Validator: echo.ValidatorFunc(
						func(response echoclient.ParsedResponses, _ error) error {
							return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
								return ExpectString(response.RawResponse["Istio-Custom-Header"], "user-defined-value", "request header")
							})
						}),
				},
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
    - b
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
        host: b`,
				call: podA.CallWithRetryOrFail,
				opts: echo.CallOptions{
					Target:   apps.PodB[0],
					PortName: "http",
					Path:     "/foo?key=value",
					Count:    callCount,
					Validator: echo.ValidatorFunc(
						func(response echoclient.ParsedResponses, _ error) error {
							return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
								return ExpectString(response.URL, "/new/path?key=value", "URL")
							})
						}),
				},
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
    - b
  http:
  - match:
    - uri:
        exact: /foo
    rewrite:
      uri: /new/path
    route:
    - destination:
        host: b`,
				call: podA.CallWithRetryOrFail,
				opts: echo.CallOptions{
					Target:   apps.PodB[0],
					PortName: "http",
					Path:     "/foo?key=value#hash",
					Count:    callCount,
					Validator: echo.ValidatorFunc(
						func(response echoclient.ParsedResponses, _ error) error {
							return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
								return ExpectString(response.URL, "/new/path?key=value", "URL")
							})
						}),
				},
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
    - b
  http:
  - match:
    - uri:
        exact: /foo
    rewrite:
      authority: new-authority
    route:
    - destination:
        host: b`,
				call: podA.CallWithRetryOrFail,
				opts: echo.CallOptions{
					Target:   apps.PodB[0],
					PortName: "http",
					Path:     "/foo",
					Count:    callCount,
					Validator: echo.ValidatorFunc(
						func(response echoclient.ParsedResponses, _ error) error {
							return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
								return ExpectString(response.Host, "new-authority", "authority")
							})
						}),
				},
			},
			TrafficTestCase{
				name: "cors",
				config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - b
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
        host: b
`,
				children: []TrafficCall{
					{
						name: "preflight",
						call: podA.CallWithRetryOrFail,
						opts: func() echo.CallOptions {
							header := http.Header{}
							header.Add("Origin", "cors.com")
							header.Add("Access-Control-Request-Method", "DELETE")
							return echo.CallOptions{
								Target:   apps.PodB[0],
								PortName: "http",
								Method:   "OPTIONS",
								Headers:  header,
								Count:    callCount,
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
						call: podA.CallWithRetryOrFail,
						opts: func() echo.CallOptions {
							header := http.Header{}
							header.Add("Origin", "cors.com")
							return echo.CallOptions{
								Target:   apps.PodB[0],
								PortName: "http",
								Headers:  header,
								Count:    callCount,
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
						call: podA.CallWithRetryOrFail,
						opts: echo.CallOptions{
							Target:   apps.PodB[0],
							PortName: "http",
							Count:    callCount,
							Validator: echo.ValidatorFunc(
								func(response echoclient.ParsedResponses, _ error) error {
									return ExpectString(response[0].RawResponse["Access-Control-Allow-Origin"], "", "mismatched CORS origin")
								}),
						},
					}},
			},
		)

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

		for _, split := range splits {
			split := split
			cases = append(cases, TrafficTestCase{
				name: fmt.Sprintf("shifting-%d from %s", split["b"], podA.Config().Cluster.Name()),
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
										return strings.HasPrefix(r.Hostname, host+"-")
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

	return cases
}

// trafficLoopCases contains tests to ensure traffic does not loop through the sidecar
func trafficLoopCases(apps *EchoDeployments) []TrafficTestCase {
	cases := []TrafficTestCase{}
	for _, c := range apps.PodA {
		for _, d := range apps.PodB {
			for _, port := range []string{"15001", "15006"} {
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
			name:   fqdn,
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
	cases = append(cases, TrafficTestCase{
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
	})
	return cases
}

// Todo merge with security TestReachability code
func protocolSniffingCases(apps *EchoDeployments) []TrafficTestCase {
	cases := []TrafficTestCase{}
	// TODO add VMs to clients when DNS works for VMs. Blocked by https://github.com/istio/istio/issues/27154
	for _, clients := range []echo.Instances{apps.PodA, apps.Naked, apps.Headless} {
		for _, client := range clients {
			destinationSets := []echo.Instances{
				apps.PodA,
				apps.VM,
				apps.External,
				// only hit same network naked services
				apps.Naked.Match(echo.InNetwork(client.Config().Cluster.NetworkName())),
				// only hit same cluster headless services
				apps.Headless.Match(echo.InCluster(client.Config().Cluster)),
			}

			for _, destinations := range destinationSets {
				if len(destinations) == 0 {
					continue
				}
				client := client
				destinations := destinations
				// grabbing the 0th assumes all echos in destinations have the same service name
				destination := destinations[0]
				if (apps.Headless.Contains(client) || apps.Headless.Contains(destination)) && len(apps.Headless) > 1 {
					// TODO(landow) fix DNS issues with multicluster/VMs/headless
					continue
				}
				if apps.Naked.Contains(client) && apps.VM.Contains(destination) {
					// Need a sidecar to connect to VMs
					continue
				}

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
				callCount := callsPerCluster * len(destinations)
				for _, call := range protocols {
					call := call
					cases = append(cases, TrafficTestCase{
						// TODO(https://github.com/istio/istio/issues/26798) enable sniffing tcp
						skip: call.scheme == scheme.TCP,
						name: fmt.Sprintf("%v %v->%v from %s", call.port, client.Config().Service, destination.Config().Service, client.Config().Cluster.Name()),
						call: client.CallWithRetryOrFail,
						opts: echo.CallOptions{
							Target:   destination,
							PortName: call.port,
							Scheme:   call.scheme,
							Count:    callCount,
							Timeout:  time.Second * 5,
							Validator: echo.And(
								echo.ExpectOK(),
								echo.ExpectHost(destination.Config().HostHeader())),
						},
					})
				}
			}
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
				to:   apps.Headless.Match(echo.InCluster(vm.Config().Cluster)),
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
			name: fmt.Sprintf("%s from %s", c.name, c.from.Config().Cluster.Name()),
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
