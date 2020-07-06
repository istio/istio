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

package istioctl

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	stableRevision = "stable"
	canaryRevision = "canary"
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Setup(istio.Setup(nil, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = fmt.Sprintf(`
revision: %s
`, stableRevision)
		})).
		Setup(istio.Setup(nil, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = fmt.Sprintf(`
profile: empty
revision: %s
components:
  pilot:
    enabled: true
`, canaryRevision)
		})).
		Run()
}

func TestUninstallByRevision(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.istioctl.uninstall").
		Run(func(ctx framework.TestContext) {
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
			uninstallCmd := []string{
				"x", "uninstall",
				"--revision=" + stableRevision, "--skip-confirmation",
			}
			istioCtl.InvokeOrFail(t, uninstallCmd)
			cs := ctx.Environment().(*kube.Environment).KubeClusters[0]

			retry.UntilSuccessOrFail(t, func() error {
				for _, gvk := range append(helmreconciler.NamespacedCPResources, helmreconciler.NonNamespacedCPResources...) {
					resources := strings.ToLower(gvk.Kind) + "s"
					gvr := schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: resources}
					ls := fmt.Sprintf("istio.io/rev=%s", stableRevision)
					usList, _ := cs.Dynamic().Resource(gvr).List(context.TODO(), kubeApiMeta.ListOptions{LabelSelector: ls})
					if len(usList.Items) != 0 {
						var stalelist []string
						for _, item := range usList.Items {
							stalelist = append(stalelist, item.GroupVersionKind().String())
						}
						return fmt.Errorf("resources expected to be pruned but still exist in the cluster: %s\n",
							strings.Join(stalelist, " "))
					}
				}
				return nil
			}, retry.Delay(time.Millisecond*100), retry.Timeout(time.Second*30))
		})
}
