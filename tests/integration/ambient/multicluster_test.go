//go:build integ
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

package ambient

import (
	"context"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestGlobalServiceReachability(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)

	framework.NewTest(t).
		RequiresMinClusters(2).
		RequireIstioVersion("1.27").
		Run(func(t framework.TestContext) {
			clusters := t.Clusters()

			clusterToNetwork := make(map[string]string)
			expectedClusters := sets.New[string]()
			for _, c := range clusters {
				name := c.StableName()
				net := c.NetworkName()
				clusterToNetwork[name] = net
				expectedClusters.Insert(name)
			}

			for _, svc := range apps.All {
				ns := svc.Config().Namespace.Name()
				svcName := svc.ServiceName()
				for _, c := range clusters {
					if _, err := c.Kube().CoreV1().Services(ns).Get(context.TODO(), svcName, metav1.GetOptions{}); err != nil {
						t.Logf("[label] skip: %s/%s not found in %s", ns, svcName, c.StableName())
						continue
					}
					labelService(t, svcName, "istio.io/global", "true")
					t.Logf("[label] labeled %s/%s as global in cluster %s (network %s)",
						ns, svcName, c.StableName(), clusterToNetwork[c.StableName()])
				}
			}

			for _, dst := range apps.Captured {
				for _, src := range apps.Captured {
					// skip self calls
					if src.ServiceName() == dst.ServiceName() &&
						src.Config().Cluster.StableName() == dst.Config().Cluster.StableName() {
						continue
					}
					for _, wl := range src.WorkloadsOrFail(t) {
						var result echo.CallResult
						t.Logf("[call] %s(%s/%s) -> %s : sending %d HTTP calls",
							src.ServiceName(),
							wl.Cluster().StableName(),
							clusterToNetwork[wl.Cluster().StableName()],
							dst.ServiceName(),
							60,
						)

						err := retry.UntilSuccess(func() error {
							var err error
							result, err = src.WithWorkloads(wl).Call(echo.CallOptions{
								To:      dst,
								Port:    echo.Port{Name: "http"},
								Count:   60,
							})
							return err
						})
						if err != nil {
							t.Fatalf("call failed: %s(%s) -> %s: %v",
								src.ServiceName(), wl.Cluster().StableName(), dst.ServiceName(), err)
						}

						respClusters := sets.New[string]()
						for _, r := range result.Responses {
							respClusters.Insert(r.Cluster)
						}

						if !expectedClusters.Equal(respClusters) {
							missing := expectedClusters.Difference(respClusters).UnsortedList()
							unexpected := respClusters.Difference(expectedClusters).UnsortedList()
							t.Fatalf("cluster mismatch: %s(%s)->%s missing=%v unexpected=%v",
								src.ServiceName(), wl.Cluster().StableName(), dst.ServiceName(), missing, unexpected)
						}
					}
				}
			}
		})
}
