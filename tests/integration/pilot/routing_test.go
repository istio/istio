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
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	echoclient "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
)

// callsPerCluster is used to ensure cross-cluster load balancing has a chance to work
const callsPerCluster = 10

type TrafficTestCase struct {
	name   string
	config string

	// Multiple calls. Cannot be used with call/validator
	calls []TrafficCall

	// Single call
	call      func() (echoclient.ParsedResponses, error)
	validator func(echoclient.ParsedResponses) error

	// if enabled, we will assert the request fails, rather than the request succeeds
	expectFailure bool
}

type TrafficCall struct {
	call          func() (echoclient.ParsedResponses, error)
	validator     func(echoclient.ParsedResponses) error
	expectFailure bool
}

func virtualServiceCases(ctx framework.TestContext) []TrafficTestCase {
	var cases []TrafficTestCase
	callCount := callsPerCluster * len(apps.podB)
	for _, podA := range apps.podA {
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
					return podA.Call(echo.CallOptions{Target: apps.podB[0], PortName: "http", Count: callCount})
				},
				validator: func(response echoclient.ParsedResponses) error {
					return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
						return expectString(response.RawResponse["Istio-Custom-Header"], "user-defined-value", "request header")
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
					return podA.Call(echo.CallOptions{Target: apps.podB[0], PortName: "http", Path: "/foo?key=value", Count: callCount})
				},
				validator: func(response echoclient.ParsedResponses) error {
					return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
						return expectString(response.URL, "/new/path?key=value", "URL")
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
					return podA.Call(echo.CallOptions{Target: apps.podB[0], PortName: "http", Path: "/foo?key=value#hash", Count: callCount})
				},
				validator: func(response echoclient.ParsedResponses) error {
					return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
						return expectString(response.URL, "/new/path?key=value", "URL")
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
					return podA.Call(echo.CallOptions{Target: apps.podB[0], PortName: "http", Path: "/foo", Count: callCount})
				},
				validator: func(response echoclient.ParsedResponses) error {
					return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
						return expectString(response.Host, "new-authority", "authority")
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
								Target:   apps.podB[0],
								PortName: "http",
								Method:   "OPTIONS",
								Headers:  header,
								Count:    callCount,
							})
						},
						validator: func(response echoclient.ParsedResponses) error {
							return response.Check(func(_ int, response *echoclient.ParsedResponse) error {
								if err := expectString(response.RawResponse["Access-Control-Allow-Origin"], "cors.com", "preflight CORS origin"); err != nil {
									return err
								}
								if err := expectString(response.RawResponse["Access-Control-Allow-Methods"], "POST,GET", "preflight CORS method"); err != nil {
									return err
								}
								if err := expectString(response.RawResponse["Access-Control-Allow-Headers"], "X-Foo-Bar,X-Foo-Baz", "preflight CORS headers"); err != nil {
									return err
								}
								if err := expectString(response.RawResponse["Access-Control-Max-Age"], "86400", "preflight CORS max age"); err != nil {
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
							return podA.Call(echo.CallOptions{Target: apps.podB[0], PortName: "http", Headers: header, Count: callCount})
						},
						validator: func(response echoclient.ParsedResponses) error {
							return expectString(response[0].RawResponse["Access-Control-Allow-Origin"], "cors.com", "GET CORS origin")
						},
					},
					{
						// GET without matching origin
						call: func() (echoclient.ParsedResponses, error) {
							return podA.Call(echo.CallOptions{Target: apps.podB[0], PortName: "http", Count: callCount})
						},
						validator: func(response echoclient.ParsedResponses) error {
							return expectString(response[0].RawResponse["Access-Control-Allow-Origin"], "", "mismatched CORS origin")
						},
					}},
			},
		)

		splits := []map[string]int{
			{
				podBSvc:  50,
				vmASvc:   25,
				nakedSvc: 25,
			},
			{
				podBSvc:  80,
				vmASvc:   10,
				nakedSvc: 10,
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
        host: vm-a
      weight: %d
`, split[podBSvc], split[nakedSvc], split[vmASvc]),
				call: func() (echoclient.ParsedResponses, error) {
					return podA.Call(echo.CallOptions{Target: apps.podB[0], PortName: "http", Count: 100})
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
						if !almostEquals(len(hostResponses), exp, errorThreshold) {
							return fmt.Errorf("expected %v calls to %q, got %v", exp, host, hostResponses)
						}

						hostDestinations := apps.all.Match(echo.Service(host))
						if host == nakedSvc {
							// only expect to hit same-network clusters for nakedSvc
							hostDestinations = apps.all.Match(echo.Service(host)).Match(echo.InNetwork(podA.Config().Cluster.NetworkName()))
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

func expectString(got, expected, help string) error {
	if got != expected {
		return fmt.Errorf("got unexpected %v: got %q, wanted %q", help, got, expected)
	}
	return nil
}

// Todo merge with security TestReachability code
func protocolSniffingCases(ctx framework.TestContext) []TrafficTestCase {
	cases := []TrafficTestCase{}
	// TODO add VMs to clients when DNS works for VMs.
	for _, clients := range []echo.Instances{apps.podA, apps.naked, apps.headless} {
		for _, client := range clients {

			destinationSets := []echo.Instances{
				apps.podA,
				apps.vmA,
				// only hit same network naked services
				apps.naked.Match(echo.InNetwork(client.Config().Cluster.NetworkName())),
				// only hit same cluster headless services
				apps.headless.Match(echo.InCluster(client.Config().Cluster)),
			}

			for _, destinations := range destinationSets {
				client := client
				destinations := destinations
				// grabbing the 0th assumes all echos in destinations have the same service name
				destination := destinations[0]
				if apps.naked.Contains(client) && apps.vmA.Contains(destination) {
					// Need a sidecar to connect to VMs
					continue
				}

				// so we can validate all clusters are hit
				callCount := callsPerCluster * len(destinations)
				for _, call := range []struct {
					// The port we call
					port string
					// The actual type of traffic we send to the port
					scheme scheme.Instance
				}{
					{"http", scheme.HTTP},
					{"auto-http", scheme.HTTP},
					{"tcp", scheme.TCP},
					{"auto-tcp", scheme.TCP},
					{"grpc", scheme.GRPC},
					{"auto-grpc", scheme.GRPC},
				} {
					call := call
					cases = append(cases, TrafficTestCase{
						name: fmt.Sprintf("%v %v->%v from %s", call.port, client.Config().Service, destination.Config().Service, client.Config().Cluster.Name()),
						call: func() (echoclient.ParsedResponses, error) {
							return client.Call(echo.CallOptions{Target: destination, PortName: call.port, Scheme: call.scheme, Count: callCount, Timeout: time.Second * 5})
						},
						validator: func(responses echoclient.ParsedResponses) error {
							return responses.CheckOK()
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

func vmTestCases(vms echo.Instances) []TrafficTestCase {
	var testCases []vmCase

	for _, vm := range vms {
		if !vm.Config().DNSCaptureOnVM {
			continue
		}
		testCases = append(testCases,
			vmCase{
				name: "dns: VM to k8s cluster IP service name.namespace host",
				from: vm,
				to:   apps.podA,
				host: podASvc + "." + apps.namespace.Name(),
			},
			vmCase{
				name: "dns: VM to k8s cluster IP service fqdn host",
				from: vm,
				to:   apps.podA,
				host: apps.podA[0].Config().FQDN(),
			},
			vmCase{
				name: "dns: VM to k8s cluster IP service short name host",
				from: vm,
				to:   apps.podA,
				host: podASvc,
			},
			vmCase{
				name: "dns: VM to k8s headless service",
				from: vm,
				to:   apps.headless,
				host: apps.headless[0].Config().FQDN(),
			},
		)
	}
	for _, podA := range apps.podA {
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

func serverFirstTestCases() []TrafficTestCase {
	cases := []TrafficTestCase{}
	clients := apps.podA
	destination := apps.podC[0]
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
		{"tcp-server", "DISABLE", "DISABLE", false},
		{"tcp-server", "DISABLE", "PERMISSIVE", false},

		// Expected to fail, incompatible configuration
		{"tcp-server", "DISABLE", "STRICT", false},
		{"tcp-server", "ISTIO_MUTUAL", "DISABLE", false},

		// In these cases, we expect success
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

func TestTraffic(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.routing", "traffic.reachability", "traffic.shifting").
		Run(func(ctx framework.TestContext) {
			cases := map[string][]TrafficTestCase{}
			cases["virtualservice"] = virtualServiceCases(ctx)
			// TODO(https://github.com/istio/istio/issues/26798)
			//cases["sniffing"] = protocolSniffingCases(ctx)
			cases["serverfirst"] = serverFirstTestCases()
			cases["vm"] = vmTestCases(apps.vmA)
			for n, tts := range cases {
				ctx.NewSubTest(n).Run(func(ctx framework.TestContext) {
					for _, tt := range tts {
						ExecuteTrafficTest(ctx, tt)
					}
				})
			}
		})
}

func ExecuteTrafficTest(ctx framework.TestContext, tt TrafficTestCase) {
	ctx.NewSubTest(tt.name).Run(func(ctx framework.TestContext) {
		if len(tt.config) > 0 {
			ctx.Config().ApplyYAMLOrFail(ctx, apps.namespace.Name(), tt.config)
			ctx.WhenDone(func() error {
				return ctx.Config().DeleteYAML(apps.namespace.Name(), tt.config)
			})
		}
		if tt.call != nil {
			if tt.calls != nil {
				ctx.Fatalf("defined calls and calls; may only define on or the other")
			}
			tt.calls = []TrafficCall{{tt.call, tt.validator, tt.expectFailure}}
		}
		for i, c := range tt.calls {
			name := fmt.Sprintf("%s/%d", tt.name, i)
			retry.UntilSuccessOrFail(ctx, func() error {
				r, err := c.call()
				if !c.expectFailure && err != nil {
					ctx.Logf("call for %v failed, retrying: %v", name, err)
					return err
				} else if c.expectFailure && err == nil {
					e := fmt.Errorf("call for %v did not fail, retrying", name)
					ctx.Log(e)
					return e
				}

				err = c.validator(r)
				if !c.expectFailure && err != nil {
					ctx.Logf("validation for call for %v failed, retrying: %v", name, err)
					return err
				} else if c.expectFailure && err == nil {
					e := fmt.Errorf("validation for %v did not fail, retrying", name)
					ctx.Log(e)
					return e
				}
				return nil
			}, retry.Delay(time.Millisecond*100), retry.Timeout(time.Second*10), retry.Converge(3))
		}
	})
}

func almostEquals(a, b, precision int) bool {
	upper := a + precision
	lower := a - precision
	if b < lower || b > upper {
		return false
	}
	return true
}
