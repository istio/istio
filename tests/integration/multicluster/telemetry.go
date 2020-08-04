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

package multicluster

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
)

// TelemetryTest validates that source and destination labels are collected
// for multicluster traffic.
func TelemetryTest(t *testing.T, ns namespace.Instance, features ...features.Feature) {
	framework.NewTest(t).
		Label(label.Multicluster).
		Features(features...).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("telemetry").
				Run(func(ctx framework.TestContext) {
					clusters := ctx.Environment().Clusters()
					builder := echoboot.NewBuilder(ctx)
					for _, cluster := range clusters {
						svcName := fmt.Sprintf("echo-%d", cluster.Index())
						builder = builder.With(nil, newEchoConfig(svcName, ns, cluster))
					}
					echos := builder.BuildOrFail(ctx)

					for _, src := range echos {
						for _, dest := range echos {
							src, dest := src, dest
							subTestName := fmt.Sprintf("%s->%s://%s:%s%s",
								src.Config().Service,
								"http",
								dest.Config().Service,
								"http",
								"/")

							ctx.NewSubTest(subTestName).
								RunParallel(func(ctx framework.TestContext) {
									_ = callOrFail(ctx, src, dest)
									validateClusterLabelsInStats(src, t)
									validateClusterLabelsInStats(dest, t)
								})
						}
					}
				})
		})
}

func validateClusterLabelsInStats(svc echo.Instance, t test.Failer) {
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
				if label.GetName() == "source_cluster" {
					hasSourceCluster = true
					continue
				}
				if label.GetName() == "destination_cluster" {
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
