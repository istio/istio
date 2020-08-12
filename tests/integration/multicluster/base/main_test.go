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

package base

import (
	"testing"

	"istio.io/istio/pkg/test/framework/resource"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/multicluster"
)

var (
	ist  istio.Instance
	apps *multicluster.Apps
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.Multicluster).
		RequireMinClusters(2).
		Setup(istio.Setup(&ist, func(cfg *istio.Config) {
			cfg.ExposeIstiod = true
		})).
		Setup(func(ctx resource.Context) error {
			var err error
			apps, err = multicluster.SetupApps(ctx)
			return err
		}).
		Run()
}

func TestMulticlusterReachability(t *testing.T) {
	multicluster.ReachabilityTest(t, apps, "installation.multicluster.multimaster", "installation.multicluster.remote")
}

func TestCrossClusterLoadbalancing(t *testing.T) {
	multicluster.LoadbalancingTest(t, apps, "installation.multicluster.multimaster", "installation.multicluster.remote")
}

func TestClusterLocalService(t *testing.T) {
	multicluster.ClusterLocalTest(t, apps, "installation.multicluster.multimaster", "installation.multicluster.remote")
}

func TestTelemetry(t *testing.T) {
	multicluster.TelemetryTest(t, apps, "installation.multicluster.multimaster", "installation.multicluster.remote")
}
