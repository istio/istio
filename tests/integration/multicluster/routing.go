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
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

// RoutingTest verifies that traffic resources created in a cluster are reflected in the
// configs a sidecar receives in multicluster scenarios.
func RoutingTest(t *testing.T, ns namespace.Instance, pilots []pilot.Instance, feature features.Feature) {
	framework.NewTest(t).
		Label(label.Multicluster).
		Features(feature).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("routing").
				Run(func(ctx framework.TestContext) {
					vsTmpl := `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: cluster-%d-vs
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

					clients := map[resource.ClusterIndex]*echo.Instance{}
					servers := map[resource.ClusterIndex]*echo.Instance{}

					for _, cluster := range clusters {

						var client echo.Instance
						clientRef := &client
						svcName := fmt.Sprintf("client-%d", cluster.Index())
						builder = builder.With(clientRef, newEchoConfig(svcName, ns, cluster, pilots))
						clients[cluster.Index()] = clientRef

						var server echo.Instance
						serverRef := &server
						svcName = fmt.Sprintf("server-%d", cluster.Index())
						builder = builder.With(serverRef, newEchoConfig(svcName, ns, cluster, pilots))
						servers[cluster.Index()] = serverRef

						vs := fmt.Sprintf(vsTmpl, cluster.Index(), cluster.Index(), cluster.Index())
						cluster.ApplyConfigOrFail(t, ns.Name(), vs)
						defer cluster.DeleteConfigOrFail(t, ns.Name(), vs)
					}
					builder.BuildOrFail(ctx)

					for index := range clients {
						src := *clients[index]
						dst := *servers[index]
						subTestName := fmt.Sprintf("%s->%s://%s:%s%s",
							src.Config().Service,
							"http",
							dst.Config().Service,
							"http",
							"/")
						ctx.NewSubTest(subTestName).
							Run(func(ctx framework.TestContext) {
								retry.UntilSuccessOrFail(ctx, func() error {
									resp, err := src.Call(echo.CallOptions{
										Target:   dst,
										PortName: "http",
									})
									if err != nil {
										return err
									}
									if len(resp) != 1 {
										ctx.Fatalf("unexpected response count: %v", resp)
									}
									if resp[0].RawResponse["Istio-Custom-Header"] != "user-defined-value" {
										return fmt.Errorf("missing request header, have %+v", resp[0].RawResponse)
									}
									return nil
								}, retry.Delay(time.Millisecond*100))
							})
					}
				})
		})
}
