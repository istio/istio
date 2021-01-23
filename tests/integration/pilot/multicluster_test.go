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
	"context"
	"fmt"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/retry"
	"testing"

	"golang.org/x/sync/errgroup"
	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/util/protomarshal"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCrossClusterLoadbalancing(t *testing.T) {
	framework.NewTest(t).
		Label(label.Multicluster).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("loadbalancing").
				Run(func(ctx framework.TestContext) {
					for _, src := range apps.PodA {
						src := src
						ctx.NewSubTest(fmt.Sprintf("from %s", src.Config().Cluster.Name())).
							Run(func(ctx framework.TestContext) {
								srcNetwork := src.Config().Cluster.NetworkName()
								src.CallOrFail(ctx, echo.CallOptions{
									PortName:  "http",
									Target:    apps.PodB[0],
									Count:     10,
									Validator: checkEqualIntraNetworkTraffic(ctx.Clusters(), srcNetwork),
								})
							})
					}
				})
		})
}

func TestClusterLocal(t *testing.T) {
	framework.NewTest(t).
		RequiresMinClusters(2).
		Run(func(ctx framework.TestContext) {
			if err := clusterLocalSetup(ctx); err != nil {
				ctx.Fatal(err)
			}
			ctx.WhenDone(func() error {
				return clusterLocalCleanup(ctx)
			})

			for _, a := range apps.PodA {
				a := a
				srcCluster := a.Config().Cluster
				ctx.NewSubTest("from " + srcCluster.Name()).
					Run(func(ctx framework.TestContext) {
						b := apps.PodB.Match(echo.InCluster(srcCluster))
						if len(b) == 0 {
							ctx.Skipf("%s does not contain pod b", srcCluster.Name())
							return
						}
						retry.UntilSuccessOrFail(ctx, func() error {
							res, err := a.Call(echo.CallOptions{
								Target:   b[0],
								PortName: "http",
								Count:    5,
							})
							if err != nil {
								return err
							}
							return res.CheckCluster(srcCluster.Name())
						})
					})
			}

		})
}

func clusterLocalSetup(ctx framework.TestContext) error {
	errG := errgroup.Group{}
	for _, c := range ctx.Clusters().OfKind(cluster.Kubernetes) {
		c := c
		errG.Go(func() error {
			return patchMeshConfig(c, func(meshConfig *mesh.MeshConfig) {
				setting := &mesh.MeshConfig_ServiceSettings{
					Hosts: []string{"b.svc.cluster.local"},
				}
				meshConfig.ServiceSettings = append(meshConfig.ServiceSettings, setting)
			})
		})
	}
	return errG.Wait()
}

func clusterLocalCleanup(ctx framework.TestContext) error {
	errG := errgroup.Group{}
	for _, c := range ctx.Clusters().OfKind(cluster.Kubernetes) {
		c := c
		errG.Go(func() error {
			return patchMeshConfig(c, func(meshConfig *mesh.MeshConfig) {
				var idx = -1
				for i, setting := range meshConfig.ServiceSettings {
					if setting.Settings == nil {
						continue
					}
					for _, host := range setting.Hosts {
						if host == "b.svc.cluster.local" {
							idx = i
							break
						}
					}
					if i >= 0 {
						break
					}
				}
				if idx < 0 {
					scopes.Framework.Warnf("did not cleanup cluster local config from meshConfig in %s", c.Name())
					return
				}
				// remove the item
				meshConfig.ServiceSettings = append(
					meshConfig.ServiceSettings[:idx],
					meshConfig.ServiceSettings[idx+1:len(meshConfig.ServiceSettings)]...
				)
			})
		})
	}
	return errG.Wait()
}

func patchMeshConfig(c cluster.Cluster, patch func(config *mesh.MeshConfig)) error {
	ns := i.Settings().SystemNamespace
	cm, err := c.Kube().CoreV1().ConfigMaps(ns).Get(context.TODO(), "istio", metav1.GetOptions{})
	if err != nil {
		return err
	}
	rawConfig, ok := cm.Data["mesh"]
	if !ok {
		return fmt.Errorf("istio configmap missing mesh key in %s", c.Name())
	}
	meshConfig := &mesh.MeshConfig{}
	if err := protomarshal.ApplyYAML(rawConfig, meshConfig); err != nil {
		return err
	}
	patch(meshConfig)
	cm.Data["mesh"], err = protomarshal.ToYAML(meshConfig)
	_, err = c.Kube().CoreV1().ConfigMaps(ns).Update(context.TODO(), cm, metav1.UpdateOptions{})
	return err
}

func checkEqualIntraNetworkTraffic(clusters cluster.Clusters, srcNetwork string) echo.Validator {
	// expect same network traffic to have very equal distribution (20% error)
	intraNetworkClusters := clusters.ByNetwork()[srcNetwork]
	return echo.ValidatorFunc(func(res client.ParsedResponses, err error) error {
		if err != nil {
			return err
		}
		intraNetworkRes := res.Match(func(r *client.ParsedResponse) bool {
			return srcNetwork == clusters.GetByName(r.Cluster).NetworkName()
		})
		if err := intraNetworkRes.CheckEqualClusterTraffic(intraNetworkClusters, 20); err != nil {
			return fmt.Errorf("same network traffic was not even: %v", err)
		}
		return nil
	})
}
