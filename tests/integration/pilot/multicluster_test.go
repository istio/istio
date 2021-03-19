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
	"sync"
	"testing"

	"istio.io/istio/pkg/test/scopes"

	"github.com/hashicorp/go-multierror"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

func TestClusterLocal(t *testing.T) {
	framework.NewTest(t).
		Features(
			"installation.multicluster.cluster_local",
			// TODO tracking topologies as feature labels doesn't make sense
			"installation.multicluster.multimaster",
			"installation.multicluster.remote",
		).
		RequiresMinClusters(2).
		Run(func(t framework.TestContext) {
			// TODO use echotest to dynamically pick 2 simple pods from apps.All
			sources := apps.PodA
			destination := apps.PodB
			patchMeshConfig(t, destination.Clusters(), fmt.Sprintf(`
serviceSettings: 
  - settings:
	  clusterLocal: true
	hosts:
	  - "%s"
`, apps.PodB[0].Config().FQDN()))

			for _, source := range sources {
				source := source
				t.NewSubTest(source.Config().Cluster.StableName()).Run(func(t framework.TestContext) {
					source.CallWithRetryOrFail(t, echo.CallOptions{
						Target:   destination[0],
						Count:    3 * len(destination),
						PortName: "http",
						Scheme:   scheme.HTTP,
						Validator: echo.And(
							echo.ExpectOK(),
							echo.ExpectReachedClusters(cluster.Clusters{source.Config().Cluster}),
						),
					})
				})
			}
		})
}

func patchMeshConfig(t framework.TestContext, clusters cluster.Clusters, patch string) {
	errG := multierror.Group{}
	origCfg := map[string]map[string]string{}
	mu := sync.RWMutex{}
	for _, c := range clusters.Kube() {
		c := c
		errG.Go(func() error {
			cmName := "istio"
			if rev := t.Settings().Revision; rev != "default" {
				cmName += "-" + rev
			}
			cm, err := c.CoreV1().ConfigMaps(i.Settings().SystemNamespace).Get(context.TODO(), cmName, v1.GetOptions{})
			if err != nil {
				return err
			}
			mu.Lock()
			origCfg[c.Name()] = cm.Data
			mu.Unlock()

			mcYaml, ok := cm.Data["mesh"]
			if !ok {
				return fmt.Errorf("mesh config was missing in istio config map for %s", c.Name())
			}
			mc := &meshconfig.MeshConfig{}
			if err := gogoprotomarshal.ApplyYAML(mcYaml, mc); err != nil {
				return err
			}
			if err := gogoprotomarshal.ApplyYAML(patch, mc); err != nil {
				return err
			}
			cm.Data["mesh"], err = gogoprotomarshal.ToYAML(mc)
			if err != nil {
				return err
			}
			_, err = c.CoreV1().ConfigMaps(i.Settings().SystemNamespace).Update(context.TODO(), cm, v1.UpdateOptions{})
			if err != nil {
				return err
			}
			scopes.Framework.Infof("patched %s meshconfig:\n%s", c.Name(), cm.Data["mesh"])
			return nil
		})
	}
	err := errG.Wait()
	t.Cleanup(func() {
		errG := multierror.Group{}
		mu.RLock()
		defer mu.RUnlock()
		for cn, data := range origCfg {
			cn, data := cn, data
			c := clusters.GetByName(cn)
			errG.Go(func() error {
				cm, err := c.CoreV1().ConfigMaps(i.Settings().SystemNamespace).Get(context.TODO(), "istio", v1.GetOptions{})
				if err != nil {
					return err
				}
				cm.Data = data
				_, err = c.CoreV1().ConfigMaps(i.Settings().SystemNamespace).Update(context.TODO(), cm, v1.UpdateOptions{})
				return err
			})
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}
