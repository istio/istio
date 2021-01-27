// +build integ
//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package centralremotekubeconfig

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/multicluster"
)

var ist istio.Instance

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.Multicluster).
		RequireMinClusters(2).
		Setup(func(ctx resource.Context) error {
			// TODO, this should be exclusively configurable outside of the framework
			configCluster := ctx.Clusters()[0]
			externalControlPlaneCluster := ctx.Clusters()[1]
			for _, c := range ctx.Clusters() {
				c.(*kubecluster.Cluster).OverrideTopology(func(c cluster.Topology) cluster.Topology {
					return c.
						WithConfig(configCluster.Name()).
						WithPrimary(externalControlPlaneCluster.Name())
				})
			}
			return nil
		}).
		Setup(istio.Setup(&ist, func(_ resource.Context, cfg *istio.Config) {
			// Set the control plane values on the config.
			cfg.ConfigClusterValues =
				`components:
  base:
    enabled: true
  pilot:
    enabled: false
  telemetry:
    enabled: false
  istiodRemote:
    enabled: true
  ingressGateways:
  - enabled: false
    name: istio-ingressgateway
  egressGateways:
  - enabled: false
    name: istio-egressgateway
values:
  istiodRemote:
    injectionURL: https://istiod.istio-system.svc:15017/inject
  base:
    validationURL: https://istiod.istio-system.svc:15017/validate`
			cfg.ControlPlaneValues = `
components:
  base:
    enabled: false
  pilot:
    enabled: true
    k8s:
      service:
        type: LoadBalancer
  ingressGateways:
  - enabled: false
    name: istio-ingressgateway
  egressGateways:
  - enabled: false
    name: istio-egressgateway
values:
  global:
    operatorManageWebhooks: true
`
		})).
		Run()
}

func TestIngressGateway(t *testing.T) {
	multicluster.GatewayTest(t, "installation.multicluster.centralremotekubeconfig")
}
