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

package base

import (
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
		Label(label.Multicluster).
		RequireMinClusters(2).
		Setup(multicluster.Setup(&appCtx)).
		Setup(istio.Setup(&ist, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = appCtx.ControlPlaneValues
		})).
		Setup(multicluster.SetupApps(&appCtx)).
		Run()
}

func TestMulticlusterReachability(t *testing.T) {
	multicluster.ReachabilityTest(t, appCtx, "installation.multicluster.multimaster", "installation.multicluster.remote")
}

func TestCrossClusterLoadbalancing(t *testing.T) {
	multicluster.LoadbalancingTest(t, appCtx, "installation.multicluster.multimaster", "installation.multicluster.remote")
}

func TestClusterLocalService(t *testing.T) {
	multicluster.ClusterLocalTest(t, appCtx, "installation.multicluster.multimaster", "installation.multicluster.remote")
}

func TestTelemetry(t *testing.T) {
	multicluster.TelemetryTest(t, appCtx, "installation.multicluster.multimaster", "installation.multicluster.remote")
}
