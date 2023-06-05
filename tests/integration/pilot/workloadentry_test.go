//go:build integ
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
	"testing"
	"time"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	commonDeployment "istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/util/retry"
)

func TestWorkloadEntryGateway(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		RequiresMinClusters(2).
		Features("traffic.reachability").
		Run(func(t framework.TestContext) {
			ist, err := istio.Get(t)
			if err != nil {
				t.Fatal(err)
			}
			clusterCfg := t.Clusters().Default()
			namespaceName := apps.Namespace.Name()

			// Create a echo deployment "z" in the echos namespace.
			t.Logf("Deploy an echo instance in namespace %s on cluster %s", namespaceName, clusterCfg.Name())
			deployment.New(t, clusterCfg).
				WithConfig(echo.Config{
					Service:        "z",
					ServiceAccount: true,
					Namespace:      apps.Namespace,
					Ports:          ports.All(),
					Subsets:        []echo.SubsetConfig{{}},
				}).BuildOrFail(t)

			// Define an AUTO_PASSTHROUGH EW gateway
			gatewayCfg := `apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: ingress-ew
  namespace: istio-system
spec:
  selector:
    istio: eastwestgateway
  servers:
  - port:
      number: 15443
      name: https
      protocol: TLS
    hosts:
    - serviceentry.mesh.global
    tls:
      mode: AUTO_PASSTHROUGH
`
			// Configure an AUTO_PASSTHROUGH EW gateway
			t.ConfigIstio().YAML("istio-system", gatewayCfg).ApplyOrFail(t, apply.CleanupConditionally)

			ewGatewayIP, ewGatewayPort := ist.EastWestGatewayFor(clusterCfg).AddressForPort(15443)
			if ewGatewayIP == "" || ewGatewayPort == 0 { // most likely EW gateway is not deployed, skip testing
				t.Skipf("Skipping test, eastwest gateway is probably not deployed for cluster %s", clusterCfg.Name())
			}
			serviceEntryYaml := `
apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: z-se
spec:
  addresses:
  - 240.240.34.56
  hosts:
  - serviceentry.mesh.global
  ports:
  - name: http
    number: 80
    protocol: HTTP
    targetPort: 8080
  location: MESH_INTERNAL
  resolution: STATIC
  workloadSelector:
    labels:
      app: z
`
			// Configure ServiceEntry
			t.ConfigIstio().YAML(namespaceName, serviceEntryYaml).ApplyOrFail(t, apply.CleanupConditionally)

			type testCase struct {
				name    string
				port    int
				address string
				network string
			}
			testCases := []testCase{
				{"gateway address", ewGatewayPort, ewGatewayIP, ""},
			}
			if len(t.Clusters().Networks()) > 0 && t.Clusters().Networks()[0] != "" {
				testCases = append(testCases, testCase{"empty address with label", 8080, "", t.Clusters().Networks()[0]})
			}

			for _, tc := range testCases {
				{
					tc := tc
					t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
						workloadEntryYaml := fmt.Sprintf(`apiVersion: networking.istio.io/v1beta1
apiVersion: networking.istio.io/v1beta1
kind: WorkloadEntry
metadata:
  name: z-we
  labels:
    security.istio.io/tlsMode: istio
    app: z
spec:
  network: %q
  ports:
    http: %v
  address: %q`, tc.network, tc.port, tc.address)

						t.ConfigIstio().YAML(namespaceName, workloadEntryYaml).ApplyOrFail(t, apply.CleanupConditionally)

						srcs := apps.All.Instances()
						for _, src := range srcs {
							srcName := src.Config().NamespacedName().Name
							// Skipping tests for these workloads:
							//      external
							//      naked
							//      proxyless-grpc
							//      vm
							if srcName == commonDeployment.ProxylessGRPCSvc ||
								srcName == commonDeployment.NakedSvc ||
								srcName == commonDeployment.ExternalSvc ||
								srcName == commonDeployment.VMSvc {
								continue
							}
							srcCluster := src.Config().Cluster.Name()
							// Assert that non-skipped workloads can reach the service which includes our workload entry
							t.NewSubTestf("%s in %s to ServiceEntry+WorkloadEntry Responds with 200", srcName, srcCluster).Run(func(t framework.TestContext) {
								src.CallOrFail(t, echo.CallOptions{
									Address: "serviceentry.mesh.global",
									Port:    echo.Port{Name: "http", ServicePort: 80},
									Scheme:  scheme.HTTP,
									HTTP: echo.HTTP{
										Path: "/path",
									},
									Check:                   check.OK(),
									NewConnectionPerRequest: true,
									Retry: echo.Retry{
										Options: []retry.Option{multiclusterRetryDelay, retry.Timeout(time.Minute)},
									},
								})
							})
						}
					})
				}
			}
		})
}
