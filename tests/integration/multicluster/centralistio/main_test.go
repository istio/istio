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
	"istio.io/istio/pkg/test/framework/components/cluster"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"testing"

	"istio.io/istio/pkg/test/framework"
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
