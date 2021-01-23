package pilot

import (
	"context"
	"fmt"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
	"testing"

	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v2"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/util/protomarshal"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
