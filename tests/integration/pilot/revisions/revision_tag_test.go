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

package revisions

import (
	"fmt"
	"path/filepath"
	"testing"

	kubetest "istio.io/istio/pkg/test/kube"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func TestRevisionTags(t *testing.T) {
	framework.NewTest(t).
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {
			tcs := []struct {
				name     string
				tag      string
				revision string
			}{
				{
					"rev-tag-canary",
					"canary",
					"default",
				},
				{
					"rev-tag-prod",
					"prod",
					"default",
				},
			}

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{Cluster: ctx.Clusters().Default()})
			baseArgs := []string{"experimental", "tag"}
			for _, tc := range tcs {
				ctx.NewSubTest(tc.name).Run(func(ctx framework.TestContext) {
					tagSetArgs := append(baseArgs, "set", tc.tag, "--revision", tc.revision)
					tagSetArgs = append(tagSetArgs, "--manifests", filepath.Join(env.IstioSrc, "manifests"))
					tagRemoveArgs := append(baseArgs, "remove", tc.tag, "-y")

					// create initial revision tag
					istioCtl.InvokeOrFail(t, tagSetArgs)

					// build namespace labeled with tag and create echo in that namespace
					revTagNs := namespace.NewOrFail(t, ctx, namespace.Config{
						Prefix:   "rev-tag",
						Inject:   true,
						Revision: tc.tag,
					})
					echoboot.NewBuilder(ctx).WithConfig(echo.Config{
						Service:   "rev-tag",
						Namespace: revTagNs,
					}).BuildOrFail(ctx)

					// make sure we have two containers in the echo pod, indicating injection
					fetch := kubetest.NewPodMustFetch(ctx.Clusters().Default(),
						revTagNs.Name(),
						fmt.Sprintf("app=%s", "rev-tag"))
					pods, err := fetch()
					if err != nil {
						t.Fatalf("failed to retrieve pods for app %q", "rev-tag")
					}
					if len(pods) != 1 {
						t.Fatalf("expected 1 pod, got %d", len(pods))
					}
					if len(pods[0].Spec.Containers) != 2 {
						t.Fatalf("expected sidecar injection, got %d containers", len(pods[0].Spec.Containers))
					}

					// remove revision tag
					istioCtl.InvokeOrFail(t, tagRemoveArgs)
				})
			}
		})
}
