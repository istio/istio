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
				return a.Call(echo.CallOptions{Target: b, PortName: "http"})
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
				return a.Call(echo.CallOptions{Target: b, PortName: "http"})
			},
			validator: func(response echoclient.ParsedResponses) error {
				if response[0].URL != "/new/path" {
					return fmt.Errorf("incorrect URL, have %+v %+v", response[0].RawResponse["URL"], response[0].URL)
				}
				return nil
			},
		},
	}

	for _, split := range []int{50, 80} {
		split := split
		cases = append(cases, TrafficTestCase{
			name: fmt.Sprintf("shifting-%d", split),
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
`, split, 100-split),
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
					for _, h := range []string{"b", "naked"} {
						if strings.HasPrefix(r.Hostname, h+"-") {
							hitCount[h]++
							break
						}
					}
				}
				if !almostEquals(hitCount["b"], split, errorThreshold) {
					return fmt.Errorf("expected %v calls to b, got %v", split, hitCount["b"])
				}
				if !almostEquals(hitCount["naked"], 100-split, errorThreshold) {
					return fmt.Errorf("expected %v calls to naked, got %v", 100-split, hitCount["naked"])
				}
				return nil
			},
		})
	}
	return cases
}

func protocolSniffingCases() []TrafficTestCase {
	cases := []TrafficTestCase{}
	for _, client := range []echo.Instance{a, naked} {
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
				name: fmt.Sprintf("sniffing %v", call.port),
				call: func() (echoclient.ParsedResponses, error) {
					return client.Call(echo.CallOptions{Target: b, PortName: call.port, Scheme: call.scheme})
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
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {
			cases := []TrafficTestCase{}
			cases = append(cases, virtualServiceCases()...)
			cases = append(cases, protocolSniffingCases()...)
			for _, tt := range cases {
				ctx.NewSubTest(tt.name).Run(func(ctx framework.TestContext) {
					if len(tt.config) > 0 {
						ctx.Config().ApplyYAMLOrFail(ctx, echoNamespace.Name(), tt.config)
						defer ctx.Config().DeleteYAMLOrFail(ctx, echoNamespace.Name(), tt.config)
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
