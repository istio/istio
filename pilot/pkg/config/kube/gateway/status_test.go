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

	admissionv1 "k8s.io/api/admissionregistration/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gw "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestStatusAfterRevisionTagRepoint(t *testing.T) {
	tag := &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "istio-revision-tag-default",
			Labels: map[string]string{
				label.IoIstioTag.Name: "default",
				label.IoIstioRev.Name: "stable",
			},
		},
	}
	gateway := &gw.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gwspec", Namespace: "ns1",
			Labels: map[string]string{label.IoIstioRev.Name: "default"},
		},
		Spec: *gatewaySpec.DeepCopy(),
		Status: gw.GatewayStatus{Conditions: []metav1.Condition{
			{Type: "Accepted", Status: metav1.ConditionFalse, Reason: "Stale"},
		}},
	}
	gateway.Spec.AllowedListeners = &gw.AllowedListeners{
		Namespaces: &gw.ListenerNamespaces{From: ptr.Of(gw.NamespacesFromAll)},
	}
	listenerSet := &gw.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "ns1"},
		Spec: gw.ListenerSetSpec{
			ParentRef: gw.ParentGatewayReference{Name: gw.ObjectName(gateway.Name)},
			Listeners: []gw.ListenerEntry{{Name: "extra", Port: 8080, Protocol: gw.HTTPProtocolType}},
		},
		Status: gw.ListenerSetStatus{Conditions: []metav1.Condition{
			{Type: "Accepted", Status: metav1.ConditionFalse, Reason: "Stale"},
		}},
	}
	c := setupControllerWithRevision(t, "canary", tag, gateway, listenerSet,
		// ListenerSets inherit tag ownership from their namespace when unlabeled.
		&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1", Labels: map[string]string{label.IoIstioRev.Name: "default"}}},
		&gw.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: "gwclass"}, Spec: *gatewayClassSpec},
		&gw.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: "route", Namespace: "ns1"}, Spec: *httpRouteSpec},
	)
	q := &TestStatusQueue{state: map[status.Resource]any{}}
	setAndWait(t, c, q)
	for _, written := range q.Statuses() {
		switch written.(type) {
		case *gw.GatewayStatus, *gw.ListenerSetStatus, *gw.HTTPRouteStatus:
			t.Fatalf("unexpected status write before ownership transfer: %+v", written)
		}
	}
	listConfigs := func() []string {
		return slices.Sort(slices.Map(c.List(gvk.Gateway, "ns1"), func(c config.Config) string { return c.Name }))
	}
	gatewayConfigs := listConfigs()
	assert.Equal(t, len(gatewayConfigs), 2)

	// Repoint the real tag watched by the controller. Leave both objects untouched.
	tag = tag.DeepCopy()
	tag.Labels[label.IoIstioRev.Name] = "canary"
	clienttest.NewWriter[*admissionv1.MutatingWebhookConfiguration](t, c.client).Update(tag)
	assert.EventuallyEqual(t, func() bool {
		return c.tagWatcher.AccessUnprotected().IsMine(gateway.ObjectMeta)
	}, true)

	t.Run("Gateway", func(t *testing.T) {
		assert.EventuallyEqual(t, func() string {
			for _, written := range q.Statuses() {
				if s, ok := written.(*gw.GatewayStatus); ok &&
					meta.IsStatusConditionTrue(s.Conditions, "Accepted") &&
					len(s.Listeners) == 1 && s.Listeners[0].AttachedRoutes == 1 {
					return "corrected Gateway status with attached route"
				}
			}
			return "no corrected Gateway status"
		}, "corrected Gateway status with attached route")
	})
	t.Run("ListenerSet", func(t *testing.T) {
		assert.EventuallyEqual(t, func() string {
			for _, written := range q.Statuses() {
				if s, ok := written.(*gw.ListenerSetStatus); ok &&
					meta.IsStatusConditionTrue(s.Conditions, "Accepted") &&
					len(s.Listeners) == 1 && s.Listeners[0].Name == "extra" {
					return "corrected ListenerSet status"
				}
			}
			return "no corrected ListenerSet status"
		}, "corrected ListenerSet status")
	})
	t.Run("HTTPRoute", func(t *testing.T) {
		assert.EventuallyEqual(t, func() string {
			for _, written := range q.Statuses() {
				if s, ok := written.(*gw.HTTPRouteStatus); ok &&
					len(s.Parents) == 1 && meta.IsStatusConditionTrue(s.Parents[0].Conditions, "Accepted") {
					return "corrected HTTPRoute status"
				}
			}
			return "no corrected HTTPRoute status"
		}, "corrected HTTPRoute status")
	})
	t.Run("loses ownership", func(t *testing.T) {
		tag = tag.DeepCopy()
		tag.Labels[label.IoIstioRev.Name] = "stable"
		clienttest.NewWriter[*admissionv1.MutatingWebhookConfiguration](t, c.client).Update(tag)
		assert.EventuallyEqual(t, func() bool {
			return c.tagWatcher.AccessUnprotected().IsMine(gateway.ObjectMeta)
		}, false)

		// A revision that lost the tag must stop writing status for the tagged namespace.
		before := len(q.Statuses())
		mutated := gateway.DeepCopy()
		mutated.Spec.Listeners[0].Name = "renamed"
		clienttest.NewWriter[*gw.Gateway](t, c.client).Update(mutated)
		assert.Equal(t, len(q.Statuses()), before)
	})
	assert.Equal(t, listConfigs(), gatewayConfigs)
}

