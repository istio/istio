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
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/util/retry"
)

// TestWorkloadEntryGateway covers a model of multi-network where we rely on writing WorkloadEntry
// resources inside each config cluster rather than doing cross-cluster discovery via remote secret.
// Each case tests a different way of using local resources to reach remote destination(s).
func TestWorkloadEntryGateway(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		RequiresMinClusters(2).
		Run(func(t framework.TestContext) {
			istio.DeployGatewayAPIOrSkip(t)
			i := istio.GetOrFail(t)
			type gwAddr struct {
				ip   string
				port int
			}
			gatewayAddresses := map[string]gwAddr{}
			for _, cluster := range t.Clusters() {
				if _, ok := gatewayAddresses[cluster.NetworkName()]; ok {
					continue
				}
				ips, ports := i.EastWestGatewayFor(cluster).AddressesForPort(15443)
				if ips != nil {
					gatewayAddresses[cluster.NetworkName()] = gwAddr{ips[0], ports[0]}
				}
			}
			if len(t.Clusters().Networks()) != len(gatewayAddresses) {
				t.Skip("must have an east-west for each network")
			}

			// we have an imaginary network for each network called {name}-manual-discovery
			gwTmpl := `
---
apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: remote-gateway-manual-discovery-%s
  labels:
    topology.istio.io/network: "%s-manual-discovery"
spec:
  gatewayClassName: istio-remote
  addresses:
  - value: %q
  listeners:
  - name: cross-network
    port: 15443
    protocol: TLS
    tls:
      mode: Passthrough
      options:
        gateway.istio.io/listener-protocol: auto-passthrough
`
			// a serviceentry that only includes cluster-local endpoints (avoid automatic cross-cluster discovery)
			seTmpl := `
---
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: serviceentry.mesh.global
spec:
  addresses:
  - 240.240.240.240
  hosts: 
  - serviceentry.mesh.global
  ports:
  - number: 80
    targetPort: 18080
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  workloadSelector:
    labels:
      app: b
      topology.istio.io/cluster: %s
`

			exposeServices := `
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: cross-network-gateway
spec:
  selector:
    istio: eastwestgateway
  servers:
  - port:
      number: 15443
      name: tls
      protocol: TLS
    tls:
      mode: AUTO_PASSTHROUGH
    hosts:
    - "serviceentry.mesh.global"
      `

			// expose the ServiceEntry and create the "manual discovery" gateways in all clusters
			cfg := t.ConfigIstio().YAML(i.Settings().SystemNamespace, exposeServices)
			for _, network := range t.Clusters().Networks() {
				cfg.
					YAML(i.Settings().SystemNamespace, fmt.Sprintf(gwTmpl, network, network, gatewayAddresses[network].ip))
			}
			cfg.ApplyOrFail(t)

			// create a unique SE per cluster
			for _, c := range t.Clusters().Configs() {
				t.ConfigKube(c).YAML(apps.Namespace.Name(), fmt.Sprintf(seTmpl, c.Name())).ApplyOrFail(t)
			}

			weTmpl := `
apiVersion: networking.istio.io/v1
kind: WorkloadEntry
metadata:
  name: se-cross-network-{{.testName}}
  labels:
    app: b
    security.istio.io/tlsMode: istio
    # TODO this should be implicit, but for some reason it isn't for WorkloadEntry
    topology.istio.io/cluster: {{.clusterName}}
spec:
  address: "{{.address}}"
  network: "{{.network}}"
{{- if gt .targetPort 0 }}
  ports:
    http: {{ .targetPort }}
{{- end }}
`

			testCases := []struct {
				name            string
				addressFunc     func(nw string) string
				targetPortFunc  func(nw string) int
				networkNameFunc func(nw string) string
			}{
				{
					name:            "with gateway address",
					addressFunc:     func(nw string) string { return gatewayAddresses[nw].ip },
					targetPortFunc:  func(nw string) int { return gatewayAddresses[nw].port },
					networkNameFunc: func(nw string) string { return "" },
				},
				{
					name:            "empty address and network",
					addressFunc:     func(nw string) string { return "" },
					targetPortFunc:  func(nw string) int { return 0 },
					networkNameFunc: func(nw string) string { return nw },
				},
				{
					name:            "locally registered gateway",
					addressFunc:     func(nw string) string { return "" },
					targetPortFunc:  func(nw string) int { return 0 },
					networkNameFunc: func(nw string) string { return nw + "-manual-discovery" },
				},
			}

			for _, tc := range testCases {
				t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					for network, networkClusters := range t.Clusters().ByNetwork() {
						weClusters := t.Clusters().Configs(networkClusters...)
						for _, weCluster := range weClusters {
							t.ConfigKube(weCluster).Eval(apps.Namespace.Name(), map[string]interface{}{
								// used so this WE doesn't get cross-cluster discovered
								"clusterName": weCluster.Name(),
								"testName":    strings.ReplaceAll(tc.name, " ", "-"),
								"network":     tc.networkNameFunc(network),
								"address":     tc.addressFunc(network),
								"targetPort":  tc.targetPortFunc(network),
							}, weTmpl).ApplyOrFail(t)
						}
					}

					for _, src := range apps.A.Instances() {
						// TODO possibly can run parallel
						t.NewSubTestf("from %s", src.Clusters().Default().Name()).Run(func(t framework.TestContext) {
							src.CallOrFail(t, echo.CallOptions{
								// check that we lb to all the networks (we won't reach non-config clusters because of the topology.istio.io/cluster selector)
								// that selector helps us verify that we reached the endpoints due to the WorkloadEntry and not regular multicluster service discovery
								Check:                   check.ReachedClusters(t.Clusters(), apps.A.Clusters().Configs()),
								Address:                 "serviceentry.mesh.global",
								Port:                    ports.HTTP,
								Scheme:                  scheme.HTTP,
								NewConnectionPerRequest: true,
								Retry:                   echo.Retry{Options: []retry.Option{multiclusterRetryDelay, retry.Timeout(time.Minute)}},
								Count:                   10,
							})
						})
					}
				})
			}
		})
}
