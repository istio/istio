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
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestNestedJoinCollection(t *testing.T) {
	opts := testOptions(t)
	c1 := krt.NewStatic[Named](nil, true, opts.WithName("c1")...)
	c2 := krt.NewStatic[Named](nil, true, opts.WithName("c2")...)
	c3 := krt.NewStatic[Named](nil, true, opts.WithName("c3")...)
	joined := krt.NewStaticCollection(nil, []krt.Collection[Named]{
		c1.AsCollection(),
		c2.AsCollection(),
		c3.AsCollection(),
	}, opts.WithName("joined")...)

	nj := krt.NestedJoinCollection(
		joined,
		opts.WithName("NestedJoin")...,
	)

	last := atomic.NewString("")
	tt := assert.NewTracker[string](t)
	nj.Register(TrackerHandler[Named](tt))
	nj.Register(func(o krt.Event[Named]) {
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

	tt.WaitOrdered("add/c1/a", "add/c2/a", "add/c3/a", "update/c1/b")
	// ordered by c1, c2, c3
	sortf := func(a Named) string {
		return a.ResourceName()
	}
	assert.Equal(
		t,
		slices.SortBy(nj.List(), sortf),
		slices.SortBy([]Named{
			{"c1", "b"},
			{"c2", "a"},
			{"c3", "a"},
		}, sortf),
	)

	// add c4
	c4 := krt.NewStatic[Named](nil, true, opts.WithName("c4")...)
	joined.UpdateObject(c4.AsCollection())
	c4.Set(&Named{"c4", "a"})
	assert.EventuallyEqual(t, last.Load, "c4/a") // Test that events from the new collection make it to the join
	tt.WaitOrdered("add/c4/a")

	// remove c1
	joined.DeleteObject(krt.GetKey(c1.AsCollection()))
	assert.EventuallyEqual(t, func() int {
		return len(nj.List())
	}, 3)
	// Wait for c1 to be removed
	// We use the event tracker since
	// we can't guarantee the order of events
	tt.WaitUnordered("delete/c1/b")
}

func TestNestedJoinCollectionIndex(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c1 := kube.NewFakeClient()
	kpc1 := kclient.New[*corev1.Pod](c1)
	pc1 := clienttest.Wrap(t, kpc1)
	pods := krt.WrapClient[*corev1.Pod](kpc1, opts.WithName("Pods1")...)
	c1.RunAndWait(stop)
	SimplePods1 := NamedSimplePodCollection(pods, opts, "Pods1")
	simplePods := krt.NewStaticCollection(nil, []krt.Collection[SimplePod]{SimplePods1}, opts.WithName("SimplePods")...)
	SimpleGlobalPods := krt.NestedJoinCollection(
		simplePods,
		opts.WithName("GlobalPods")...,
	)
	tt := assert.NewTracker[string](t)
	IPIndex := krt.NewIndex[string, SimplePod](SimpleGlobalPods, "ips", func(o SimplePod) []string {
		return []string{o.IP}
	})
	fetchSorted := func(ip string) []SimplePod {
		return slices.SortBy(IPIndex.Lookup(ip), func(t SimplePod) string {
			return t.ResourceName()
		})
	}

	SimpleGlobalPods.Register(TrackerHandler[SimplePod](tt))
	assert.EventuallyEqual(t, func() bool {
		return SimpleGlobalPods.WaitUntilSynced(opts.Stop())
	}, true)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.4"},
	}
	pc1.CreateOrUpdateStatus(pod)
	tt.WaitUnordered("add/namespace/name")
	assert.Equal(t, fetchSorted("1.2.3.4"), []SimplePod{{NewNamed(pod), Labeled{}, "1.2.3.4"}})

	c2 := kube.NewFakeClient()
	kpc2 := kclient.New[*corev1.Pod](c2)
	pc2 := clienttest.Wrap(t, kpc2)
	pods2 := krt.WrapClient[*corev1.Pod](kpc2, opts.WithName("Pods2")...)
	c2.RunAndWait(stop)
	SimplePods2 := NamedSimplePodCollection(pods2, opts, "Pods2")
	simplePods.UpdateObject(SimplePods2)

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name2",
			Namespace: "namespace",
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.5"},
	}
	pc2.CreateOrUpdateStatus(pod2)
	tt.WaitUnordered("add/namespace/name2")
	assert.Equal(t, fetchSorted("1.2.3.5"), []SimplePod{{NewNamed(pod2), Labeled{}, "1.2.3.5"}})

	// remove c1
	simplePods.DeleteObject(krt.GetKey(SimplePods1))
	tt.WaitUnordered("delete/namespace/name")
	assert.Equal(t, fetchSorted("1.2.3.4"), []SimplePod{})
}

