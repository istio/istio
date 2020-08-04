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

package centralistio

import (
	"testing"

	"istio.io/istio/tests/integration/multicluster"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	ist                              istio.Instance
	clusterLocalNS, mcReachabilityNS namespace.Instance
	controlPlaneValues               string
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.Multicluster, label.Flaky).
		RequireMinClusters(2).
		Setup(multicluster.Setup(&controlPlaneValues, &clusterLocalNS, &mcReachabilityNS)).
		Setup(kube.Setup(func(s *kube.Settings) {
			// Make CentralIstiod run on first cluster, all others are remotes which use centralIstiod's pilot
			s.ControlPlaneTopology = make(map[resource.ClusterIndex]resource.ClusterIndex)
			primaryCluster := resource.ClusterIndex(0)
			for i := 0; i < len(s.KubeConfig); i++ {
				s.ControlPlaneTopology[resource.ClusterIndex(i)] = primaryCluster
			}
		})).
		Setup(istio.Setup(&ist, func(cfg *istio.Config) {

			cfg.Values["global.centralIstiod"] = "true"

			// Set the control plane values on the config.
			cfg.ControlPlaneValues = controlPlaneValues + `
  gateways:
    istio-ingressgateway:
      meshExpansionPorts:
      - port: 15017
        targetPort: 15017
        name: tcp-webhook
      - port: 15012
        targetPort: 15012
        name: tcp-istiod
  global:
    meshExpansion:
      enabled: true
    centralIstiod: true
    caAddress: istiod.istio-system.svc:15012`
			cfg.RemoteClusterValues = `
components:
  base:
    enabled: true
  pilot:
    enabled: false  
  istiodRemote:
    enabled: true 
  ingressGateways:
  - name: istio-ingressgateway
    enabled: true
values:
  global:
    centralIstiod: true`
		})).
		Run()
}

func TestMulticlusterReachability(t *testing.T) {
	multicluster.ReachabilityTest(t, mcReachabilityNS, "installation.multicluster.central-istiod")
}

func TestClusterLocalService(t *testing.T) {
	multicluster.ClusterLocalTest(t, clusterLocalNS, "installation.multicluster.central-istiod")
}
