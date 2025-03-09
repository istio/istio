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
	"context"
	"testing"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestDynamicJoinCollection(t *testing.T) {
	opts := testOptions(t)
	c1 := krt.NewStatic[Named](nil, true, opts.WithName("c1")...)
	c2 := krt.NewStatic[Named](nil, true, opts.WithName("c2")...)
	c3 := krt.NewStatic[Named](nil, true, opts.WithName("c3")...)
	dj := krt.DynamicJoinCollection(
		[]krt.Collection[Named]{c1.AsCollection(), c2.AsCollection(), c3.AsCollection()},
		opts.WithName("DynamicJoin")...,
	)

	last := atomic.NewString("")
	dj.Register(func(o krt.Event[Named]) {
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
		slices.SortBy(dj.List(), sortf),
		slices.SortBy([]Named{
			{"c1", "b"},
			{"c2", "a"},
			{"c3", "a"},
		}, sortf),
	)

	// add c4
	c4 := krt.NewStatic[Named](nil, true, opts.WithName("c4")...)
	dj.AddOrUpdateCollection(c4.AsCollection())
	c4.Set(&Named{"c4", "a"})
	assert.EventuallyEqual(t, last.Load, "c4/a") // Test that events from the new collection make it to the join

	// remove c1
	dj.RemoveCollection(c1.AsCollection())
	c1.Set(&Named{"c1", "c"})
	assert.EventuallyEqual(t, last.Load, "c4/a") // Test that events from removed collections do not make it to the join
}

func TestDynamicJoinCollectionIndex(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c1 := kube.NewFakeClient()
	kpc1 := kclient.New[*corev1.Pod](c1)
	pc1 := clienttest.Wrap(t, kpc1)
	pods := krt.WrapClient[*corev1.Pod](kpc1, opts.WithName("Pods1")...)
	c1.RunAndWait(stop)
	SimplePods1 := NamedSimplePodCollection(pods, opts, "Pods1")
	SimpleGlobalPods := krt.DynamicJoinCollection(
		[]krt.Collection[SimplePod]{SimplePods1},
		opts.WithName("GlobalPods")...,
	)
	tt := assert.NewTracker[string](t)
	IPIndex := krt.NewIndex[string, SimplePod](SimpleGlobalPods, func(o SimplePod) []string {
		return []string{o.IP}
	})
	fetchSorted := func(ip string) []SimplePod {
		return slices.SortBy(IPIndex.Lookup(ip), func(t SimplePod) string {
			return t.ResourceName()
		})
	}

	SimpleGlobalPods.Register(TrackerHandler[SimplePod](tt))

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

	SimpleGlobalPods.AddOrUpdateCollection(SimplePods2)
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
	SimpleGlobalPods.RemoveCollection(SimplePods1)
	tt.WaitUnordered("delete/namespace/name")
	assert.Equal(t, fetchSorted("1.2.3.4"), []SimplePod{})
}

func TestDynamicCollectionJoinSync(t *testing.T) {
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
	AllPods := krt.DynamicJoinCollection(
		[]krt.Collection[SimplePod]{SimplePods, ExtraSimplePods.AsCollection()},
		opts.WithName("AllPods")...,
	)
	assert.Equal(t, AllPods.WaitUntilSynced(stop), true)
	// Assert Equal -- not EventuallyEqual -- to ensure our WaitForCacheSync is proper
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
	AllPods.AddOrUpdateCollection(ExtraSimplePods2.AsCollection())
	AllPods.AddOrUpdateCollection(SimplePods2)
	// Should be false since we never ran the fake client
	// Use a context for a convenient timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel() // make the linter happy
	assert.Equal(t, AllPods.WaitUntilSynced(ctx.Done()), false)
	c2.RunAndWait(stop)
	assert.Equal(t, AllPods.WaitUntilSynced(stop), true)
	// Assert Equal -- not EventuallyEqual -- to ensure our WaitForCacheSync is proper
	assert.Equal(t, fetcherSorted(AllPods)(), []SimplePod{
		{Named{"namespace", "name"}, NewLabeled(map[string]string{"app": "foo"}), "1.2.3.4"},
		{Named{"namespace", "name-static"}, NewLabeled(map[string]string{"app": "foo"}), "9.9.9.9"},
		{Named{"namespace", "name2"}, NewLabeled(map[string]string{"app": "bar"}), "1.2.3.5"},
		{Named{"namespace", "name2-static"}, NewLabeled(map[string]string{"app": "bar"}), "9.9.9.8"},
	})

	AllPods.RemoveCollection(SimplePods2)
	assert.Equal(t, fetcherSorted(AllPods)(), []SimplePod{
		{Named{"namespace", "name"}, NewLabeled(map[string]string{"app": "foo"}), "1.2.3.4"},
		{Named{"namespace", "name-static"}, NewLabeled(map[string]string{"app": "foo"}), "9.9.9.9"},
		{Named{"namespace", "name2-static"}, NewLabeled(map[string]string{"app": "bar"}), "9.9.9.8"},
	})
}
