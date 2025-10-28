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

package gateway

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sbeta "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

// TestHTTPRouteNamespaceRevisionInheritance tests that HTTPRoute resources
// correctly inherit revision labels from their namespace when they don't
// have an explicit revision label.
func TestHTTPRouteNamespaceRevisionInheritance(t *testing.T) {
	test.SetForTest(t, &features.EnableAlphaGatewayAPI, true)

	cases := []struct {
		name               string
		revision           string
		namespace          *corev1.Namespace
		httpRoute          *k8sbeta.HTTPRoute
		expectedInRevision bool
		description        string
	}{
		{
			name:     "HTTPRoute without label in namespace with matching revision",
			revision: "test-1",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Labels: map[string]string{
						label.IoIstioRev.Name: "test-1",
					},
				},
			},
			httpRoute: &k8sbeta.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-ns",
					// No istio.io/rev label
				},
				Spec: k8sbeta.HTTPRouteSpec{
					CommonRouteSpec: k8sbeta.CommonRouteSpec{
						ParentRefs: []k8sbeta.ParentReference{{
							Name: "test-gateway",
						}},
					},
				},
			},
			expectedInRevision: true,
			description:        "HTTPRoute should inherit revision from namespace",
		},
		{
			name:     "HTTPRoute without label in namespace with different revision",
			revision: "test-1",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Labels: map[string]string{
						label.IoIstioRev.Name: "test-2",
					},
				},
			},
			httpRoute: &k8sbeta.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-ns",
				},
				Spec: k8sbeta.HTTPRouteSpec{
					CommonRouteSpec: k8sbeta.CommonRouteSpec{
						ParentRefs: []k8sbeta.ParentReference{{
							Name: "test-gateway",
						}},
					},
				},
			},
			expectedInRevision: false,
			description:        "HTTPRoute should not be in revision when namespace has different revision",
		},
		{
			name:     "HTTPRoute with explicit label overrides namespace",
			revision: "test-1",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Labels: map[string]string{
						label.IoIstioRev.Name: "test-2",
					},
				},
			},
			httpRoute: &k8sbeta.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-ns",
					Labels: map[string]string{
						label.IoIstioRev.Name: "test-1", // Explicit label
					},
				},
				Spec: k8sbeta.HTTPRouteSpec{
					CommonRouteSpec: k8sbeta.CommonRouteSpec{
						ParentRefs: []k8sbeta.ParentReference{{
							Name: "test-gateway",
						}},
					},
				},
			},
			expectedInRevision: true,
			description:        "Explicit HTTPRoute label should take precedence over namespace label",
		},
		{
			name:     "HTTPRoute without label in namespace without revision label defaults to test-1 if it's default revision",
			revision: "test-1",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					// No revision label
				},
			},
			httpRoute: &k8sbeta.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-ns",
				},
				Spec: k8sbeta.HTTPRouteSpec{
					CommonRouteSpec: k8sbeta.CommonRouteSpec{
						ParentRefs: []k8sbeta.ParentReference{{
							Name: "test-gateway",
						}},
					},
				},
			},
			expectedInRevision: false,
			description:        "HTTPRoute without revision label in namespace without label is checked via TagWatcher, which returns false when not tagged",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kc := kube.NewFakeClient(tc.namespace, tc.httpRoute)
			setupClientCRDs(t, kc)
			stop := test.NewStop(t)

			c := NewController(
				kc,
				AlwaysReady,
				controller.Options{
					Revision:    tc.revision,
					KrtDebugger: krt.GlobalDebugHandler,
				},
				nil,
			)

			kc.RunAndWait(stop)
			go c.Run(stop)

			// Wait for sync
			kube.WaitForCacheSync("test", stop, c.HasSynced)

			// Test the inRevision function
			result := c.inRevision(tc.httpRoute)
			assert.Equal(t, result, tc.expectedInRevision, tc.description)
		})
	}
}

// Note: The namespace change test would require complex KRT integration testing.
// The key fix is adding krt.FetchOne for the namespace in each route collection,
// which registers the namespace as a dependency. This ensures KRT automatically
// triggers recomputation when the namespace's labels change, solving the issue
// where changing a namespace's revision label didn't trigger route re-evaluation.
