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
	"testing"
	"time"

	echoclient "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

type routingTestCase struct {
	name      string
	vs        string
	validator func(*echoclient.ParsedResponse) error
}

func TestTrafficRouting(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.routing").
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "traffic-routing",
				Inject: true,
			})

			echos := echoboot.NewBuilderOrFail(t, ctx).
				WithClusters(ctx.Clusters()).
				With(nil, echoConfigForCluster(ns, "client-%d")).
				With(nil, echoConfigForCluster(ns, "server-%d")).
				BuildOrFail(t)
			clients := echos.GetByServiceNamePrefix("client-")
			servers := echos.GetByServiceNamePrefix("server-")

			for _, src := range ctx.Clusters() {
				for _, dst := range ctx.Clusters() {
					ctx.NewSubTest(fmt.Sprintf("cluster-%d->cluster-%d", src.Index(), dst.Index())).
						Run(func(ctx framework.TestContext) {
							client, server := clients[src.Index()], servers[dst.Index()]
							cases := buildCases(server.Config().Service)
							for _, tt := range cases {
								ctx.NewSubTest(tt.name).Run(func(ctx framework.TestContext) {
									ctx.Config().ApplyYAMLOrFail(ctx, ns.Name(), tt.vs)
									defer ctx.Config().DeleteYAMLOrFail(ctx, ns.Name(), tt.vs)
									retry.UntilSuccessOrFail(ctx, func() error {
										resp, err := client.Call(echo.CallOptions{
											Target:   server,
											PortName: "http",
										})
										if err != nil {
											return err
										}
										if len(resp) != 1 {
											ctx.Fatalf("unexpected response count: %v", resp)
										}
										return tt.validator(resp[0])
									}, retry.Delay(time.Millisecond*100))
								})
							}
						})
				}
			}
		})
}

func buildCases(svcName string) []routingTestCase {
	return []routingTestCase{
		{
			name: "added header",
			vs: fmt.Sprintf(`
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
          istio-custom-header: user-defined-value`, svcName, svcName),
			validator: validateCustomHeader,
		},
		{
			name: "redirect",
			vs: fmt.Sprintf(`
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
        host: %s`, svcName, svcName),
			validator: validateRedirect,
		},
	}
}

func validateCustomHeader(response *echoclient.ParsedResponse) error {
	if response.RawResponse["Istio-Custom-Header"] != "user-defined-value" {
		return fmt.Errorf("missing request header, have %+v", response.RawResponse)
	}
	return nil
}

func validateRedirect(response *echoclient.ParsedResponse) error {
	if response.URL != "/new/path" {
		return fmt.Errorf("incorrect URL, have %+v %+v", response.RawResponse["URL"], response.URL)
	}
	return nil
}
