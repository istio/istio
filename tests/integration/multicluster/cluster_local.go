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
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
)

// ClusterLocalTest tests that traffic works within a local cluster while in a multicluster configuration
// ClusterLocalNS have been configured in meshConfig.serviceSettings to be clusterLocal.
func ClusterLocalTest(t *testing.T, apps *Apps, features ...features.Feature) {
	framework.NewTest(t).
		Features(features...).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("respect-cluster-local-config").Run(func(ctx framework.TestContext) {
				// turn off cross-cluster load balancing for the namespace
				oldMeshCfg := clusterLocalMeshConfigOrFail(ctx, apps.Namespace)
				defer restoreMeshConfigOrFail(ctx, oldMeshCfg)

				for _, c := range ctx.Clusters() {
					c := c
					ctx.NewSubTest(fmt.Sprintf("%s", c.Name())).
						Label(label.Multicluster).
						RunParallel(func(ctx framework.TestContext) {
							local := apps.LBEchos.GetOrFail(ctx, echo.InCluster(c))
							retry.UntilSuccessOrFail(ctx, func() error {
								results, err := local.Call(echo.CallOptions{
									Target:   local,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Count:    15,
								})
								if err == nil {
									err = results.CheckOK()
								}
								if err != nil {
									return fmt.Errorf("%s to %s:%s using %s: expected success but failed: %v",
										local.Config().Service, local.Config().Service, "http", scheme.HTTP, err)
								}
								if err := results.CheckCluster(c.Name()); err != nil {
									return err
								}
								return nil
							}, retry.Timeout(retryTimeout), retry.Delay(retryDelay))
						})
				}
			})
		})
}

const clusterLocalConfig = `
defaultConfig:
  serviceSettings: 
    - settings:
        clusterLocal: true
      hosts:
      - "*.%s.svc.cluster.local"
`

func clusterLocalMeshConfigOrFail(ctx framework.TestContext, ns namespace.Instance) map[resource.ClusterIndex]string {
	originals := map[resource.ClusterIndex]string{}
	for _, c := range ctx.Clusters() {
		cms := c.CoreV1().ConfigMaps(istio.GetOrFail(ctx, ctx).Settings().SystemNamespace)
		cm, err := cms.Get(context.TODO(), "istio", metav1.GetOptions{})
		if err != nil {
			ctx.Fatal(err)
		}
		originals[c.Index()] = cm.Data["mesh"]
		meshConfig := &v1alpha1.MeshConfig{}
		if err := protomarshal.ApplyYAML(cm.Data["mesh"], meshConfig); err != nil {
			ctx.Fatal(err)
		}
		if err := protomarshal.ApplyYAML(fmt.Sprintf(clusterLocalConfig, ns.Name()), meshConfig); err != nil {
			ctx.Fatal(err)
		}
		cm.Data["mesh"], err = protomarshal.ToYAML(meshConfig)
		if err != nil {
			ctx.Fatal(err)
		}
		if _, err := cms.Update(context.TODO(), cm, metav1.UpdateOptions{}); err != nil {
			ctx.Fatal(err)
		}
	}
	return originals
}

func restoreMeshConfigOrFail(ctx framework.TestContext, originals map[resource.ClusterIndex]string) {
	for _, c := range ctx.Clusters() {
		cms := c.CoreV1().ConfigMaps(istio.GetOrFail(ctx, ctx).Settings().SystemNamespace)
		cm, err := cms.Get(context.TODO(), "istio", metav1.GetOptions{})
		if err != nil {
			ctx.Fatal(err)
		}
		cm.Data["mesh"] = originals[c.Index()]
		if _, err := cms.Update(context.TODO(), cm, metav1.UpdateOptions{}); err != nil {
			ctx.Fatal(err)
		}
	}
}
