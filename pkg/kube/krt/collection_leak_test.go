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
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/tests/util/leak"
)

// leakSources holds a set of long-lived source collections used by the leak
// tests below. These collections outlive the derived collections under test, so
// any handler registration a derived collection leaves behind on them (after it
// is stopped) shows up as a leaked goroutine.
type leakSources struct {
	opts        krt.OptionsBuilder
	stop        <-chan struct{}
	pods        krt.Collection[SimplePod]
	services    krt.Collection[SimpleService]
	rawServices krt.Collection[*corev1.Service]
}

// setupLeakSources builds long-lived pod and service collections seeded with a
// single matching pod/service, and returns them along with the shared options
// and stop channel.
func setupLeakSources(t *testing.T) leakSources {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()

	kpc := kclient.New[*corev1.Pod](c)
	pc := clienttest.Wrap(t, kpc)
	pods := krt.WrapClient(kpc, opts.WithName("Pods")...)

	ksc := kclient.New[*corev1.Service](c)
	sc := clienttest.Wrap(t, ksc)
	services := krt.WrapClient(ksc, opts.WithName("Services")...)
	c.RunAndWait(stop)

	SimplePods := SimplePodCollection(pods, opts)
	SimpleServices := SimpleServiceCollection(services, opts)
	assert.Equal(t, SimplePods.WaitUntilSynced(stop), true)
	assert.Equal(t, SimpleServices.WaitUntilSynced(stop), true)

	pc.Create(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", Labels: map[string]string{"app": "foo"}},
		Status:     corev1.PodStatus{PodIP: "1.2.3.4"},
	})
	sc.Create(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "foo"}},
	})

	return leakSources{opts: opts, stop: stop, pods: SimplePods, services: SimpleServices, rawServices: services}
}

// TestCollectionDependencyLeak verifies that stopping a derived NewManyCollection
// (via its own stop channel), while the collections it depends on remain alive,
// does not leak the handler registrations (and their goroutines) it created on
// those collections -- both the primary parent subscription and any Fetch
// dependency subscriptions.
func TestCollectionDependencyLeak(t *testing.T) {
	s := setupLeakSources(t)
	leak.Check(t)

	const iterations = 20
	for i := 0; i < iterations; i++ {
		derivedStop := make(chan struct{})
		endpoints := krt.NewManyCollection(
			s.services,
			func(ctx krt.HandlerContext, svc SimpleService) []SimpleEndpoint {
				matched := krt.Fetch(ctx, s.pods, krt.FilterLabel(svc.Selector))
				return slices.Map(matched, func(pod SimplePod) SimpleEndpoint {
					return SimpleEndpoint{Pod: pod.Name, Service: svc.Name, Namespace: svc.Namespace, IP: pod.IP}
				})
			},
			s.opts.With(krt.WithName("Endpoints"), krt.WithStop(derivedStop))...,
		)
		assert.Equal(t, endpoints.WaitUntilSynced(s.stop), true)
		assert.EventuallyEqual(t, func() int { return len(endpoints.List()) }, 1)
		close(derivedStop)
	}
}

// TestMergeJoinCollectionLeak verifies that stopping a JoinWithMergeCollection
// does not leak the handler registrations it created on its (still-alive) source
// collections.
func TestMergeJoinCollectionLeak(t *testing.T) {
	s := setupLeakSources(t)
	leak.Check(t)

	merge := func(ts []SimplePod) *SimplePod { return &ts[0] }

	const iterations = 20
	for i := 0; i < iterations; i++ {
		derivedStop := make(chan struct{})
		merged := krt.JoinWithMergeCollection(
			[]krt.Collection[SimplePod]{s.pods},
			merge,
			s.opts.With(krt.WithName("MergedPods"), krt.WithStop(derivedStop))...,
		)
		assert.Equal(t, merged.WaitUntilSynced(s.stop), true)
		assert.EventuallyEqual(t, func() int { return len(merged.List()) }, 1)
		close(derivedStop)
	}
}

// TestNestedJoinWithMergeCollectionLeak verifies that stopping a
// NestedJoinWithMergeCollection does not leak the handler registrations it created
// on the (still-alive) container collection or its sub-collections.
func TestNestedJoinWithMergeCollectionLeak(t *testing.T) {
	s := setupLeakSources(t)

	// A long-lived container collection holding a single sub-collection.
	multi := krt.NewStaticCollection(
		nil,
		[]krt.Collection[SimplePod]{s.pods},
		s.opts.WithName("MultiPods")...,
	)
	assert.Equal(t, multi.WaitUntilSynced(s.stop), true)

	leak.Check(t)

	merge := func(ts []SimplePod) *SimplePod { return &ts[0] }

	const iterations = 20
	for i := 0; i < iterations; i++ {
		derivedStop := make(chan struct{})
		merged := krt.NestedJoinWithMergeCollection(
			multi,
			merge,
			s.opts.With(krt.WithName("NestedMergedPods"), krt.WithStop(derivedStop))...,
		)
		assert.Equal(t, merged.WaitUntilSynced(s.stop), true)
		assert.EventuallyEqual(t, func() int { return len(merged.List()) }, 1)
		close(derivedStop)
	}
}

