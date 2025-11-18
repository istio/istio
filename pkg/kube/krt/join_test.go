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

package krt_test

import (
	"testing"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	istio "istio.io/api/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

// NamedValue has the same resource name (from Named) but different values
// for testing conflict resolution with overlapping keys
type NamedValue struct {
	Named
	Value string
}

func TestJoinCollection(t *testing.T) {
	opts := testOptions(t)
	c1 := krt.NewStatic[Named](nil, true, krt.WithName("c1"))
	c2 := krt.NewStatic[Named](nil, true, krt.WithName("c2"))
	c3 := krt.NewStatic[Named](nil, true, krt.WithName("c3"))
	j := krt.JoinCollection(
		[]krt.Collection[Named]{c1.AsCollection(), c2.AsCollection(), c3.AsCollection()},
		opts.WithName("Join")...,
	)
	last := atomic.NewString("")
	j.Register(func(o krt.Event[Named]) {
		last.Store(o.Latest().ResourceName())
	})
	assert.EventuallyEqual(t, last.Load, "")
	c1.Set(&Named{"c1", "a"})
	assert.EventuallyEqual(t, last.Load, "c1/a")

	c2.Set(&Named{"c2", "a"})
	assert.EventuallyEqual(t, last.Load, "c2/a")

	c3.Set(&Named{"c3", "a"})
	assert.EventuallyEqual(t, last.Load, "c3/a")

	c1.Set(&Named{"c1", "b"})
	assert.EventuallyEqual(t, last.Load, "c1/b")
	// ordered by c1, c2, c3
	sortf := func(a Named) string {
		return a.ResourceName()
	}
	assert.Equal(
		t,
		slices.SortBy(j.List(), sortf),
		slices.SortBy([]Named{
			{"c1", "b"},
			{"c2", "a"},
			{"c3", "a"},
		}, sortf),
	)
}

func TestCollectionJoin(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()
	pods := krt.NewInformer[*corev1.Pod](c, opts.WithName("Pods")...)
	services := krt.NewInformer[*corev1.Service](c, opts.WithName("Services")...)
	serviceEntries := krt.NewInformer[*istioclient.ServiceEntry](c, opts.WithName("ServiceEntrys")...)
	c.RunAndWait(stop)
	pc := clienttest.Wrap(t, kclient.New[*corev1.Pod](c))
	sc := clienttest.Wrap(t, kclient.New[*corev1.Service](c))
	sec := clienttest.Wrap(t, kclient.New[*istioclient.ServiceEntry](c))
	SimplePods := SimplePodCollection(pods, opts)
	ExtraSimplePods := krt.NewStatic(&SimplePod{
		Named:   Named{"namespace", "name-static"},
		Labeled: Labeled{map[string]string{"app": "foo"}},
		IP:      "9.9.9.9",
	}, true)
	SimpleServices := SimpleServiceCollection(services, opts)
	SimpleServiceEntries := SimpleServiceCollectionFromEntries(serviceEntries, opts)
	AllServices := krt.JoinCollection(
		[]krt.Collection[SimpleService]{SimpleServices, SimpleServiceEntries},
		opts.WithName("AllServices")...,
	)
	AllPods := krt.JoinCollection(
		[]krt.Collection[SimplePod]{SimplePods, ExtraSimplePods.AsCollection()},
		opts.WithName("AllPods")...,
	)
	SimpleEndpoints := SimpleEndpointsCollection(AllPods, AllServices, opts)

	fetch := func() []SimpleEndpoint {
		return slices.SortBy(SimpleEndpoints.List(), func(s SimpleEndpoint) string { return s.ResourceName() })
	}

	assert.Equal(t, fetch(), nil)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "foo"},
		},
	}
	pc.Create(pod)
	assert.Equal(t, fetch(), nil)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "foo"}},
	}
	sc.Create(svc)

	pod.Status = corev1.PodStatus{PodIP: "1.2.3.4"}
	pc.UpdateStatus(pod)

	assert.EventuallyEqual(t, fetch, []SimpleEndpoint{
		{pod.Name, svc.Name, pod.Namespace, "1.2.3.4"},
		{"name-static", svc.Name, pod.Namespace, "9.9.9.9"},
	})

	ExtraSimplePods.Set(nil)
	assert.EventuallyEqual(t, fetch, []SimpleEndpoint{
		{pod.Name, svc.Name, pod.Namespace, "1.2.3.4"},
	})

	pod.Status.PodIP = "1.2.3.5"
	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, fetch, []SimpleEndpoint{{pod.Name, svc.Name, pod.Namespace, "1.2.3.5"}})

	pc.Delete(pod.Name, pod.Namespace)
	assert.EventuallyEqual(t, fetch, nil)

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name2",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "foo"},
		},
		Status: corev1.PodStatus{PodIP: "2.3.4.5"},
	}
	pc.CreateOrUpdateStatus(pod)
	pc.CreateOrUpdateStatus(pod2)
	assert.EventuallyEqual(t, fetch, []SimpleEndpoint{
		{pod.Name, svc.Name, pod.Namespace, pod.Status.PodIP},
		{pod2.Name, svc.Name, pod2.Namespace, pod2.Status.PodIP},
	})

	se := &istioclient.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-entry",
			Namespace: "namespace",
		},
		Spec: istio.ServiceEntry{WorkloadSelector: &istio.WorkloadSelector{Labels: map[string]string{"app": "foo"}}},
	}
	sec.Create(se)
	assert.EventuallyEqual(t, fetch, []SimpleEndpoint{
		{pod.Name, se.Name, pod.Namespace, pod.Status.PodIP},
		{pod2.Name, se.Name, pod2.Namespace, pod2.Status.PodIP},
		{pod.Name, svc.Name, pod.Namespace, pod.Status.PodIP},
		{pod2.Name, svc.Name, pod2.Namespace, pod2.Status.PodIP},
	})
}

