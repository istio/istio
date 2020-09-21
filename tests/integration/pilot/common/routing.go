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
	"fmt"
	"net/http"
	"strings"
	"time"

	"istio.io/istio/pkg/config/protocol"
	echoclient "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework/components/echo"
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
				call: func() (echoclient.ParsedResponses, error) {
					return podA.Call(echo.CallOptions{Target: apps.PodB[0], PortName: "http", Count: callCount})
				},
				validator: func(response echoclient.ParsedResponses) error {
					return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
						return ExpectString(response.RawResponse["Istio-Custom-Header"], "user-defined-value", "request header")
					})
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
				call: func() (echoclient.ParsedResponses, error) {
					return podA.Call(echo.CallOptions{Target: apps.PodB[0], PortName: "http", Path: "/foo?key=value", Count: callCount})
				},
				validator: func(response echoclient.ParsedResponses) error {
					return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
						return ExpectString(response.URL, "/new/path?key=value", "URL")
					})
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
				call: func() (echoclient.ParsedResponses, error) {
					return podA.Call(echo.CallOptions{Target: apps.PodB[0], PortName: "http", Path: "/foo?key=value#hash", Count: callCount})
				},
				validator: func(response echoclient.ParsedResponses) error {
					return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
						return ExpectString(response.URL, "/new/path?key=value", "URL")
					})
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
				call: func() (echoclient.ParsedResponses, error) {
					return podA.Call(echo.CallOptions{Target: apps.PodB[0], PortName: "http", Path: "/foo", Count: callCount})
				},
				validator: func(response echoclient.ParsedResponses) error {
					return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
						return ExpectString(response.Host, "new-authority", "authority")
					})
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
				calls: []TrafficCall{
					{
						// Preflight request
						call: func() (echoclient.ParsedResponses, error) {
							header := http.Header{}
							header.Add("Origin", "cors.com")
							header.Add("Access-Control-Request-Method", "DELETE")
							return podA.Call(echo.CallOptions{
								Target:   apps.PodB[0],
								PortName: "http",
								Method:   "OPTIONS",
								Headers:  header,
								Count:    callCount,
							})
						},
						validator: func(response echoclient.ParsedResponses) error {
							return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
								if err := ExpectString(response.RawResponse["Access-Control-Allow-Origin"], "cors.com", "preflight CORS origin"); err != nil {
									return err
								}
								if err := ExpectString(response.RawResponse["Access-Control-Allow-Methods"], "POST,GET", "preflight CORS method"); err != nil {
									return err
								}
								if err := ExpectString(response.RawResponse["Access-Control-Allow-Headers"], "X-Foo-Bar,X-Foo-Baz", "preflight CORS headers"); err != nil {
									return err
								}
								if err := ExpectString(response.RawResponse["Access-Control-Max-Age"], "86400", "preflight CORS max age"); err != nil {
									return err
								}
								return nil
							})
						},
					},
					{
						// GET
						call: func() (echoclient.ParsedResponses, error) {
							header := http.Header{}
							header.Add("Origin", "cors.com")
							return podA.Call(echo.CallOptions{Target: apps.PodB[0], PortName: "http", Headers: header, Count: callCount})
						},
						validator: func(response echoclient.ParsedResponses) error {
							return ExpectString(response[0].RawResponse["Access-Control-Allow-Origin"], "cors.com", "GET CORS origin")
						},
					},
					{
						// GET without matching origin
						call: func() (echoclient.ParsedResponses, error) {
							return podA.Call(echo.CallOptions{Target: apps.PodB[0], PortName: "http", Count: callCount})
						},
						validator: func(response echoclient.ParsedResponses) error {
							return ExpectString(response[0].RawResponse["Access-Control-Allow-Origin"], "", "mismatched CORS origin")
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
				call: func() (echoclient.ParsedResponses, error) {
					return podA.Call(echo.CallOptions{Target: apps.PodB[0], PortName: "http", Count: 100})
				},
				validator: func(responses echoclient.ParsedResponses) error {
					if err := responses.CheckOK(); err != nil {
						return err
					}
					errorThreshold := 10
					for host, exp := range split {
						hostResponses := responses.Match(func(r *echoclient.ParsedResponse) bool {
							return strings.HasPrefix(r.Hostname, host+"-")
						})
						if !AlmostEquals(len(hostResponses), exp, errorThreshold) {
							return fmt.Errorf("expected %v calls to %q, got %v", exp, host, hostResponses)
						}

						hostDestinations := apps.All.Match(echo.Service(host))
						if host == NakedSvc {
							// only expect to hit same-network clusters for nakedSvc
							hostDestinations = apps.All.Match(echo.Service(host)).Match(echo.InNetwork(podA.Config().Cluster.NetworkName()))
						}

						// since we're changing where traffic goes, make sure we don't break cross-cluster load balancing
						if err := hostResponses.CheckReachedClusters(hostDestinations.Clusters()); err != nil {
							return fmt.Errorf("did not reach all clusters for %s: %v", host, err)
						}
					}
					return nil
				},
			})
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
			call: func() (echoclient.ParsedResponses, error) {
				return apps.Ingress.CallEcho(echo.CallOptions{Port: &echo.Port{Protocol: protocol.HTTP}, Host: fqdn})
			},
			validator: func(responses echoclient.ParsedResponses) error {
				return responses.CheckOK()
			},
		})
	}
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
					// TODO(https://github.com/istio/istio/issues/26798) enable sniffing tcp
					//{"tcp", scheme.TCP},
					//{"auto-tcp", scheme.TCP},
					{"grpc", scheme.GRPC},
					{"auto-grpc", scheme.GRPC},
				}

				// so we can validate all clusters are hit
				callCount := callsPerCluster * len(destinations)
				for _, call := range protocols {
					call := call
					cases = append(cases, TrafficTestCase{
						name: fmt.Sprintf("%v %v->%v from %s", call.port, client.Config().Service, destination.Config().Service, client.Config().Cluster.Name()),
						call: func() (echoclient.ParsedResponses, error) {
							return client.Call(echo.CallOptions{
								Target:   destination,
								PortName: call.port,
								Scheme:   call.scheme,
								Count:    callCount,
								Timeout:  time.Second * 5,
							})
						},
						validator: func(responses echoclient.ParsedResponses) error {
							if err := responses.CheckOK(); err != nil {
								return err
							}
							if err := responses.CheckHost(destination.Config().HostHeader()); err != nil {
								return err
							}
							return nil
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
	cases := []TrafficTestCase{}
	for _, c := range testCases {
		c := c
		cases = append(cases, TrafficTestCase{
			name: fmt.Sprintf("%s from %s", c.name, c.from.Config().Cluster.Name()),
			call: func() (echoclient.ParsedResponses, error) {
				return c.from.Call(echo.CallOptions{
					// assume that all echos in `to` only differ in which cluster they're deployed in
					Target:   c.to[0],
					PortName: "http",
					Host:     c.host,
					Count:    callsPerCluster * len(c.to),
				})
			},
			validator: func(responses echoclient.ParsedResponses) error {
				if err := responses.CheckOK(); err != nil {
					return err
				}
				// when vm -> k8s tests are enabled, they should reach all clusters
				return responses.CheckReachedClusters(c.to.Clusters())
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
	cases := []TrafficTestCase{}
	clients := apps.PodA
	destination := apps.PodC[0]
	configs := []struct {
		port    string
		dest    string
		auth    string
		success bool
	}{
		// TODO: All these cases *should* succeed (except the TLS mismatch cases) - but don't due to issues in our implementation

		// For auto port, outbound request will be delayed by the protocol sniffer, regardless of configuration
		{"auto-tcp-server", "DISABLE", "DISABLE", false},
		{"auto-tcp-server", "DISABLE", "PERMISSIVE", false},
		{"auto-tcp-server", "DISABLE", "STRICT", false},
		{"auto-tcp-server", "ISTIO_MUTUAL", "DISABLE", false},
		{"auto-tcp-server", "ISTIO_MUTUAL", "PERMISSIVE", false},
		{"auto-tcp-server", "ISTIO_MUTUAL", "STRICT", false},

		// These is broken because we will still enable inbound sniffing for the port. Since there is no tls,
		// there is no server-first "upgrading" to client-first
		{"tcp-server", "DISABLE", "DISABLE", true},
		{"tcp-server", "DISABLE", "PERMISSIVE", false},

		// Expected to fail, incompatible configuration
		{"tcp-server", "DISABLE", "STRICT", false},
		{"tcp-server", "ISTIO_MUTUAL", "DISABLE", false},

		// In these cases, we expect success
		// There is no sniffer on either side
		{"tcp-server", "DISABLE", "DISABLE", true},

		// On outbound, we have no sniffer involved
		// On inbound, the request is TLS, so its not server first
		{"tcp-server", "ISTIO_MUTUAL", "PERMISSIVE", true},
		{"tcp-server", "ISTIO_MUTUAL", "STRICT", true},
	}
	for _, client := range clients {
		for _, c := range configs {
			client, c := client, c
			cases = append(cases, TrafficTestCase{
				name:   fmt.Sprintf("%v:%v/%v", c.port, c.dest, c.auth),
				config: destinationRule(destination.Config().Service, c.dest) + peerAuthentication(destination.Config().Service, c.auth),
				call: func() (echoclient.ParsedResponses, error) {
					return client.Call(echo.CallOptions{
						Target:   destination,
						PortName: c.port,
						Scheme:   scheme.TCP,
						// Inbound timeout is 1s. We want to test this does not hit the listener filter timeout
						Timeout: time.Millisecond * 100,
					})
				},
				validator: func(responses echoclient.ParsedResponses) error {
					return responses.CheckOK()
				},
				expectFailure: !c.success,
			})
		}
	}

	return cases
}
