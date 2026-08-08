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

package common

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	echodeploy "istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

// RunUserNamespaceTest validates that traffic flows correctly to and from a workload
// running inside a Linux User Namespace (hostUsers: false). It is backend-agnostic and
// is intended to be called from both the default iptables suite and the nftables suite.
func RunUserNamespaceTest(t framework.TestContext, apps deployment.SingleNamespaceView) {
	if t.Settings().Skip(echo.UserNamespace) {
		t.Skip("user namespace tests are disabled")
	}

	ns := namespace.NewOrFail(t, namespace.Config{
		Prefix: "userns",
		Inject: true,
	})

	if t.Settings().OpenShift {
		AnnotateNamespaceForHostUsers(t, ns.Name())
	}

	var userns echo.Instance
	echodeploy.New(t).
		With(&userns, echo.Config{
			Service:        "userns",
			Namespace:      ns,
			ServiceAccount: true,
			Ports:          ports.All(),
			Subsets:        []echo.SubsetConfig{{}},
			UserNamespace:  true,
		}).
		BuildOrFail(t)

	src := apps.A[0]
	t.NewSubTest("from standard to userns").Run(func(t framework.TestContext) {
		src.CallOrFail(t, echo.CallOptions{
			To:    userns,
			Port:  echo.Port{Name: "http"},
			Check: check.OK(),
		})
	})
	t.NewSubTest("from userns to standard").Run(func(t framework.TestContext) {
		userns.CallOrFail(t, echo.CallOptions{
			To:    src,
			Port:  echo.Port{Name: "http"},
			Check: check.OK(),
		})
	})
}

// AnnotateNamespaceForHostUsers overrides the OpenShift project's default UID/GID allocation
// to a range below 65,535, which is required for Linux User Namespaces (hostUsers: false).
//
// OpenShift's default project allocation assigns IDs starting at ~1,000,000,000. For user namespaces,
// the kernel and CRI-O require container-side IDs to fit within POSIX boundaries (< 65,535).
// Without this override, pods with hostUsers: false will be rejected by the SCC admission webhook
// (validation mismatch against the project's billion-level range) or fail at the Kubelet/CRI-O level
// (unable to map host IDs into a 16-bit user namespace slot).
func AnnotateNamespaceForHostUsers(t framework.TestContext, nsName string) {
	for _, c := range t.Clusters() {
		ns, err := c.Kube().CoreV1().Namespaces().Get(context.TODO(), nsName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get namespace %s on cluster %s: %v", nsName, c.Name(), err)
		}
		if ns.Annotations == nil {
			ns.Annotations = make(map[string]string)
		}
		ns.Annotations["openshift.io/sa.scc.uid-range"] = "1000/10000"
		ns.Annotations["openshift.io/sa.scc.supplemental-groups"] = "1000/10000"
		if _, err := c.Kube().CoreV1().Namespaces().Update(context.TODO(), ns, metav1.UpdateOptions{}); err != nil {
			t.Fatalf("failed to annotate namespace %s on cluster %s: %v", nsName, c.Name(), err)
		}
	}
}
