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

package multimaster

import (
	"testing"

	"istio.io/istio/tests/integration/multicluster"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	ist                              istio.Instance
	pilots                           []pilot.Instance
	clusterLocalNS, mcReachabilityNS namespace.Instance
	controlPlaneValues               string
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("multicluster/multimaster", m).
		Label(label.Multicluster).
		RequireMinClusters(2).
		Setup(multicluster.Setup(&controlPlaneValues, &clusterLocalNS, &mcReachabilityNS)).
		Setup(istio.Setup(&ist, func(cfg *istio.Config) {
			// Set the control plane values on the config.
			cfg.ControlPlaneValues = controlPlaneValues
		})).
		Setup(func(ctx resource.Context) (err error) {
			pilots = make([]pilot.Instance, len(ctx.Environment().Clusters()))
			for i, cluster := range ctx.Environment().Clusters() {
				if pilots[i], err = pilot.New(ctx, pilot.Config{
					Cluster: cluster,
				}); err != nil {
					return err
				}
			}
			return nil
		}).
		Run()
}

func TestMulticlusterReachability(t *testing.T) {
	multicluster.ReachabilityTest(t, mcReachabilityNS, pilots)
}

func TestClusterLocalService(t *testing.T) {
	multicluster.ClusterLocalTest(t, clusterLocalNS, pilots)
}

func TestTelemetry(t *testing.T) {
	multicluster.TelemetryTest(t, mcReachabilityNS, pilots)
}
