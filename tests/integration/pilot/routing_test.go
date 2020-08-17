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

type EchoCall func() (echoclient.ParsedResponses, error)

type TrafficTestCase struct {
	name   string
	config string

	// Multiple calls. Cannot be used with call/validator
	calls []TrafficCall

	// Single call
	call      func() (echoclient.ParsedResponses, error)
	validator func(echoclient.ParsedResponses) error
}

type TrafficCall struct {
	call      func() (echoclient.ParsedResponses, error)
	validator func(echoclient.ParsedResponses) error
}

func virtualServiceCases() []TrafficTestCase {
	cases := []TrafficTestCase{
		{
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
				return apps.podA.Call(echo.CallOptions{Target: apps.podB, PortName: "http"})
			},
			validator: func(response echoclient.ParsedResponses) error {
				return expectString(response[0].RawResponse["Istio-Custom-Header"], "user-defined-value", "request header")
			},
		},
		{
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
				return apps.podA.Call(echo.CallOptions{Target: apps.podB, PortName: "http", Path: "/foo?key=value"})
			},
			validator: func(response echoclient.ParsedResponses) error {
				return expectString(response[0].URL, "/new/path?key=value", "URL")
			},
		},
		{
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
				return apps.podA.Call(echo.CallOptions{Target: apps.podB, PortName: "http", Path: "/foo?key=value#hash"})
			},
			validator: func(response echoclient.ParsedResponses) error {
				return expectString(response[0].URL, "/new/path?key=value", "URL")
			},
		},
		{
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
				return apps.podA.Call(echo.CallOptions{Target: apps.podB, PortName: "http", Path: "/foo"})
			},
			validator: func(response echoclient.ParsedResponses) error {
				return expectString(response[0].Host, "new-authority", "authority")
			},
		},
		{
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
						return apps.podA.Call(echo.CallOptions{
							Target:   apps.podB,
							PortName: "http",
							Method:   "OPTIONS",
							Headers:  header,
						})
					},
					validator: func(response echoclient.ParsedResponses) error {
						if err := expectString(response[0].RawResponse["Access-Control-Allow-Origin"], "cors.com", "preflight CORS origin"); err != nil {
							return err
						}
						if err := expectString(response[0].RawResponse["Access-Control-Allow-Methods"], "POST,GET", "preflight CORS method"); err != nil {
							return err
						}
						if err := expectString(response[0].RawResponse["Access-Control-Allow-Headers"], "X-Foo-Bar,X-Foo-Baz", "preflight CORS headers"); err != nil {
							return err
						}
						if err := expectString(response[0].RawResponse["Access-Control-Max-Age"], "86400", "preflight CORS max age"); err != nil {
							return err
						}
						return nil
					},
				},
				{
					// GET
					call: func() (echoclient.ParsedResponses, error) {
						header := http.Header{}
						header.Add("Origin", "cors.com")
						return apps.podA.Call(echo.CallOptions{Target: apps.podB, PortName: "http", Headers: header})
					},
					validator: func(response echoclient.ParsedResponses) error {
						return expectString(response[0].RawResponse["Access-Control-Allow-Origin"], "cors.com", "GET CORS origin")
					},
				},
				{
					// GET without matching origin
					call: func() (echoclient.ParsedResponses, error) {
						return apps.podA.Call(echo.CallOptions{Target: apps.podB, PortName: "http"})
					},
					validator: func(response echoclient.ParsedResponses) error {
						return expectString(response[0].RawResponse["Access-Control-Allow-Origin"], "", "mismatched CORS origin")
					},
				}},
		},
	}
	splits := []map[string]int{
		{
			"b":     50,
			"vm-a":  25,
			"naked": 25,
		},
		{
			"b":     80,
			"vm-a":  10,
			"naked": 10,
		},
	}
	for _, split := range splits {
		split := split
		cases = append(cases, TrafficTestCase{
			name: fmt.Sprintf("shifting-%d", split["b"]),
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
`, split["b"], split["naked"], split["vm-a"]),
			call: func() (echoclient.ParsedResponses, error) {
				return apps.podA.Call(echo.CallOptions{Target: apps.podB, PortName: "http", Count: 100})
			},
			validator: func(responses echoclient.ParsedResponses) error {
				if err := responses.CheckOK(); err != nil {
					return err
				}
				hitCount := map[string]int{}
				errorThreshold := 10
				for _, r := range responses {
					for _, h := range []string{"b", "naked", "vm-a"} {
						if strings.HasPrefix(r.Hostname, h+"-") {
							hitCount[h]++
							break
						}
					}
				}

				for _, h := range []string{"b", "naked", "vm-a"} {
					if !almostEquals(hitCount[h], split[h], errorThreshold) {
						return fmt.Errorf("expected %v calls to %q, got %v", split[h], h, hitCount[h])
					}
				}
				return nil
			},
		})
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
func protocolSniffingCases() []TrafficTestCase {
	cases := []TrafficTestCase{}
	for _, client := range []echo.Instance{apps.podA, apps.naked, apps.headless} {
		for _, destination := range []echo.Instance{apps.podA, apps.naked, apps.vmA, apps.headless} {
			client := client
			destination := destination
			if client == apps.naked && destination == apps.vmA {
				// Need a sidecar to connect to VMs
				continue
			}
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
				cases = append(cases, TrafficTestCase{
					name: fmt.Sprintf("%v %v %v", call.port, client.Config().Service, destination.Config().Service),
					call: func() (echoclient.ParsedResponses, error) {
						return client.Call(echo.CallOptions{Target: destination, PortName: call.port, Scheme: call.scheme})
					},
					validator: func(responses echoclient.ParsedResponses) error {
						return responses.CheckOK()
					},
				})
			}
		}
	}
	return cases
}

func vmTestCases(vm echo.Instance) []TrafficTestCase {
	testCases := []struct {
		name string
		from echo.Instance
		to   echo.Instance
		host string
	}{
		{
			name: "k8s to vm",
			from: apps.podA,
			to:   vm,
		},
		// Keeping this around until we have a DNS implementation for VMs.
		// {
		// 	name: "dns: VM to k8s cluster IP service name.namespace host",
		// 	from: vm,
		// 	to:   apps.podA,
		// 	host: apps.podA.Config().Service + "." + apps.namespace.Name(),
		// },
		// {
		// 	name: "dns: VM to k8s cluster IP service fqdn host",
		// 	from: vm,
		// 	to:   apps.podA,
		// 	host: apps.podA.Config().FQDN(),
		// },
		// {
		// 	name: "dns: VM to k8s cluster IP service short name host",
		// 	from: vm,
		// 	to:   apps.podA,
		// 	host: apps.podA.Config().Service,
		// },
		// {
		// 	name: "dns: VM to k8s headless service",
		// 	from: vm,
		// 	to:   apps.headless,
		// 	host: apps.headless.Config().FQDN(),
		// },
	}
	cases := []TrafficTestCase{}
	for _, c := range testCases {
		c := c
		cases = append(cases, TrafficTestCase{
			name: c.name,
			call: func() (echoclient.ParsedResponses, error) {
				return c.from.Call(echo.CallOptions{
					Target:   c.to,
					PortName: "http",
					Host:     c.host,
				})
			},
			validator: func(responses echoclient.ParsedResponses) error {
				return responses.CheckOK()
			},
		})
	}
	return cases
}

func TestTraffic(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.routing", "traffic.reachability", "traffic.shifting").
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {
			cases := map[string][]TrafficTestCase{}
			cases["virtualservice"] = virtualServiceCases()
			cases["sniffing"] = protocolSniffingCases()
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
			tt.calls = []TrafficCall{{tt.call, tt.validator}}
		}
		for i, c := range tt.calls {
			name := fmt.Sprintf("%s/%d", tt.name, i)
			retry.UntilSuccessOrFail(ctx, func() error {
				r, err := c.call()
				if err != nil {
					ctx.Logf("call for %v failed, retrying: %v", name, err)
					return err
				}
				if err := c.validator(r); err != nil {
					ctx.Logf("validation for call for %v failed, retrying: %v", name, err)
					return err
				}
				return nil
			}, retry.Delay(time.Millisecond*100))
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
