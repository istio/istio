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
	"strings"
	"testing"
	"time"

	echoclient "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/util/retry"
)

type TrafficTestCase struct {
	name      string
	config    string
	call      func() (echoclient.ParsedResponses, error)
	validator func(echoclient.ParsedResponses) error
	features  []features.Feature
}

func virtualServiceCases() []TrafficTestCase {
	cases := make([]TrafficTestCase, 0)
	for _, a := range apps.podA {
		for _, b := range apps.podB {
			cases = append(cases,
				TrafficTestCase{
					name: "added header",
					config: fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - %s
  http:
  - route:
    - destination:
        host: %s
    headers:
      request:
        add:
          istio-custom-header: user-defined-value`, b.Config().Service, b.Config().Service),
					call: func() (echoclient.ParsedResponses, error) {
						return a.Call(echo.CallOptions{Target: b, PortName: "http"})
					},
					validator: func(response echoclient.ParsedResponses) error {
						if response[0].RawResponse["Istio-Custom-Header"] != "user-defined-value" {
							return fmt.Errorf("missing request header, have %+v", response[0].RawResponse)
						}
						return nil
					},
				},
				TrafficTestCase{
					name:     "redirect",
					features: []features.Feature{"traffic.routing"},
					config: fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - %s
  http:
  - match:
    - uri:
        exact: /
    redirect:
      uri: /new/path
  - match:
    - uri:
        exact: /new/path
    route:
    - destination:
        host: %s`, b.Config().Service, b.Config().Service),
					call: func() (echoclient.ParsedResponses, error) {
						return a.Call(echo.CallOptions{Target: b, PortName: "http"})
					},
					validator: func(response echoclient.ParsedResponses) error {
						if response[0].URL != "/new/path" {
							return fmt.Errorf("incorrect URL, have %+v %+v", response[0].RawResponse["URL"], response[0].URL)
						}
						return nil
					},
				},
			)
			for _, naked := range apps.naked {
				bSvc, nakedSvc := b.Config().Service, naked.Config().Service
				for _, split := range []int{50, 80} {
					split := split
					cases = append(cases, TrafficTestCase{
						name:     fmt.Sprintf("shifting-%d", split),
						features: []features.Feature{"traffic.shifting"},
						config:   splitConfig(bSvc, nakedSvc, split),
						call: func() (echoclient.ParsedResponses, error) {
							return a.Call(echo.CallOptions{Target: b, PortName: "http", Count: 100})
						},
						validator: func(responses echoclient.ParsedResponses) error {
							if err := responses.CheckOK(); err != nil {
								return err
							}
							hitCount := map[string]int{}
							errorThreshold := 10
							for _, r := range responses {
								for _, h := range []string{bSvc, nakedSvc} {
									if strings.HasPrefix(r.Hostname, h+"-") {
										hitCount[h]++
										break
									}
								}
							}
							if !almostEquals(hitCount[bSvc], split, errorThreshold) {
								return fmt.Errorf("expected %v calls to b, got %v", split, hitCount["b"])
							}
							if !almostEquals(hitCount[nakedSvc], 100-split, errorThreshold) {
								return fmt.Errorf("expected %v calls to naked, got %v", 100-split, hitCount["naked"])
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

func splitConfig(a, b string, split int) string {
	return fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - %s
  http:
  - route:
    - destination:
        host: %s
      weight: %d
    - destination:
        host: %s
      weight: %d
`, a, a, split, b, 100-split)
}

// Todo merge with security TestReachability code
func protocolSniffingCases() []TrafficTestCase {
	cases := []TrafficTestCase{}
	var instances echo.Instances
	for _, set := range []echo.Instances{apps.podA, apps.naked, apps.vmA, apps.headless} {
		instances = append(instances, set...)
	}
	for _, client := range instances {
		for _, destination := range instances {
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
					name:     fmt.Sprintf("%v %v %v", call.port, client.Config().Service, destination.Config().Service),
					features: []features.Feature{"traffic.routing"},
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

func vmTestCases(ctx framework.TestContext, vm echo.Instances) []TrafficTestCase {
	type vmTestCase struct {
		name string
		from echo.Instance
		to   echo.Instance
		host string
	}

	testCases := map[string][]vmTestCase{}
	for _, src := range ctx.Clusters() {
		for _, dst := range ctx.Clusters() {
			srcVM := vm.GetOrFail(ctx, echo.InCluster(src))
			dstA := apps.podA.GetOrFail(ctx, echo.InCluster(dst))
			testCases[fmt.Sprintf("%s-%s", src.Name(), dst.Name())] = []vmTestCase{
				{
					name: "k8s to vm",
					from: apps.podA.GetOrFail(ctx, echo.InCluster(src)),
					to:   vm.GetOrFail(ctx, echo.InCluster(dst)),
				},
				{
					name: "dns: VM to k8s cluster IP service fqdn host",
					from: srcVM,
					to:   dstA,
					host: dstA.Config().FQDN(),
				},
				{
					name: "dns: VM to k8s cluster IP service name.namespace host",
					from: srcVM,
					to:   dstA,
					host: dstA.Config().Service + "." + apps.namespace.Name(),
				},
				{
					name: "dns: VM to k8s cluster IP service short name host",
					from: srcVM,
					to:   dstA,
					host: dstA.Config().Service,
				},
				{
					name: "dns: VM to k8s headless service",
					from: srcVM,
					to:   apps.headless.GetOrFail(ctx, echo.InCluster(dst)),
				},
			}
		}
	}

	cases := []TrafficTestCase{}
	for name, vmCases := range testCases {
		for _, c := range vmCases {
			c := c
			cases = append(cases, TrafficTestCase{
				name:     fmt.Sprintf("%s_%s", name, c.name),
				features: []features.Feature{"traffic.reachability.vm"},
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
	}
	return cases
}

func TestTraffic(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic").
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {
			cases := map[string][]TrafficTestCase{}
			cases["virtualservice"] = virtualServiceCases()
			cases["sniffing"] = protocolSniffingCases()
			cases["vm"] = vmTestCases(ctx, apps.vmA)
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
	ctx.NewSubTest(tt.name).Features(tt.features...).Run(func(ctx framework.TestContext) {
		if len(tt.config) > 0 {
			ctx.Config().ApplyYAMLOrFail(ctx, apps.namespace.Name(), tt.config)
			defer ctx.Config().DeleteYAMLOrFail(ctx, apps.namespace.Name(), tt.config)
		}
		retry.UntilSuccessOrFail(ctx, func() error {
			resp, err := tt.call()
			if err != nil {
				ctx.Logf("call for %v failed, retrying: %v", tt.name, err)
				return err
			}
			return tt.validator(resp)
		}, retry.Delay(time.Millisecond*100))
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