func TestStatusCollections(t *testing.T) {
	stop := test.NewStop(t)
	fetch := func(q *TestStatusQueue) []string {
		return slices.Sort(slices.Map(q.Statuses(), func(e any) string {
			return *e.(*string)
		}))
	}

	type Status = krt.ObjectWithStatus[*v1.ConfigMap, string]
	c := setupControllerWithoutGatewayClasses(t)
	obj1 := Status{
		Obj:    &v1.ConfigMap{},
		Status: "hello world",
	}
	fakeCol := krt.NewMutableCollection[Status](nil, []Status{obj1}, krt.WithStop(stop))
	status.RegisterStatus(c.status, fakeCol.AsCollection(), func(i *v1.ConfigMap) string {
		return ""
	}, c.tagWatcher, krt.WithStop(stop))

	sq1 := &TestStatusQueue{state: map[status.Resource]any{}}
	setAndWait(t, c, sq1)
	assert.Equal(t, fetch(sq1), []string{"hello world"})

	c.status.UnsetQueue()

	// We should not get an update on the un-registered queue
	fakeCol.UpdateObject(Status{
		Obj:    &v1.ConfigMap{},
		Status: "hello world2",
	})
	assert.Equal(t, fetch(sq1), []string{"hello world"})

	// New queue should send new events, including existing state
	sq2 := &TestStatusQueue{state: map[status.Resource]any{}}
	setAndWait(t, c, sq2)
	assert.Equal(t, fetch(sq2), []string{"hello world2"})
	// And any new state
	fakeCol.UpdateObject(Status{
		Obj:    &v1.ConfigMap{},
		Status: "hello world3",
	})
	// New event, so this is eventually consistent
	assert.EventuallyEqual(t, func() []string {
		return fetch(sq2)
	}, []string{"hello world3"})
}

func setAndWait(t test.Failer, c *Controller, q status.Queue) {
	stop := test.NewStop(t)
	for _, syncer := range c.status.SetQueue(q) {
		syncer.WaitUntilSynced(stop)
	}
}

func setupControllerWithoutGatewayClasses(t *testing.T, objs ...runtime.Object) *Controller {
	kc := kube.NewFakeClient(objs...)
	setupClientCRDs(t, kc)
	stop := test.NewStop(t)
	controller := NewController(
		kc,
		func(class schema.GroupVersionResource, stop <-chan struct{}) bool {
			return false
		},
		controller.Options{KrtDebugger: krt.GlobalDebugHandler},
		nil)
	kc.RunAndWait(stop)
	go controller.Run(stop)
	cg := core.NewConfigGenTest(t, core.TestOptions{})
	controller.Reconcile(cg.PushContext())
	kube.WaitForCacheSync("test", stop, controller.HasSynced)

	return controller
}

func TestMergeLivePolicyStatus(t *testing.T) {
	ancestor := func(name string, controller gw.GatewayController, gen int64) gw.PolicyAncestorStatus {
		return gw.PolicyAncestorStatus{
			AncestorRef:    gw.ParentReference{Name: gw.ObjectName(name)},
			ControllerName: controller,
			Conditions:     []metav1.Condition{{Type: string(gw.PolicyConditionAccepted), ObservedGeneration: gen}},
		}
	}
	mesh := gw.GatewayController(constants.ManagedGatewayMeshController)
	agw := gw.GatewayController(constants.ManagedAgentgatewayController)
	// Another controller already wrote generation 2 for its entry.
	live := &gw.PolicyStatus{Ancestors: []gw.PolicyAncestorStatus{
		ancestor("svc", mesh, 1),
		ancestor("agw-gateway", agw, 2),
	}}
	// Our desired status was computed from an older snapshot, which still has the other controller's entry at generation 1.
	desired := &gw.PolicyStatus{Ancestors: []gw.PolicyAncestorStatus{
		ancestor("svc", mesh, 2),
		ancestor("agw-gateway", agw, 1),
	}}
	got := mergeLivePolicyStatus(live, desired)
	assert.Equal(t, got, any(&gw.PolicyStatus{Ancestors: []gw.PolicyAncestorStatus{
		ancestor("svc", mesh, 2),
		ancestor("agw-gateway", agw, 2),
	}}))
	// Live status must not be mutated.
	assert.Equal(t, live.Ancestors[0], ancestor("svc", mesh, 1))
}
