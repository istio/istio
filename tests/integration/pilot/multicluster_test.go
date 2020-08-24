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

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/resource"
)

func TestClusterLocalService(t *testing.T) {
	framework.NewTest(t).
		RequiresMinClusters(2).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("respect-cluster-local-config").Run(func(ctx framework.TestContext) {
				for _, c := range ctx.Clusters() {
					c := c
					ctx.NewSubTest(c.Name()).
						Run(func(ctx framework.TestContext) {
							local := apps.local.GetOrFail(ctx, echo.InCluster(c))
							if err := local.CallOrFail(ctx, echo.CallOptions{
								Target:   local,
								PortName: "http",
								Count:    callsPerCluster,
							}).CheckReachedClusters(resource.Clusters{c}); err != nil {
								ctx.Fatalf("traffic was not restricted to %s: %v", c.Name(), err)
							}
						})
				}
			})
		})
}

// TelemetryTest validates that source and destination labels are collected
// for multicluster traffic.
func TestClusterTelemetryLabels(t *testing.T) {
	// TODO(landow) we can remove this when tests/integration/telemetry are converted to cover multicluster
	framework.NewTest(t).
		RequiresMinClusters(2).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("telemetry").
				Run(func(ctx framework.TestContext) {
					for _, src := range apps.podA {
						src := src
						ctx.NewSubTest(fmt.Sprintf("from %s", src.Config().Cluster.Name())).
							Run(func(ctx framework.TestContext) {
								res := src.CallOrFail(ctx, echo.CallOptions{
									Target:   apps.podB[0],
									PortName: "http",
									Count:    callsPerCluster * len(apps.podB),
								})

								// check we reached all clusters before checking each cluster's stats
								if err := res.CheckReachedClusters(apps.podB.Clusters()); err != nil {
									ctx.Fatal(err)
								}

								for _, dest := range apps.podB {
									validateClusterLabelsInStats(t, src, src.Config().Cluster, dest.Config().Cluster)
									validateClusterLabelsInStats(t, dest, src.Config().Cluster, dest.Config().Cluster)
								}
							})
					}
				})
		})
}

func validateClusterLabelsInStats(t test.Failer, svc echo.Instance, expSrc, expDst resource.Cluster) {
	t.Helper()
	workloads := svc.WorkloadsOrFail(t)
	stats := workloads[0].Sidecar().StatsOrFail(t)

	for _, metricName := range []string{"istio_requests_total", "istio_request_duration_milliseconds"} {
		instances, found := stats[metricName]
		if !found {
			t.Fatalf("%s not found in stats: %v", metricName, stats)
		}

		for _, metric := range instances.Metric {
			hasSourceCluster := false
			hasDestinationCluster := false
			for _, label := range metric.Label {
				if label.GetName() == "source_cluster" && label.GetValue() == expSrc.Name() {
					hasSourceCluster = true
					continue
				}
				if label.GetName() == "destination_cluster" && label.GetValue() == expDst.Name() {
					hasDestinationCluster = true
					continue
				}
			}
			if !hasSourceCluster && !hasDestinationCluster {
				t.Fatalf("cluster labels missing for %q. labels: %v", metricName, metric.Label)
			}
		}
	}
}
