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

	"istio.io/istio/tests/integration/pilot/vm"

	"istio.io/istio/pkg/test/util/tmpl"

	echoclient "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	addedHeaderVSYaml = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - {{ .ServerName }}
  http:
  - route:
    - destination:
        host: {{ .ServerName }}
    headers:
      request:
        add:
          istio-custom-header: user-defined-value`

	redirectVSYaml = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - {{ .ServerName }}
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
        host: {{ .ServerName }}`
)

type ServerConfig struct {
	ServerName string
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

			var client, server, vmServer echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&client, echoVMConfig(ns, "client")).
				With(&server, echoVMConfig(ns, "server")).
				With(&vmServer, echoVMConfig(ns, "server-vm", vm.DefaultVMImage)).
				BuildOrFail(t)

			cases := []struct {
				name      string
				vs        string
				target    echo.Instance
				validator func(*echoclient.ParsedResponse) error
			}{
				{
					"added header",
					tmpl.EvaluateOrFail(t, addedHeaderVSYaml, ServerConfig{
						"server",
					}),
					server,
					addedHeaderValidator,
				},
				{
					"vm added header",
					tmpl.EvaluateOrFail(t, addedHeaderVSYaml, ServerConfig{
						"server-vm",
					}),
					vmServer,
					addedHeaderValidator,
				},
				{
					"redirect",
					tmpl.EvaluateOrFail(t, redirectVSYaml, ServerConfig{
						"server",
					}),
					server,
					redirectValidator,
				},
				{
					"vm redirect",
					tmpl.EvaluateOrFail(t, redirectVSYaml, ServerConfig{
						"server-vm",
					}),
					vmServer,
					redirectValidator,
				},
			}

			for _, tt := range cases {
				ctx.NewSubTest(tt.name).Run(func(ctx framework.TestContext) {
					ctx.ApplyConfigOrFail(ctx, ns.Name(), tt.vs)
					defer ctx.DeleteConfigOrFail(ctx, ns.Name(), tt.vs)
					retry.UntilSuccessOrFail(ctx, func() error {
						resp, err := client.Call(echo.CallOptions{
							Target:   tt.target,
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

func addedHeaderValidator(response *echoclient.ParsedResponse) error {
	if response.RawResponse["Istio-Custom-Header"] != "user-defined-value" {
		return fmt.Errorf("missing request header, have %+v", response.RawResponse)
	}
	return nil
}

func redirectValidator(response *echoclient.ParsedResponse) error {
	if response.URL != "/new/path" {
		return fmt.Errorf("incorrect URL, have %+v %+v", response.RawResponse["URL"], response.URL)
	}
	return nil
}