func TestNestedJoinCollectionSync(t *testing.T) {
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
	SimplePods := NamedSimplePodCollection(pods, opts, "Pods")
	ExtraSimplePods := krt.NewStatic(&SimplePod{
		Named:   Named{"namespace", "name-static"},
		Labeled: Labeled{map[string]string{"app": "foo"}},
		IP:      "9.9.9.9",
	}, true, opts.WithName("Simple")...)
	simplePods := krt.NewStaticCollection(nil, []krt.Collection[SimplePod]{SimplePods, ExtraSimplePods.AsCollection()}, opts.WithName("SimplePods")...)
	AllPods := krt.NestedJoinCollection(
		simplePods,
		opts.WithName("AllPods")...,
	)
	assert.Equal(t, AllPods.WaitUntilSynced(stop), true)
	// Assert Equal -- not EventuallyEqual -- to ensure our WaitForCacheSync is proper.
	// Once changes are made to the outer collection, we'll use EventuallyEqual
	assert.Equal(t, fetcherSorted(AllPods)(), []SimplePod{
		{Named{"namespace", "name"}, NewLabeled(map[string]string{"app": "foo"}), "1.2.3.4"},
		{Named{"namespace", "name-static"}, NewLabeled(map[string]string{"app": "foo"}), "9.9.9.9"},
	})

	c2 := kube.NewFakeClient(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name2",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "bar"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.5"},
	})
	pods2 := krt.NewInformer[*corev1.Pod](c2, opts.WithName("Pods2")...)
	SimplePods2 := NamedSimplePodCollection(pods2, opts, "Pods2")
	ExtraSimplePods2 := krt.NewStatic(&SimplePod{
		Named:   Named{"namespace", "name2-static"},
		Labeled: Labeled{map[string]string{"app": "bar"}},
		IP:      "9.9.9.8",
	}, true, opts.WithName("Simple2")...)
	simplePods.UpdateObject(SimplePods2)
	simplePods.UpdateObject(ExtraSimplePods2.AsCollection())
	// The collection sync == initial sync so this should be true despite the fact
	// that we haven't run the client yet
	assert.Equal(t, AllPods.WaitUntilSynced(stop), true)
	c2.RunAndWait(stop)
	// Assert EventuallyEqual -- not Equal -- because we're querying a collection of a collection
	// so eventually consistency is expected/ok
	assert.EventuallyEqual(t, fetcherSorted(AllPods), []SimplePod{
		{Named{"namespace", "name"}, NewLabeled(map[string]string{"app": "foo"}), "1.2.3.4"},
		{Named{"namespace", "name-static"}, NewLabeled(map[string]string{"app": "foo"}), "9.9.9.9"},
		{Named{"namespace", "name2"}, NewLabeled(map[string]string{"app": "bar"}), "1.2.3.5"},
		{Named{"namespace", "name2-static"}, NewLabeled(map[string]string{"app": "bar"}), "9.9.9.8"},
	})

	simplePods.DeleteObject(krt.GetKey(SimplePods2))
	assert.EventuallyEqual(t, fetcherSorted(AllPods), []SimplePod{
		{Named{"namespace", "name"}, NewLabeled(map[string]string{"app": "foo"}), "1.2.3.4"},
		{Named{"namespace", "name-static"}, NewLabeled(map[string]string{"app": "foo"}), "9.9.9.9"},
		{Named{"namespace", "name2-static"}, NewLabeled(map[string]string{"app": "bar"}), "9.9.9.8"},
	})
}

// This differes from the previous index test in that it passes a
// nested join to a regular collection to ensure that nested collections
// work as expected.
func TestNestedJoinCollectionTransform(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()

	pods := krt.NewInformer[*corev1.Pod](c, opts.WithName("Pods")...)
	MultiPods := krt.NewStaticCollection(
		nil,
		[]krt.Collection[*corev1.Pod]{pods},
		opts.WithName("MultiPods")...,
	)
	AllPods := krt.NestedJoinCollection(
		MultiPods,
		opts.WithName("AllPods")...,
	)
	c.RunAndWait(stop)
	assert.EventuallyEqual(t, func() bool {
		return AllPods.WaitUntilSynced(opts.Stop())
	}, true)

	pc := clienttest.Wrap(t, kclient.New[*corev1.Pod](c))

	SimplePods := krt.NewCollection(AllPods, func(ctx krt.HandlerContext, o *corev1.Pod) *SimplePod {
		return &SimplePod{
			Named:   Named{o.Namespace, o.Name},
			Labeled: Labeled{o.Labels},
			IP:      o.Status.PodIP,
		}
	}, opts.WithName("SimplePods")...)

	tt := assert.NewTracker[string](t)
	IPIndex := krt.NewIndex[string, SimplePod](SimplePods, "ips", func(o SimplePod) []string {
		return []string{o.IP}
	})

	fetchSorted := func(ip string) []SimplePod {
		return slices.SortBy(IPIndex.Lookup(ip), func(t SimplePod) string {
			return t.ResourceName()
		})
	}

	SimplePods.Register(TrackerHandler[SimplePod](tt))

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.4"},
	}
	pc.CreateOrUpdateStatus(pod)
	tt.WaitUnordered("add/namespace/name")
	assert.Equal(t, fetchSorted("1.2.3.4"), []SimplePod{{NewNamed(pod), Labeled{}, "1.2.3.4"}})

	c2 := kube.NewFakeClient()
	kpc2 := kclient.New[*corev1.Pod](c2)
	pc2 := clienttest.Wrap(t, kpc2)
	pods2 := krt.WrapClient[*corev1.Pod](kpc2, opts.WithName("Pods2")...)
	c2.RunAndWait(stop)
	MultiPods.UpdateObject(pods2)

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name2",
			Namespace: "namespace",
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.5"},
	}
	pc2.CreateOrUpdateStatus(pod2)
	tt.WaitUnordered("add/namespace/name2")
	assert.Equal(t, fetchSorted("1.2.3.5"), []SimplePod{{NewNamed(pod2), Labeled{}, "1.2.3.5"}})
}

