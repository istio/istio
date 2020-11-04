// Copyright 2019 Istio Authors
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
	"context"
	"fmt"
	"testing"
	"time"

	echoclient "istio.io/istio/pkg/test/echo/client"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/environment"
	"istio.io/istio/pkg/test/util/retry"
)

func TestTrafficRouting(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "traffic-routing",
				Inject: true,
			})

			var client, server echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&client, echoConfig(ns, "client")).
				With(&server, echoConfig(ns, "server")).
				BuildOrFail(t)

			cases := []struct {
				name      string
				vs        string
				validator func(*echoclient.ParsedResponse) error
			}{
				{
					"added header",
					`
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - server
  http:
  - route:
    - destination:
        host: server
    headers:
      request:
        add:
          istio-custom-header: user-defined-value`,
					func(response *echoclient.ParsedResponse) error {
						if response.RawResponse["Istio-Custom-Header"] != "user-defined-value" {
							return fmt.Errorf("missing request header, have %+v", response.RawResponse)
						}
						return nil
					},
				},
				{
					"redirect",
					`
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - server
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
        host: server`,
					func(response *echoclient.ParsedResponse) error {
						if response.URL != "/new/path" {
							return fmt.Errorf("incorrect URL, have %+v %+v", response.RawResponse["URL"], response.URL)
						}
						return nil
					},
				},
			}
			for _, tt := range cases {
				ctx.NewSubTest(tt.name).Run(func(ctx framework.TestContext) {
					g.ApplyConfigOrFail(ctx, ns, tt.vs)
					defer g.DeleteConfigOrFail(ctx, ns, tt.vs)
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

func TestTrafficLoop(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "echo",
				Inject: true,
			})

			var a, b echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, echoConfig(ns, "a")).
				With(&b, echoConfig(ns, "b")).
				BuildOrFail(t)
			for _, port := range []string{"15001", "15006"} {
				ctx.NewSubTest(port).Run(func(ctx framework.TestContext) {
					dwl := b.WorkloadsOrFail(ctx)[0]
					cwl := a.WorkloadsOrFail(ctx)[0]
					resp, err := cwl.ForwardEcho(context.Background(), &epb.ForwardEchoRequest{
						Url:   fmt.Sprintf("http://%s:%s", dwl.Address(), port),
						Count: 1,
					})
					// Ideally we would actually check to make sure we do not blow up the pod,
					// but I couldn't find a way to reliably detect this.
					if err == nil {
						ctx.Fatalf("expected request to fail, but it didn't: %v", resp)
					}
				})
			}
		})
}
