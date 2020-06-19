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

package multicluster

import (
	"fmt"
	"testing"

	echoclient "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
)

// RoutingTest validates that source and destination labels are collected
// for multicluster traffic.
func RoutingTest(t *testing.T, ns namespace.Instance, pilots []pilot.Instance, feature features.Feature) {
	framework.NewTest(t).
		Label(label.Multicluster).
		Features(feature).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("routing").
				Run(func(ctx framework.TestContext) {

					type tmpObj struct {
						name      string
						vs        string
						validator func(*echoclient.ParsedResponse) error
						client    *echo.Instance
						server    *echo.Instance
					}

					cases := []tmpObj{}

					vs := `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - server-%d
  http:
  - route:
    - destination:
        host: server-%d
    headers:
      request:
        add:
          istio-custom-header: user-defined-value`

					clusters := ctx.Environment().Clusters()
					builder := echoboot.NewBuilderOrFail(ctx, ctx)
					for _, cluster := range clusters {

						var client echo.Instance
						ref := &client
						svcName := fmt.Sprintf("client-%d", cluster.Index())
						builder = builder.With(ref, newEchoConfig(svcName, ns, cluster, pilots))

						var server echo.Instance
						servRef := &server
						svcName = fmt.Sprintf("server-%d", cluster.Index())
						builder = builder.With(servRef, newEchoConfig(svcName, ns, cluster, pilots))
						vs := fmt.Sprintf(vs, cluster.Index(), cluster.Index())
						cases = append(cases, tmpObj{
							name:   fmt.Sprintf("cluster-%d-routing", cluster.Index()),
							vs:     vs,
							client: ref,
							server: servRef,
						})
						cluster.ApplyConfigOrFail(t, ns.Name(), vs)
					}
					builder.BuildOrFail(ctx)

					for _, tt := range cases {
						ctx.NewSubTest(tt.name).
							Run(func(ctx framework.TestContext) {
								//(*tt.cluster).ApplyConfigOrFail(t, ns.Name(), tt.vs)
								//defer (*tt.cluster).DeleteConfigOrFail(t, ns.Name(), tt.vs)
								resp := callOrFail(ctx, *tt.client, *tt.server)
								if len(resp) < 1 {
									ctx.Fatalf("unexpected response count: %v", resp)
								}
								found := false
								for _, r := range resp {
									if r.RawResponse["Istio-Custom-Header"] == "user-defined-value" {
										found = true
										break
									}
								}
								if !found {
									ctx.Errorf("missing request header, have %+v", resp[0].RawResponse)
								}
							})
					}
				})
		})
}