// TestJoinCollectionLeak guards JoinCollection in checked mode (>=2 sub-collections).
// Checked joins subscribe to every sub-collection at construction and rely on their
// own dedicated stop-cleanup goroutine (separate from the manyCollection teardown
// path) to unregister them; this ensures that cleanup keeps working.
func TestJoinCollectionLeak(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)

	c1 := krt.NewStaticCollection(nil, []Named{{Namespace: "ns", Name: "a"}}, opts.WithName("Join1")...)
	c2 := krt.NewStaticCollection(nil, []Named{{Namespace: "ns", Name: "b"}}, opts.WithName("Join2")...)
	assert.Equal(t, c1.WaitUntilSynced(stop), true)
	assert.Equal(t, c2.WaitUntilSynced(stop), true)

	leak.Check(t)

	const iterations = 20
	for i := 0; i < iterations; i++ {
		derivedStop := make(chan struct{})
		joined := krt.JoinCollection(
			[]krt.Collection[Named]{c1, c2},
			opts.With(krt.WithName("Joined"), krt.WithStop(derivedStop))...,
		)
		assert.Equal(t, joined.WaitUntilSynced(stop), true)
		assert.EventuallyEqual(t, func() int { return len(joined.List()) }, 2)
		close(derivedStop)
	}
}

// TestJoinCollectionUncheckedLeak guards JoinCollection in unchecked mode. Unchecked
// joins currently hold no construction-time subscription (handlers register lazily and
// are caller-owned), so stopping one should leak nothing. This guards against a future
// change that adds construction-time subscriptions to the unchecked path without
// wiring up teardown.
func TestJoinCollectionUncheckedLeak(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)

	c1 := krt.NewStaticCollection(nil, []Named{{Namespace: "ns", Name: "a"}}, opts.WithName("UJoin1")...)
	c2 := krt.NewStaticCollection(nil, []Named{{Namespace: "ns", Name: "b"}}, opts.WithName("UJoin2")...)
	assert.Equal(t, c1.WaitUntilSynced(stop), true)
	assert.Equal(t, c2.WaitUntilSynced(stop), true)

	leak.Check(t)

	const iterations = 20
	for i := 0; i < iterations; i++ {
		derivedStop := make(chan struct{})
		joined := krt.JoinCollection(
			[]krt.Collection[Named]{c1, c2},
			opts.With(krt.WithName("UJoined"), krt.WithStop(derivedStop), krt.WithJoinUnchecked())...,
		)
		assert.Equal(t, joined.WaitUntilSynced(stop), true)
		assert.EventuallyEqual(t, func() int { return len(joined.List()) }, 2)
		close(derivedStop)
	}
}

// TestSingletonCollectionLeak guards NewSingleton, which is implemented on top of a
// manyCollection (over an internal dummy collection) that Fetches other collections.
// This ensures the singleton wrapper does not leak the Fetch subscription it makes on a
// still-alive dependency when it is stopped.
func TestSingletonCollectionLeak(t *testing.T) {
	s := setupLeakSources(t)
	leak.Check(t)

	const iterations = 20
	for i := 0; i < iterations; i++ {
		derivedStop := make(chan struct{})
		single := krt.NewSingleton(
			func(ctx krt.HandlerContext) *Named {
				pods := krt.Fetch(ctx, s.pods)
				return &Named{Namespace: "ns", Name: fmt.Sprintf("count-%d", len(pods))}
			},
			s.opts.With(krt.WithName("Singleton"), krt.WithStop(derivedStop))...,
		)
		assert.Equal(t, single.AsCollection().WaitUntilSynced(s.stop), true)
		assert.EventuallyEqual(t, func() *Named { return single.Get() }, &Named{Namespace: "ns", Name: "count-1"})
		close(derivedStop)
	}
}

// TestStatusCollectionLeak guards NewStatusCollection, another public constructor built
// on manyCollection. It subscribes to its parent input and Fetches a dependency; stopping
// it must unregister both from the still-alive inputs.
func TestStatusCollectionLeak(t *testing.T) {
	s := setupLeakSources(t)
	leak.Check(t)

	const iterations = 20
	for i := 0; i < iterations; i++ {
		derivedStop := make(chan struct{})
		_, primary := krt.NewStatusCollection(
			s.rawServices,
			func(ctx krt.HandlerContext, svc *corev1.Service) (*string, *Named) {
				pods := krt.Fetch(ctx, s.pods, krt.FilterLabel(svc.Spec.Selector))
				if len(pods) == 0 {
					st := "empty"
					return &st, nil
				}
				st := "ok"
				return &st, &Named{Namespace: svc.Namespace, Name: svc.Name}
			},
			s.opts.With(krt.WithName("Status"), krt.WithStop(derivedStop))...,
		)
		assert.Equal(t, primary.WaitUntilSynced(s.stop), true)
		assert.EventuallyEqual(t, func() int { return len(primary.List()) }, 1)
		close(derivedStop)
	}
}