func TestCollectionJoinSync(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "foo"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.4"},
	})
	pods := krt.NewInformer[*corev1.Pod](c, opts.WithName("Pods")...)
	c.RunAndWait(stop)
	SimplePods := SimplePodCollection(pods, opts)
	ExtraSimplePods := krt.NewStatic(&SimplePod{
		Named:   Named{"namespace", "name-static"},
		Labeled: Labeled{map[string]string{"app": "foo"}},
		IP:      "9.9.9.9",
	}, true)
	AllPods := krt.JoinCollection(
		[]krt.Collection[SimplePod]{SimplePods, ExtraSimplePods.AsCollection()},
		opts.WithName("AllPods")...,
	)
	assert.Equal(t, AllPods.WaitUntilSynced(stop), true)
	// Assert Equal -- not EventuallyEqual -- to ensure our WaitForCacheSync is proper
	assert.Equal(t, fetcherSorted(AllPods)(), []SimplePod{
		{Named{"namespace", "name"}, NewLabeled(map[string]string{"app": "foo"}), "1.2.3.4"},
		{Named{"namespace", "name-static"}, NewLabeled(map[string]string{"app": "foo"}), "9.9.9.9"},
	})
}

// TestJoinCollectionConflictResolution tests conflict resolution with overlapping keys.
// Priority order: c1 > c2 > c3 (first collection wins)
func TestJoinCollectionConflictResolution(t *testing.T) {
	opts := testOptions(t)
	c1 := krt.NewStatic[NamedValue](nil, true, krt.WithName("c1"))
	c2 := krt.NewStatic[NamedValue](nil, true, krt.WithName("c2"))
	c3 := krt.NewStatic[NamedValue](nil, true, krt.WithName("c3"))

	j := krt.JoinCollection(
		[]krt.Collection[NamedValue]{c1.AsCollection(), c2.AsCollection(), c3.AsCollection()},
		opts.WithName("Join")...,
	)

	events := []krt.Event[NamedValue]{}
	j.Register(func(o krt.Event[NamedValue]) {
		events = append(events, o)
	})

	// Test 1: c2 adds key "a" first - should see ADD
	c2.Set(&NamedValue{Named{"ns", "a"}, "c2-value"})
	assert.EventuallyEqual(t, func() int { return len(events) }, 1)
	assert.Equal(t, events[0].Event, controllers.EventAdd)
	assert.Equal(t, events[0].New.Value, "c2-value")

	// Test 2: c1 adds same key - should see UPDATE (c1 has higher priority)
	events = nil
	c1.Set(&NamedValue{Named{"ns", "a"}, "c1-value"})
	assert.EventuallyEqual(t, func() int { return len(events) }, 1)
	assert.Equal(t, events[0].Event, controllers.EventUpdate)
	assert.Equal(t, events[0].Old.Value, "c2-value")
	assert.Equal(t, events[0].New.Value, "c1-value")

	// Test 3: c3 adds same key - should see NO event (c1 has priority)
	events = nil
	c3.Set(&NamedValue{Named{"ns", "a"}, "c3-value"})
	assert.Consistently(t, func() int { return len(events) }, 0)
	assert.Equal(t, j.GetKey("ns/a").Value, "c1-value")

	// Test 4: c2 updates - should see NO event (c1 still owns it)
	events = nil
	c2.Set(&NamedValue{Named{"ns", "a"}, "c2-updated"})
	assert.Consistently(t, func() int { return len(events) }, 0)

	// Test 5: c1 deletes - should see UPDATE to c2's version (fallback)
	events = nil
	c1.Set(nil)
	assert.EventuallyEqual(t, func() int { return len(events) }, 1)
	assert.Equal(t, events[0].Event, controllers.EventUpdate)
	assert.Equal(t, events[0].Old.Value, "c1-value")
	assert.Equal(t, events[0].New.Value, "c2-updated")

	// Test 6: c2 deletes - should see UPDATE to c3's version
	events = nil
	c2.Set(nil)
	assert.EventuallyEqual(t, func() int { return len(events) }, 1)
	assert.Equal(t, events[0].Event, controllers.EventUpdate)
	assert.Equal(t, events[0].Old.Value, "c2-updated")
	assert.Equal(t, events[0].New.Value, "c3-value")

	// Test 7: c3 deletes - should see DELETE (no more fallbacks)
	events = nil
	c3.Set(nil)
	assert.EventuallyEqual(t, func() int { return len(events) }, 1)
	assert.Equal(t, events[0].Event, controllers.EventDelete)
	assert.Equal(t, events[0].Old.Value, "c3-value")

	// Test 8: Delete from lower priority collection when higher priority owns it - NO event
	events = nil
	c1.Set(&NamedValue{Named{"ns", "b"}, "c1-b"})
	assert.EventuallyEqual(t, func() int { return len(events) }, 1) // Wait for c1's ADD

	events = nil
	c2.Set(&NamedValue{Named{"ns", "b"}, "c2-b"})
	assert.Consistently(t, func() int { return len(events) }, 0) // c2 adding should be ignored

	c2.Set(nil) // c2 deletes but c1 still owns it
	assert.Consistently(t, func() int { return len(events) }, 0) // c2 delete should be ignored
	assert.Equal(t, j.GetKey("ns/b").Value, "c1-b")
}
