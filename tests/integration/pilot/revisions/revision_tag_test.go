//go:build integ

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

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	kubetest "istio.io/istio/pkg/test/kube"
)

func TestRevisionTags(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		RequiresSingleCluster().
		RequiresLocalControlPlane().
		Run(func(t framework.TestContext) {
			tcs := []struct {
				name     string
				tag      string
				revision string
				nsLabel  string
				error    string
			}{
				{
					"prod tag pointed to stable",
					"prod",
					"stable",
					"istio.io/rev=prod",
					"",
				},
				{
					"prod tag pointed to canary",
					"prod",
					"canary",
					"istio.io/rev=prod",
					"",
				},
				{
					"tag pointed to non existent revision",
					"prod",
					"fake-revision",
					"istio.io/rev=prod",
					"cannot modify tag",
				},
				{
					"default tag-injects for istio injection enabled",
					"default",
					"stable",
					"istio-injection=enabled",
					"",
				},
			}

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{Cluster: t.Clusters().Default()})
			baseArgs := []string{"tag"}
			for _, tc := range tcs {
				t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					tagSetArgs := append(baseArgs, "set", tc.tag, "--revision", tc.revision, "--skip-confirmation", "--overwrite")
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

					// Expect the specified revision to inject for the namespace with the
					// given injection label
					revTagNs := namespace.NewOrFail(t, namespace.Config{
						Prefix: "revision-tag",
					})
					nsLabelParts := strings.Split(tc.nsLabel, "=")
					if len(nsLabelParts) != 2 {
						t.Fatalf("invalid namespace label %s", tc.nsLabel)
					}
					if err := revTagNs.SetLabel(nsLabelParts[0], nsLabelParts[1]); err != nil {
						t.Fatalf("couldn't set label %q on namespace %s: %v",
							tc.nsLabel, revTagNs.Name(), err)
					}

					deployment.New(t).WithConfig(echo.Config{
						Service:   "revision-tag",
						Namespace: revTagNs,
					}).BuildOrFail(t)

					fetch := kubetest.NewSinglePodFetch(t.Clusters().Default(),
						revTagNs.Name(),
						fmt.Sprintf("app=%s", "revision-tag"))
					pods, err := fetch()
					if err != nil {
						t.Fatalf("error fetching pods: %v", err)
					}

					verifyRevision(t, istioCtl, pods[0].Name, revTagNs.Name(), tc.revision)
				})
			}
		})
}

func verifyRevision(t framework.TestContext, i istioctl.Instance, podName, podNamespace, revision string) {
	t.Helper()
	pcArgs := []string{"pc", "bootstrap", podName, "-n", podNamespace}
	bootstrapConfig, _ := i.InvokeOrFail(t, pcArgs)
	expected := fmt.Sprintf("\"discoveryAddress\": \"istiod-%s.istio-system.svc:15012\"", revision)
	if !strings.Contains(bootstrapConfig, expected) {
		t.Errorf("expected revision %q in bootstrap config, did not find", revision)
	}
}
