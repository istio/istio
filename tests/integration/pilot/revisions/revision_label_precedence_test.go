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

// TestRevisionLabelPrecedenceNamespaceDefault locks in the pre-existing (default
// sidecarInjectorWebhook.revisionLabelPrecedence=namespace) behavior of the stable/canary
// control planes: when a pod carries an explicit istio.io/rev label that conflicts with its
// namespace's istio.io/rev label, the namespace label wins and the pod-level label is
// ignored. This is the "existing precedence assumption" that revisionLabelPrecedence=pod is
// meant to optionally override.
func TestRevisionLabelPrecedenceNamespaceDefault(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		RequiresSingleCluster().
		RequiresLocalControlPlane().
		Run(func(t framework.TestContext) {
			istioCtl := istioctl.NewOrFail(t, istioctl.Config{Cluster: t.Clusters().Default()})
			podName, podNamespace := deployWithRevisionLabels(t, "stable-ns-canary-pod-label", "stable", "canary")
			verifyRevision(t, istioCtl, podName, podNamespace, "stable")
		})
}

// TestRevisionLabelPrecedencePodWins lives in its own package/cluster
// (tests/integration/pilot/revisionlabelprecedence) since co-installing a
// revisionLabelPrecedence=pod revision alongside these namespace-precedence
// stable/canary revisions trips istioctl's own webhook-overlap validation
// (IST0139) at install time: stable/canary's Case 1 claims any pod in their
// labeled namespace unconditionally, while a pod-precedence revision's Case 2
// claims any pod with a matching explicit label regardless of namespace, so
// the two structurally overlap for any hypothetical pod caught between them.
