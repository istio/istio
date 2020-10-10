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

package centralistio

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/multicluster"
)

var (
	ist    istio.Instance
	appCtx multicluster.AppContext
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.Multicluster, label.Flaky).
		RequireMinClusters(2).
		Setup(multicluster.Setup(&appCtx)).
		Setup(kube.Setup(func(s *kube.Settings, ctx resource.Context) {
			// Make externalIstiod run on first cluster, all others are remotes which use externalIstiod's pilot
			s.ControlPlaneTopology = make(map[resource.ClusterIndex]resource.ClusterIndex)
			primaryCluster := resource.ClusterIndex(0)
			for i := 0; i < len(s.KubeConfig); i++ {
				s.ControlPlaneTopology[resource.ClusterIndex(i)] = primaryCluster
			}
		})).
		Setup(istio.Setup(&ist, func(_ resource.Context, cfg *istio.Config) {

			cfg.Values["global.externalIstiod"] = "true"

			// Set the control plane values on the config.
			// For ingress, add port 15017 to the default list of ports.
			cfg.ControlPlaneValues = appCtx.ControlPlaneValues + `
  global:
    externalIstiod: true`
			cfg.RemoteClusterValues = `
components:
  base:
    enabled: true
  pilot:
    enabled: false  
  istiodRemote:
    enabled: true
values:
  global:
    externalIstiod: true`
		})).
		Setup(multicluster.SetupApps(&appCtx)).
		Run()
}

func TestMulticlusterReachability(t *testing.T) {
	multicluster.ReachabilityTest(t, appCtx, "installation.multicluster.central-istiod")
}

func TestCrossClusterLoadbalancing(t *testing.T) {
	multicluster.LoadbalancingTest(t, appCtx, "installation.multicluster.central-istiod")
}

func TestClusterLocalService(t *testing.T) {
	multicluster.ClusterLocalTest(t, appCtx, "installation.multicluster.central-istiod")
}
