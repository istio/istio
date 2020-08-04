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
	"istio.io/istio/pkg/test/util/retry"
)

type TrafficTestCase struct {
	name      string
	config    string
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
				if response[0].RawResponse["Istio-Custom-Header"] != "user-defined-value" {
					return fmt.Errorf("missing request header, have %+v", response[0].RawResponse)
				}
				return nil
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
        exact: /
    redirect:
      uri: /new/path
  - match:
    - uri:
        exact: /new/path
    route:
    - destination:
        host: b`,
			call: func() (echoclient.ParsedResponses, error) {
				return apps.podA.Call(echo.CallOptions{Target: apps.podB, PortName: "http"})
			},
			validator: func(response echoclient.ParsedResponses) error {
				if response[0].URL != "/new/path" {
					return fmt.Errorf("incorrect URL, have %+v %+v", response[0].RawResponse["URL"], response[0].URL)
				}
				return nil
			},
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

// Todo merge with security TestReachability code
func protocolSniffingCases() []TrafficTestCase {
	cases := []TrafficTestCase{}
	for _, client := range []echo.Instance{apps.podA, apps.naked, apps.vmA, apps.headless} {
		for _, destination := range []echo.Instance{apps.podA, apps.naked, apps.vmA, apps.headless} {
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
		{
			name: "dns: VM to k8s cluster IP service name.namespace host",
			from: vm,
			to:   apps.podA,
			host: apps.podA.Config().Service + "." + apps.namespace.Name(),
		},
		{
			name: "dns: VM to k8s cluster IP service fqdn host",
			from: vm,
			to:   apps.podA,
			host: apps.podA.Config().FQDN(),
		},
		{
			name: "dns: VM to k8s cluster IP service short name host",
			from: vm,
			to:   apps.podA,
			host: apps.podA.Config().Service,
		},
		{
			name: "dns: VM to k8s headless service",
			from: vm,
			to:   apps.headless,
			host: apps.headless.Config().FQDN(),
		},
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
