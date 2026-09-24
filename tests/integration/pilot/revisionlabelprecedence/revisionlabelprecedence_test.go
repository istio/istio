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

package revisionlabelprecedence

import (
	"fmt"
	"strings"
	"testing"

	"istio.io/api/label"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	kubetest "istio.io/istio/pkg/test/kube"
)

// deployWithRevisionLabels deploys an echo instance into a namespace labeled with
// istio.io/rev=nsRevision, with an explicit pod-level istio.io/rev=podRevision label
// (podRevision == "" means no pod-level label), and returns the resulting pod's name
// and namespace.
func deployWithRevisionLabels(t framework.TestContext, nsPrefix, nsRevision, podRevision string) (podName, podNamespace string) {
	t.Helper()
	nsConfig := namespace.Config{
		Prefix:   nsPrefix,
		Inject:   true,
		Revision: nsRevision,
	}
	if nsRevision == "default" {
		// The namespace helper translates Revision: "default" into legacy
		// injection. Set the literal tag label instead to exercise revision precedence.
		nsConfig.Inject = false
		nsConfig.Labels = map[string]string{label.IoIstioRev.Name: nsRevision}
	}
	ns := namespace.NewOrFail(t, nsConfig)

	var podLabels map[string]string
	if podRevision != "" {
		podLabels = map[string]string{label.IoIstioRev.Name: podRevision}
	}

	deployment.New(t).WithConfig(echo.Config{
		Service:   "revision-precedence",
		Namespace: ns,
		Subsets: []echo.SubsetConfig{
			{
				Labels: podLabels,
			},
		},
	}).BuildOrFail(t)

	fetch := kubetest.NewSinglePodFetch(t.Clusters().Default(),
		ns.Name(),
		fmt.Sprintf("app=%s", "revision-precedence"))
	pods, err := fetch()
	if err != nil {
		t.Fatalf("error fetching pods: %v", err)
	}
	return pods[0].Name, ns.Name()
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

// TestRevisionLabelPrecedencePodWins verifies that when both revisions involved are
// configured with sidecarInjectorWebhook.revisionLabelPrecedence=pod, an explicit pod-level
// istio.io/rev label overrides a conflicting namespace-level istio.io/rev label, and that
// pods with no explicit override still fall back to the namespace label as before.
func TestRevisionLabelPrecedencePodWins(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		RequiresSingleCluster().
		RequiresLocalControlPlane().
		Run(func(t framework.TestContext) {
			istioCtl := istioctl.NewOrFail(t, istioctl.Config{Cluster: t.Clusters().Default()})
			// The installer creates the default tag for rev-a. Explicit tags must
			// inherit the target revision's precedence as well.
			istioCtl.InvokeOrFail(t, []string{"tag", "set", "prod", "--revision", "rev-b"})
			t.Cleanup(func() {
				istioCtl.InvokeOrFail(t, []string{"tag", "remove", "prod", "--skip-confirmation"})
			})
			for _, tc := range []struct {
				name, namespaceRevision, podRevision, want string
			}{
				{"PodOverride", "rev-a", "rev-b", "rev-b"},
				{"NamespaceFallback", "rev-a", "", "rev-a"},
				{"DefaultTagOverride", "default", "rev-b", "rev-b"},
				{"DefaultTagFallback", "default", "", "rev-a"},
				{"PodTagOverride", "rev-a", "prod", "rev-b"},
				{"NamespaceTagOverride", "prod", "rev-a", "rev-a"},
				{"NamespaceTagFallback", "prod", "", "rev-b"},
			} {
				t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					podName, podNamespace := deployWithRevisionLabels(t, "revision-precedence", tc.namespaceRevision, tc.podRevision)
					verifyRevision(t, istioCtl, podName, podNamespace, tc.want)
				})
			}
		})
}
