//go:build integ
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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

func TestClusterTrustBundleInjectionAndRBAC(t *testing.T) {
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		ns := namespace.NewOrFail(ctx, namespace.Config{Inject: true})
		cluster := ctx.Clusters().Default()

		// Deploy with ENABLE_CLUSTER_TRUST_BUNDLE_API=true
		values := map[string]string{
			"pilot.env.ENABLE_CLUSTER_TRUST_BUNDLE_API": "true",
		}
		ctx.ConfigIstio().EvalFile(ns.Name(), values, "tests/integration/pilot/testdata/clustertrustbundle-injection.yaml")

		// Check that the ClusterTrustBundle exists
		dyn := cluster.Dynamic()
		ctbs, err := dyn.Resource(
			schema.GroupVersionResource{
				Group:    "cert-manager.io",
				Version:  "v1",
				Resource: "clustertrustbundles",
			},
		).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			t.Fatalf("failed to list ClusterTrustBundles: %v", err)
		}
		foundCTB := false
		for _, ctb := range ctbs.Items {
			if ctb.GetName() == "test-bundle" {
				foundCTB = true
				break
			}
		}
		if !foundCTB {
			t.Fatalf("ClusterTrustBundle 'test-bundle' not found")
		}

		// Wait for pod
		var pod *corev1.Pod
		retry.UntilSuccessOrFail(t, func() error {
			pods, err := cluster.PodsForSelector(context.Background(), ns.Name(), "app=test-app")
			if err != nil || len(pods.Items) == 0 {
				return fmt.Errorf("no pods: %v", err)
			}
			p := &pods.Items[0]
			if p.Status.Phase != corev1.PodRunning {
				return fmt.Errorf("pod not running: %v", p.Status.Phase)
			}
			pod = p
			return nil
		}, retry.Timeout(2*time.Minute), retry.Delay(2*time.Second))

		// Check for projected clusterTrustBundle volume referencing 'test-bundle'
		found := false
		for _, v := range pod.Spec.Volumes {
			if v.Projected != nil {
				for _, src := range v.Projected.Sources {
					if src.ClusterTrustBundle != nil {
						found = true
						if src.ClusterTrustBundle.Name != nil && *src.ClusterTrustBundle.Name == "test-bundle" {
							// Success
							return
						}
						t.Fatalf("clusterTrustBundle volume found but name mismatch: %+v", src.ClusterTrustBundle)
					}
				}
			}
		}
		if !found {
			t.Fatalf("No projected clusterTrustBundle volume found in pod spec")
		}

		// Check ClusterRole for clustertrustbundles RBAC
		cr, err := cluster.Kube().RbacV1().ClusterRoles().Get(context.Background(), "istiod-clusterrole-default-istio-system", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get ClusterRole: %v", err)
		}
		rbacFound := false
		for _, rule := range cr.Rules {
			if contains(rule.Resources, "clustertrustbundles") && contains(rule.Verbs, "get") {
				rbacFound = true
				break
			}
		}
		if !rbacFound {
			t.Fatalf("ClusterRole missing clustertrustbundles RBAC rule")
		}
	})
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
