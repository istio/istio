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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8s "sigs.k8s.io/gateway-api/apis/v1"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

var (
	gatewayClassSpec = &k8s.GatewayClassSpec{
		ControllerName: k8s.GatewayController(features.ManagedGatewayController),
	}
	gatewaySpec = &k8s.GatewaySpec{
		GatewayClassName: "gwclass",
		Listeners: []k8s.Listener{
			{
				Name:          "default",
				Port:          9009,
				Protocol:      "HTTP",
				AllowedRoutes: &k8s.AllowedRoutes{Namespaces: &k8s.RouteNamespaces{From: func() *k8s.FromNamespaces { x := k8s.NamespacesFromAll; return &x }()}},
			},
		},
	}
	httpRouteSpec = &k8s.HTTPRouteSpec{
		CommonRouteSpec: k8s.CommonRouteSpec{ParentRefs: []k8s.ParentReference{{
			Name: "gwspec",
		}}},
		Hostnames: []k8s.Hostname{"test.cluster.local"},
	}

	expectedgw = &networking.Gateway{
		Servers: []*networking.Server{
			{
				Port: &networking.Port{
					Number:   9009,
					Name:     "default",
					Protocol: "HTTP",
				},
				Hosts: []string{"*/*"},
			},
		},
	}
)

var AlwaysReady = func(class schema.GroupVersionResource, stop <-chan struct{}) bool {
	return true
}

func setupController(t *testing.T, objs ...runtime.Object) *Controller {
	kc := kube.NewFakeClient(objs...)
	setupClientCRDs(t, kc)
	stop := test.NewStop(t)
	controller := NewController(
		kc,
		AlwaysReady,
		controller.Options{KrtDebugger: krt.GlobalDebugHandler},
		nil)
	kc.RunAndWait(stop)
	go controller.Run(stop)
	cg := core.NewConfigGenTest(t, core.TestOptions{})
	controller.Reconcile(cg.PushContext())
	kube.WaitForCacheSync("test", stop, controller.HasSynced)

	return controller
}

func setupControllerWithRevision(t *testing.T, revision string, objs ...runtime.Object) *Controller {
	kc := kube.NewFakeClient(objs...)
	setupClientCRDs(t, kc)
	stop := test.NewStop(t)
	ctrl := NewController(
		kc,
		AlwaysReady,
		controller.Options{
			Revision:    revision,
			KrtDebugger: krt.GlobalDebugHandler,
		},
		nil)
	kc.RunAndWait(stop)
	go ctrl.Run(stop)
	cg := core.NewConfigGenTest(t, core.TestOptions{})
	ctrl.Reconcile(cg.PushContext())
	kube.WaitForCacheSync("test", stop, ctrl.HasSynced)
	return ctrl
}

// TestHTTPRouteVisibleAcrossRevisions verifies that an HTTPRoute labeled with
// one revision is still programmed into a VirtualService by a controller
// running under a different revision. This is the fix for
// https://github.com/istio/istio/issues/58840 where revision-scoped informer
// filtering caused routes to be invisible during multi-revision canary upgrades.
func TestHTTPRouteVisibleAcrossRevisions(t *testing.T) {
	// The controller runs as revision "rev-B".  The Gateway is labeled with
	// "rev-B" so tagWatcher.IsMine claims it.  The HTTPRoute is labeled with
	// "rev-A" (a different revision).  Without the fix, the inRevision
	// informer filter would drop the route entirely, producing zero
	// VirtualServices.
	ctrl := setupControllerWithRevision(t, "rev-B",
		&k8s.GatewayClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gwclass",
			},
			Spec: *gatewayClassSpec,
		},
		&k8s.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gwspec",
				Namespace: "ns1",
				Labels: map[string]string{
					"istio.io/rev": "rev-B",
				},
			},
			Spec: *gatewaySpec,
		},
		&k8s.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cross-rev-route",
				Namespace: "ns1",
				Labels: map[string]string{
					"istio.io/rev": "rev-A",
				},
			},
			Spec: k8s.HTTPRouteSpec{
				CommonRouteSpec: k8s.CommonRouteSpec{ParentRefs: []k8s.ParentReference{{
					Name: "gwspec",
				}}},
				Hostnames: []k8s.Hostname{"test.cluster.local"},
				Rules: []k8s.HTTPRouteRule{{
					BackendRefs: []k8s.HTTPBackendRef{{
						BackendRef: k8s.BackendRef{
							BackendObjectReference: k8s.BackendObjectReference{
								Name: "test-service",
								Port: func() *k8s.PortNumber { p := k8s.PortNumber(80); return &p }(),
							},
						},
					}},
				}},
			},
		})

	dumpOnFailure(t, krt.GlobalDebugHandler)

	// The route should produce a VirtualService even though the route carries
	// a different revision label than the controller.
	vs := ctrl.List(gvk.VirtualService, "ns1")
	if len(vs) == 0 {
		t.Fatalf("expected VirtualService to be generated for cross-revision HTTPRoute, got none")
	}
}

func TestListInvalidGroupVersionKind(t *testing.T) {
	controller := setupController(t)

	typ := config.GroupVersionKind{Kind: "wrong-kind"}
	c := controller.List(typ, "ns1")
	assert.Equal(t, len(c), 0)
}

func TestListGatewayResourceType(t *testing.T) {
	controller := setupController(t,
		&k8s.GatewayClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gwclass",
			},
			Spec: *gatewayClassSpec,
		},
		&k8s.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gwspec",
				Namespace: "ns1",
			},
			Spec: *gatewaySpec,
		},
		&k8s.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "http-route",
				Namespace: "ns1",
			},
			Spec: *httpRouteSpec,
		})

	dumpOnFailure(t, krt.GlobalDebugHandler)
	cfg := controller.List(gvk.Gateway, "ns1")
	assert.Equal(t, len(cfg), 1)
	for _, c := range cfg {
		assert.Equal(t, c.GroupVersionKind, gvk.Gateway)
		assert.Equal(t, c.Name, "gwspec"+"~"+constants.KubernetesGatewayName+"~default")
		assert.Equal(t, c.Namespace, "ns1")
		assert.Equal(t, c.Spec, any(expectedgw))
	}
}
