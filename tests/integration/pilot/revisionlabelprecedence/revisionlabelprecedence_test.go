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
	ns := namespace.NewOrFail(t, namespace.Config{
		Prefix:   nsPrefix,
		Inject:   true,
		Revision: nsRevision,
	})

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

			t.NewSubTest("PodOverride").Run(func(t framework.TestContext) {
				// Namespace is labeled istio.io/rev=rev-a, but the pod explicitly
				// requests rev-b. With revisionLabelPrecedence=pod on both revisions,
				// the pod label wins.
				podName, podNamespace := deployWithRevisionLabels(t, "rev-a-ns-rev-b-pod", "rev-a", "rev-b")
				verifyRevision(t, istioCtl, podName, podNamespace, "rev-b")
			})

			t.NewSubTest("NamespaceFallback").Run(func(t framework.TestContext) {
				// Pod has no explicit revision label, so injection falls back to the
				// namespace's istio.io/rev=rev-a label, same as under the default
				// (namespace) precedence mode.
				podName, podNamespace := deployWithRevisionLabels(t, "rev-a-ns-no-pod-label", "rev-a", "")
				verifyRevision(t, istioCtl, podName, podNamespace, "rev-a")
			})
		})
}
