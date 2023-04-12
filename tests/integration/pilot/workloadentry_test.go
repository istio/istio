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

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
)

func TestWorkloadEntry(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		Features("traffic.reachability").
		Run(func(t framework.TestContext) {
			ist, err := istio.Get(t)
			if err != nil {
				t.Fatal(err)
			}
			clusterCfg := t.Clusters().Default()

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
    - serviceentry.mesh.cluster.local
    tls:
      mode: AUTO_PASSTHROUGH
`
			// Configure an AUTO_PASSTHROUGH EW gateway
			if err := t.ConfigIstio().YAML("istio-system", gatewayCfg).Apply(apply.CleanupConditionally); err != nil {
				t.Fatal(err)
			}

			ewGatewayIP, ewGatewayPort := ist.EastWestGatewayFor(clusterCfg).AddressForPort(15443)
			if ewGatewayIP == "" || ewGatewayPort == 0 { // most likely EW gateway is not deployed, skip testing
				t.Skipf("Skipping test, eastwest gateway is probably not deployed for cluster %s", clusterCfg.Name())
			}

			workloadEntryYaml := fmt.Sprintf(`apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: a-se
spec:
  addresses:
  - 240.240.34.56
  hosts:
  - serviceentry.mesh.cluster.local
  ports:
  - name: http
    number: 80
    protocol: HTTP
    targetPort: 8080
  location: MESH_INTERNAL
  resolution: STATIC
  workloadSelector:
    labels:
      app: a
---
apiVersion: networking.istio.io/v1beta1
kind: WorkloadEntry
metadata:
  name: a-we
  labels:
    security.istio.io/tlsMode: istio
spec:
  network: other
  ports:
    http: %v
  address: %s
  labels:
    security.istio.io/tlsMode: istio
    app: a`, ewGatewayPort, ewGatewayIP)

			aNamespace := apps.A.Instances().NamespaceName()
			if err := t.ConfigIstio().YAML(aNamespace, workloadEntryYaml).Apply(apply.CleanupConditionally); err != nil {
				t.Fatal(err)
			}

			srcs := apps.All.Instances()
			for _, src := range srcs {
				srcName := src.Config().NamespacedName().Name
				// Skipping tests for these workloads:
				//      external
				//      naked
				//      proxyless-grpc
				//      vm
				if srcName == deployment.ProxylessGRPCSvc || srcName == deployment.NakedSvc || srcName == deployment.ExternalSvc || srcName == deployment.VMSvc {
					continue
				}
				srcCluster := src.Config().Cluster.Name()
				// Assert that non-skipped workloads can reach the service which includes our workload entry
				t.NewSubTestf("%s in %s to ServiceEntry+WorkloadEntry Responds with 200", srcName, srcCluster).Run(func(t framework.TestContext) {
					src.CallOrFail(t, echo.CallOptions{
						Address: "serviceentry.mesh.cluster.local",
						Port:    echo.Port{Name: "http", ServicePort: 80},
						Scheme:  scheme.HTTP,
						HTTP: echo.HTTP{
							Path: "/path",
						},
						Check: check.OK(),
					})
				})
			}
		})
}
