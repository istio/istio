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
	"strings"
	"testing"

	"istio.io/api/label"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	kubetest "istio.io/istio/pkg/test/kube"
)

func TestRevisionTags(t *testing.T) {
	framework.NewTest(t).
		Features("installation.istioctl.revision_tags").
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			tcs := []struct {
				name     string
				tag      string
				revision string
				error    string
			}{
				{
					"prod-tag-pointed-to-stable",
					"prod",
					"stable",
					"",
				},
				{
					"prod-tag-pointed-to-canary",
					"prod",
					"canary",
					"",
				},
				{
					"tag-pointed-to-non-existent-revision",
					"prod",
					"fake-revision",
					"cannot modify tag",
				},
			}

			istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{Cluster: t.Clusters().Default()})
			baseArgs := []string{"experimental", "tag"}
			for _, tc := range tcs {
				t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					tagSetArgs := append(baseArgs, "set", tc.tag, "--revision", tc.revision, "--skip-confirmation")
					tagSetArgs = append(tagSetArgs, "--manifests", filepath.Join(env.IstioSrc, "manifests"))
					tagRemoveArgs := append(baseArgs, "remove", tc.tag, "-y")

					_, cmdErr, _ := istioCtl.Invoke(tagSetArgs)
					t.Cleanup(func() {
						_, _, _ = istioCtl.Invoke(tagRemoveArgs)
					})

					if tc.error == "" && cmdErr != "" {
						t.Fatalf("did not expect error, got %q", cmdErr)
					}
					if tc.error != "" {
						if !strings.Contains(cmdErr, tc.error) {
							t.Fatalf("expected error to contain %q, got %q", tc.error, cmdErr)
						}
						// found correct error, don't proceed
						return
					}

					// build namespace labeled with tag and create echo in that namespace
					revTagNs := namespace.NewOrFail(t, t, namespace.Config{
						Prefix:   "rev-tag",
						Inject:   true,
						Revision: tc.tag,
					})
					echoboot.NewBuilder(t).WithConfig(echo.Config{
						Service:   "rev-tag",
						Namespace: revTagNs,
					}).BuildOrFail(t)

					fetch := kubetest.NewSinglePodFetch(t.Clusters().Default(),
						revTagNs.Name(),
						fmt.Sprintf("app=%s", "rev-tag"))
					pods, err := fetch()
					if err != nil {
						t.Fatalf("error fetching pods: %v", err)
					}

					injectedRevision := pods[0].GetLabels()[label.IoIstioRev.Name]
					if injectedRevision != tc.revision {
						t.Fatalf("expected revision tag %q, got %q", tc.revision, injectedRevision)
					}
				})
			}
		})
}
