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

package remote

import (
	"testing"

	"istio.io/istio/pkg/test/framework/components/environment/kube"

	"istio.io/istio/tests/integration/multicluster"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	clustersByNetwork                map[string][]*kube.Cluster
	ist                              istio.Instance
	clusterLocalNS, mcReachabilityNS namespace.Instance
	controlPlaneValues               string
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.Multicluster).
		RequireMinClusters(2).
		Setup(multicluster.Setup(&controlPlaneValues, &clusterLocalNS, &mcReachabilityNS)).
		Setup(func(ctx resource.Context) error {
			clustersByNetwork = ctx.Environment().(*kube.Environment).ClustersByNetwork()
			return nil
		}).
		Setup(kube.Setup(func(s *kube.Settings) {
			// Make all clusters in a network use the same control plane
			s.ControlPlaneTopology = make(map[resource.ClusterIndex]resource.ClusterIndex)
			for _, clusters := range clustersByNetwork {
				primary := clusters[0]
				for _, c := range clusters {
					s.ControlPlaneTopology[c.Index()] = primary.Index()
				}
			}
		})).
		Setup(istio.Setup(&ist, func(cfg *istio.Config) {
			// Set the control plane values on the config.
			cfg.ControlPlaneValues = controlPlaneValues
		})).
		Run()
}

func TestMulticlusterReachability(t *testing.T) {
	multicluster.ReachabilityTest(t, mcReachabilityNS, "installation.multicluster.remote")
}

func TestClusterLocalService(t *testing.T) {
	multicluster.ClusterLocalTest(t, clusterLocalNS, "installation.multicluster.remote")
}

func TestTelemetry(t *testing.T) {
	multicluster.TelemetryTest(t, mcReachabilityNS, "installation.multicluster.remote")
}