func TestNestedJoinWithMergeSimpleCollection(t *testing.T) {
	opts := testOptions(t)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "foo"},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
			},
			ClusterIP: "1.2.3.4",
		},
	}

	c := kube.NewFakeClient(svc)
	services1 := krt.NewInformer[*corev1.Service](c, opts.WithName("Services")...)
	SimpleServices := krt.NewCollection(services1, func(ctx krt.HandlerContext, o *corev1.Service) *SimpleService {
		return &SimpleService{
			Named:    Named{o.Namespace, o.Name},
			Selector: o.Spec.Selector,
		}
	}, opts.WithName("SimpleServices")...)
	MultiServices := krt.NewStaticCollection(
		nil,
		[]krt.Collection[SimpleService]{SimpleServices},
		opts.WithName("MultiServices")...,
	)

	AllServices := krt.NestedJoinWithMergeCollection(
		MultiServices,
		func(ts []SimpleService) *SimpleService {
			if len(ts) == 0 {
				return nil
			}

			simpleService := SimpleService{
				Named:    ts[0].Named,
				Selector: maps.Clone(ts[0].Selector),
			}

			for i, t := range ts {
				if i == 0 {
					continue
				}
				// SimpleService values always take precedence
				newSelector := maps.MergeCopy(t.Selector, simpleService.Selector)
				simpleService.Selector = newSelector
			}

			// For the purposes of this test, the "app" label should always
			// be set to "foo" if it exists
			if _, ok := simpleService.Selector["app"]; ok {
				simpleService.Selector["app"] = "foo"
			}

			return &simpleService
		},
		opts.With(
			krt.WithName("AllServices"),
		)...,
	)
	tt := assert.NewTracker[string](t)
	AllServices.RegisterBatch(BatchedTrackerHandler[SimpleService](tt), true)
	c.RunAndWait(opts.Stop())
	tt.WaitOrdered("add/namespace/svc")

	assert.EventuallyEqual(t, func() bool {
		return AllServices.WaitUntilSynced(opts.Stop())
	}, true)

	svc2 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "bar", "version": "v1"},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
			},
			ClusterIP: "1.2.3.4",
		},
	}

	c2 := kube.NewFakeClient(svc2)
	services2 := krt.NewInformer[*corev1.Service](c2, opts.WithName("Services")...)
	SimpleServices2 := krt.NewCollection(services2, func(ctx krt.HandlerContext, o *corev1.Service) *SimpleService {
		return &SimpleService{
			Named:    Named{o.Namespace, o.Name},
			Selector: o.Spec.Selector,
		}
	}, opts.WithName("SimpleServices2")...)
	c2.RunAndWait(opts.Stop())

	MultiServices.UpdateObject(SimpleServices2)

	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	}, &SimpleService{
		Named:    Named{"namespace", "svc"},
		Selector: map[string]string{"app": "foo", "version": "v1"},
	})

	// Have to wait a bit for the events to propagate due to client syncing
	// But what we want is the original add and then an update because the
	// merged value changed
	tt.WaitOrdered("update/namespace/svc")

	// Now delete one of the collections
	MultiServices.DeleteObject(krt.GetKey(SimpleServices2))
	// This should be another update event, not a delete event
	tt.WaitOrdered("update/namespace/svc")
	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	},
		&SimpleService{
			Named:    Named{"namespace", "svc"},
			Selector: map[string]string{"app": "foo"},
		},
	)

	// Now delete the other collection; this should be a delete event
	MultiServices.DeleteObject(krt.GetKey(SimpleServices))
	tt.WaitOrdered("delete/namespace/svc")
	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	}, nil)

	// Now add the two collections back
	MultiServices.UpdateObject(SimpleServices)
	MultiServices.UpdateObject(SimpleServices2)
	tt.WaitOrdered("add/namespace/svc", "update/namespace/svc")
	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	}, &SimpleService{
		Named:    Named{"namespace", "svc"},
		Selector: map[string]string{"app": "foo", "version": "v1"},
	})
}
