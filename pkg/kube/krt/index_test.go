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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestIndex(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	pc := clienttest.Wrap(t, kpc)
	pods := krt.WrapClient[*corev1.Pod](kpc, opts.WithName("Pods")...)
	c.RunAndWait(stop)
	SimplePods := SimplePodCollection(pods, opts)
	tt := assert.NewTracker[string](t)
	IPIndex := krt.NewIndex[string, SimplePod](SimplePods, "ip", func(o SimplePod) []string {
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

	pod.Status.PodIP = "1.2.3.5"
	pc.UpdateStatus(pod)
	tt.WaitUnordered("update/namespace/name")
	assert.Equal(t, fetchSorted("1.2.3.4"), []SimplePod{})
	assert.Equal(t, fetchSorted("1.2.3.5"), []SimplePod{{NewNamed(pod), Labeled{}, "1.2.3.5"}})

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name2",
			Namespace: "namespace",
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.5"},
	}
	pc.CreateOrUpdateStatus(pod2)
	tt.WaitUnordered("add/namespace/name2")
	assert.Equal(t, fetchSorted("1.2.3.5"), []SimplePod{
		{NewNamed(pod), Labeled{}, "1.2.3.5"},
		{NewNamed(pod2), Labeled{}, "1.2.3.5"},
	})

	pc.Delete(pod.Name, pod.Namespace)
	pc.Delete(pod2.Name, pod2.Namespace)
	tt.WaitUnordered("delete/namespace/name", "delete/namespace/name2")
	assert.Equal(t, fetchSorted("1.2.3.4"), []SimplePod{})
}

func TestIndexCollection(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	pc := clienttest.Wrap(t, kpc)
	podsCol := krt.WrapClient[*corev1.Pod](kpc, opts.WithName("Pods")...)
	c.RunAndWait(stop)
	SimplePods := SimplePodCollection(podsCol, opts)
	tt := assert.NewTracker[string](t)
	IPIndex := krt.NewIndex[string, SimplePod](SimplePods, "ip", func(o SimplePod) []string {
		return []string{o.IP}
	})
	LabelIndex := krt.NewIndex[string, SimplePod](SimplePods, "label", func(o SimplePod) []string {
		var out []string
		for k, v := range o.GetLabels() {
			out = append(out, k+"="+v)
		}
		return out
	})
	Collection := krt.NewSingleton[string](func(ctx krt.HandlerContext) *string {
		// two fetches by the same index
		a := krt.Fetch(ctx, SimplePods, krt.FilterIndex(IPIndex, "2.2.2.2"))
		b := krt.Fetch(ctx, SimplePods, krt.FilterIndex(IPIndex, "3.3.3.3"))
		// a third fetch on the same SimplePods but with another index
		c := krt.Fetch(ctx, SimplePods, krt.FilterIndex(LabelIndex, "marker=true"))

		pods := append(a, b...)
		pods = append(pods, c...)

		names := slices.Sort(slices.Map(pods, SimplePod.ResourceName))
		return ptr.Of(strings.Join(names, ","))
	}, opts.WithName("Collection")...)
	Collection.AsCollection().WaitUntilSynced(stop)
	fetchSorted := func(ip string) []SimplePod {
		return slices.SortBy(IPIndex.Lookup(ip), func(t SimplePod) string {
			return t.ResourceName()
		})
	}

	SimplePods.Register(TrackerHandler[SimplePod](tt))

	var pods []*corev1.Pod
	for i := range 4 {
		pods = append(pods, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "name" + strconv.Itoa(i+1),
				Namespace: "namespace",
				Labels:    map[string]string{},
			},
			Status: corev1.PodStatus{PodIP: fmt.Sprintf("%d.%d.%d.%d", i+1, i+1, i+1, i+1)},
		})
	}
	pod := pods[0]
	pod2 := pods[1]
	pod3 := pods[2]
	pod4 := pods[3]

	// pod 1 with 1.1.1.1 doesn't show up in the collection
	pc.CreateOrUpdateStatus(pod)
	tt.WaitUnordered("add/namespace/name1")
	assert.Equal(t, Collection.Get(), ptr.Of(""))

	// when we update it to what we Fetch with, we will see it
	pod.Status.PodIP = "2.2.2.2"
	pc.UpdateStatus(pod)
	tt.WaitUnordered("update/namespace/name1")
	assert.EventuallyEqual(t, Collection.Get, ptr.Of("namespace/name1"))

	// adding pod 2 with the same IP gives us both
	pc.CreateOrUpdateStatus(pod2)
	tt.WaitUnordered("add/namespace/name2")
	assert.EventuallyEqual(t, Collection.Get, ptr.Of("namespace/name1,namespace/name2"))

	// add pod 3 to make sure our second fetch works
	pc.CreateOrUpdateStatus(pod3)
	tt.WaitUnordered("add/namespace/name3")
	assert.EventuallyEqual(t, Collection.Get, ptr.Of("namespace/name1,namespace/name2,namespace/name3"))

	// make sure the separate index fetch works
	pod4.GetLabels()["marker"] = "true"
	pc.CreateOrUpdateStatus(pod4)
	tt.WaitUnordered("add/namespace/name4")
	assert.EventuallyEqual(t, Collection.Get, ptr.Of("namespace/name1,namespace/name2,namespace/name3,namespace/name4"))

	// delete everything
	for _, pod := range pods {
		pc.Delete(pod.Name, pod.Namespace)
	}
	tt.WaitUnordered(
		"delete/namespace/name1",
		"delete/namespace/name2",
		"delete/namespace/name3",
		"delete/namespace/name4",
	)
	assert.Equal(t, fetchSorted("1.1.1.1"), []SimplePod{})
}

type PodCount struct {
	IP    string
	Count int
}

func (p PodCount) ResourceName() string {
	return p.IP
}

func TestIndexAsCollection(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	pc := clienttest.Wrap(t, kpc)
	pods := krt.WrapClient[*corev1.Pod](kpc, opts.WithName("Pods")...)
	c.RunAndWait(stop)
	SimplePods := SimplePodCollection(pods, opts)
	tt := assert.NewTracker[string](t)
	IPIndex := krt.NewIndex[string, SimplePod](SimplePods, "ip", func(o SimplePod) []string {
		return []string{o.IP}
	})

	Collection := krt.NewCollection(IPIndex.AsCollection(), func(ctx krt.HandlerContext, i krt.IndexObject[string, SimplePod]) *PodCount {
		return &PodCount{
			IP:    i.Key,
			Count: len(i.Objects),
		}
	}, opts.WithName("Collection")...)
	Collection.WaitUntilSynced(stop)

	SimplePods.Register(TrackerHandler[SimplePod](tt))

	assertion := func(ip string, want int) {
		var wo *PodCount
		if want > 0 {
			wo = &PodCount{IP: ip, Count: want}
		}
		assert.EventuallyEqual(t, func() *PodCount {
			return Collection.GetKey(ip)
		}, wo)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.4"},
	}
	pc.CreateOrUpdateStatus(pod)
	tt.WaitUnordered("add/namespace/name")
	assertion("1.2.3.4", 1)

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name2",
			Namespace: "namespace",
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.5"},
	}
	pc.CreateOrUpdateStatus(pod2)
	tt.WaitUnordered("add/namespace/name2")
	assertion("1.2.3.5", 1)

	pod.Status.PodIP = "1.2.3.5"
	pc.UpdateStatus(pod)
	tt.WaitUnordered("update/namespace/name")
	assertion("1.2.3.4", 0)
	assertion("1.2.3.5", 2)
}

type PodCounts struct {
	ByIP   int
	ByName int
}

func (p PodCounts) ResourceName() string {
	return "singleton"
}

func TestReverseIndex(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	pc := clienttest.Wrap(t, kpc)
	pods := krt.WrapClient[*corev1.Pod](kpc, opts.WithName("Pods")...)
	c.RunAndWait(stop)
	SimplePods := SimplePodCollection(pods, opts)
	tt := assert.NewTracker[string](t)
	IPIndex := krt.NewIndex[string, SimplePod](SimplePods, "ip", func(o SimplePod) []string {
		return []string{o.IP}
	})
	Collection := krt.NewSingleton(func(ctx krt.HandlerContext) *PodCounts {
		idxPods := krt.Fetch(ctx, SimplePods, krt.FilterIndex(IPIndex, "1.2.3.5"))
		namePods := krt.Fetch(ctx, SimplePods, krt.FilterKeys("namespace/name", "namespace/name3"))
		return &PodCounts{
			ByIP:   len(idxPods),
			ByName: len(namePods),
		}
	}, opts.WithName("Collection")...)
	Collection.AsCollection().WaitUntilSynced(stop)

	SimplePods.Register(TrackerHandler[SimplePod](tt))

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.4"},
	}
	pc.CreateOrUpdateStatus(pod)
	assert.EventuallyEqual(t, Collection.Get, &PodCounts{ByIP: 0, ByName: 1})

	pod.Status.PodIP = "1.2.3.5"
	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, Collection.Get, &PodCounts{ByIP: 1, ByName: 1})

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name2",
			Namespace: "namespace",
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.5"},
	}
	pc.CreateOrUpdateStatus(pod2)
	assert.EventuallyEqual(t, Collection.Get, &PodCounts{ByIP: 2, ByName: 1})

	pod3 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name3",
			Namespace: "namespace",
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.7"},
	}
	pc.CreateOrUpdateStatus(pod3)
	assert.EventuallyEqual(t, Collection.Get, &PodCounts{ByIP: 2, ByName: 2})

	pc.Delete(pod.Name, pod.Namespace)
	pc.Delete(pod2.Name, pod2.Namespace)
	assert.EventuallyEqual(t, Collection.Get, &PodCounts{ByIP: 0, ByName: 1})
}

func TestIndexAsCollectionMetadata(t *testing.T) {
	opts := testOptions(t)
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	meta := krt.Metadata{
		"key1": "value1",
	}
	pods := krt.WrapClient[*corev1.Pod](kpc, opts.WithName("Pods")...)
	SimplePods := SimplePodCollection(pods, opts)
	IPIndex := krt.NewIndex[string, SimplePod](SimplePods, "ips", func(o SimplePod) []string {
		return []string{o.IP}
	})
	c.RunAndWait(opts.Stop())
	assert.Equal(t, IPIndex.AsCollection(krt.WithMetadata(meta)).Metadata(), meta)
}

// TestIndexConcurrentUpdates tests concurrent updates with shared index keys (issue #58014).
func TestIndexConcurrentUpdates(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	pc := clienttest.Wrap(t, kpc)
	pods := krt.WrapClient[*corev1.Pod](kpc, opts.WithName("Pods")...)
	c.RunAndWait(stop)
	SimplePods := SimplePodCollection(pods, opts)

	// Create an index collection that groups pods by IP
	IPIndex := krt.NewIndex[string, SimplePod](SimplePods, "ip", func(o SimplePod) []string {
		return []string{o.IP}
	})
	indexCollection := IPIndex.AsCollection(krt.WithName("IndexCollection"))

	// Track IndexObject.Objects arrays to detect incomplete data
	type observedState struct {
		mu      sync.Mutex
		objects map[string]int // key -> max observed object count
		events  []string
	}
	obs := &observedState{
		objects: make(map[string]int),
	}

	indexCollection.Register(func(ev krt.Event[krt.IndexObject[string, SimplePod]]) {
		obs.mu.Lock()
		defer obs.mu.Unlock()
		if ev.New != nil {
			count := len(ev.New.Objects)
			if prev, exists := obs.objects[ev.New.Key]; !exists || count > prev {
				obs.objects[ev.New.Key] = count
			}
			obs.events = append(obs.events, fmt.Sprintf("add:%s:%d", ev.New.Key, count))
		} else if ev.Old != nil {
			obs.events = append(obs.events, fmt.Sprintf("delete:%s", ev.Old.Key))
		}
	})

	// Create pods sharing the same IP to simulate multiple HTTPRoutes per gateway+hostname.
	const sharedIP = "10.0.0.1"
	const podsPerIP = 10
	for i := 0; i < podsPerIP; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-shared-%d", i),
				Namespace: "test",
			},
			Status: corev1.PodStatus{
				PodIP: sharedIP,
			},
		}
		pc.CreateOrUpdateStatus(pod)
	}

	// Wait for all pods to be indexed together
	assert.EventuallyEqual(t, func() int {
		return len(IPIndex.Lookup(sharedIP))
	}, podsPerIP)

	// Create additional pods to simulate concurrent load.
	for i := 0; i < 20; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-load-%d", i),
				Namespace: "test",
			},
			Status: corev1.PodStatus{
				PodIP: fmt.Sprintf("10.0.1.%d", i),
			},
		}
		pc.CreateOrUpdateStatus(pod)
	}

	// Update pods concurrently to stress the system.
	for i := 0; i < podsPerIP; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-shared-%d", i),
				Namespace: "test",
			},
			Status: corev1.PodStatus{
				PodIP: fmt.Sprintf("10.0.2.%d", i), // Change to unique IPs
			},
		}
		pc.UpdateStatus(pod)

		// Interleave with load pod updates to create timing pressure
		if i < 20 {
			loadPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("pod-load-%d", i),
					Namespace: "test",
				},
				Status: corev1.PodStatus{
					PodIP: fmt.Sprintf("10.0.1.%d", i+100), // Change IP
				},
			}
			pc.UpdateStatus(loadPod)
		}
	}

	// Wait for the shared IP to have no pods
	assert.EventuallyEqual(t, func() int {
		return len(IPIndex.Lookup(sharedIP))
	}, 0)

	// Verify we never saw incomplete Objects arrays during concurrent updates.
	obs.mu.Lock()
	maxCount := obs.objects[sharedIP]
	obs.mu.Unlock()

	if maxCount != podsPerIP {
		t.Errorf("Race detected: IndexObject had max count %d, expected %d", maxCount, podsPerIP)
		t.Logf("Events observed: %v", obs.events)
	}

	// Verify all pods ended up at their new IPs
	for i := 0; i < podsPerIP; i++ {
		ip := fmt.Sprintf("10.0.2.%d", i)
		assert.EventuallyEqual(t, func() int {
			return len(IPIndex.Lookup(ip))
		}, 1)
	}
}

// TestIndexRingBufferDelay tests the specific race condition from #58014.
// When multiple objects share the same index key and updates happen rapidly,
// the old code could query parent's state at the wrong time and emit incomplete Objects.
func TestIndexRingBufferDelay(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	pc := clienttest.Wrap(t, kpc)
	pods := krt.WrapClient[*corev1.Pod](kpc, opts.WithName("Pods")...)
	c.RunAndWait(stop)
	SimplePods := SimplePodCollection(pods, opts)

	IPIndex := krt.NewIndex[string, SimplePod](SimplePods, "ip", func(o SimplePod) []string {
		return []string{o.IP}
	})
	indexColl := IPIndex.AsCollection(krt.WithName("IPIndexCollection"))

	// Track all events and verify Objects arrays are never incomplete
	type eventRecord struct {
		key     string
		objects []string
	}
	var events []eventRecord
	var eventsMu sync.Mutex

	indexColl.Register(func(ev krt.Event[krt.IndexObject[string, SimplePod]]) {
		eventsMu.Lock()
		defer eventsMu.Unlock()
		if ev.New != nil {
			podNames := slices.Map(ev.New.Objects, func(p SimplePod) string {
				return p.ResourceName()
			})
			slices.Sort(podNames)
			events = append(events, eventRecord{key: ev.New.Key, objects: podNames})
		}
	})

	indexColl.WaitUntilSynced(stop)

	// Setup: Create multiple pods sharing the same IP (simulating multiple HTTPRoutes per gateway+hostname)
	sharedIP := "10.0.0.1"
	podNames := []string{"route-http", "route-https", "route-grpc"}

	for _, name := range podNames {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test"},
			Status:     corev1.PodStatus{PodIP: sharedIP},
		}
		pc.CreateOrUpdateStatus(pod)
	}

	assert.EventuallyEqual(t, func() int { return len(IPIndex.Lookup(sharedIP)) }, len(podNames))

	// Rapid updates and deletes to create timing pressure
	// Update route-http (changes IP)
	pc.UpdateStatus(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "route-http", Namespace: "test"},
		Status:     corev1.PodStatus{PodIP: "10.0.0.2"},
	})

	// Delete route-grpc
	pc.Delete("route-grpc", "test")

	// Update route-https (changes IP)
	pc.UpdateStatus(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "route-https", Namespace: "test"},
		Status:     corev1.PodStatus{PodIP: "10.0.0.3"},
	})

	// Wait for all updates to settle
	time.Sleep(100 * time.Millisecond)

	// Verify all events had consistent Objects arrays
	// The key invariant: Objects should grow incrementally during initial sync,
	// then shrink incrementally as we move/delete pods
	eventsMu.Lock()
	defer eventsMu.Unlock()

	// Find events for sharedIP
	var sharedIPEvents []eventRecord
	for _, e := range events {
		if e.key == sharedIP {
			sharedIPEvents = append(sharedIPEvents, e)
		}
	}

	// During initial sync, Objects count should grow: 1, 2, 3
	// Then during updates, it should shrink as pods move away: 2, 1, 0
	// The new code (with internalState) will maintain this progression correctly
	// Old code might skip intermediate states or show empty Arrays prematurely

	var maxCount int
	for _, e := range sharedIPEvents {
		if len(e.objects) > maxCount {
			maxCount = len(e.objects)
		}
	}

	// We should have seen all 3 pods at some point
	if maxCount != 3 {
		t.Errorf("Never saw all 3 pods together. Max count: %d, want 3", maxCount)
		t.Logf("All sharedIP events: %+v", sharedIPEvents)
	}

	// Verify smooth progression (no jumps that would indicate querying parent at wrong time)
	// We expect counts to go up during sync (1,2,3) then down during updates (2,1,0)
	// Old buggy code might jump directly from 3 to 0 if it queries parent after all pods moved
	for i := 1; i < len(sharedIPEvents); i++ {
		prev := len(sharedIPEvents[i-1].objects)
		curr := len(sharedIPEvents[i].objects)
		diff := curr - prev

		// Count should only change by +1 or -1 at a time (smooth progression)
		// A jump like 3 -> 0 would indicate old code querying parent (all pods gone)
		if diff < -1 || diff > 1 {
			t.Errorf("Object count jumped from %d to %d (expected smooth +1/-1 progression)", prev, curr)
			t.Logf("Event %d: %+v", i, sharedIPEvents[i])
			t.Logf("Event %d: %+v", i-1, sharedIPEvents[i-1])
		}
	}
}

// TestIndexInternalStateConsistency directly tests that IndexCollection maintains
// internal state correctly, independent of timing. This validates the fix for #58014.
//
// The race condition occurs because:
// 1. Parent collection processes events and updates its internal state
// 2. Events flow through an unbounded ring buffer to IndexCollection
// 3. By the time IndexCollection processes Batch N, parent may have processed Batch N+1
// 4. OLD CODE: GetKey() queries parent's CURRENT state (at N+1), not state at event time (N)
// 5. NEW CODE: Internal state tracks events processed, so Objects reflects state at event time
//
// This test verifies that even when multiple batches are processed with parent ahead,
// the IndexCollection emits events with Objects reflecting the correct state for each batch.
func TestIndexInternalStateConsistency(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	pc := clienttest.Wrap(t, kpc)
	pods := krt.WrapClient[*corev1.Pod](kpc, opts.WithName("Pods")...)
	c.RunAndWait(stop)
	SimplePods := SimplePodCollection(pods, opts)

	IPIndex := krt.NewIndex[string, SimplePod](SimplePods, "ip", func(o SimplePod) []string {
		return []string{o.IP}
	})
	indexColl := IPIndex.AsCollection(krt.WithName("IPIndexCollection"))

	// Track ALL events emitted by IndexCollection
	type eventSnapshot struct {
		key        string
		objectKeys []string // sorted pod names
		eventType  string
	}
	var allEvents []eventSnapshot
	var eventsMu sync.Mutex

	indexColl.Register(func(ev krt.Event[krt.IndexObject[string, SimplePod]]) {
		eventsMu.Lock()
		defer eventsMu.Unlock()

		snapshot := eventSnapshot{}
		if ev.New != nil {
			snapshot.key = ev.New.Key
			snapshot.objectKeys = slices.Sort(slices.Map(ev.New.Objects, func(p SimplePod) string {
				return p.ResourceName()
			}))
			snapshot.eventType = "add"
		} else if ev.Old != nil {
			snapshot.key = ev.Old.Key
			snapshot.eventType = "delete"
		}
		allEvents = append(allEvents, snapshot)
	})

	indexColl.WaitUntilSynced(stop)

	// Create initial state: 3 pods sharing IP
	sharedIP := "10.0.0.1"
	for _, name := range []string{"pod-a", "pod-b", "pod-c"} {
		pc.CreateOrUpdateStatus(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
			Status:     corev1.PodStatus{PodIP: sharedIP},
		})
	}

	// Wait for all 3 pods to be indexed
	assert.EventuallyEqual(t, func() int { return len(IPIndex.Lookup(sharedIP)) }, 3)

	// Clear events from initial sync
	eventsMu.Lock()
	allEvents = nil
	eventsMu.Unlock()

	// Now trigger updates that could expose the race:
	// 1. Update pod-a (stays in sharedIP)
	// 2. Move pod-b to different IP
	// 3. Delete pod-c
	//
	// With the fix, each event should show the correct intermediate state.
	// Without the fix, events might show "future" state where pod-b and pod-c are already gone.

	pc.UpdateStatus(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "ns", Labels: map[string]string{"updated": "true"}},
		Status:     corev1.PodStatus{PodIP: sharedIP},
	})

	pc.UpdateStatus(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-b", Namespace: "ns"},
		Status:     corev1.PodStatus{PodIP: "10.0.0.2"}, // Move to different IP
	})

	pc.Delete("pod-c", "ns")

	// Wait for final state
	assert.EventuallyEqual(t, func() int { return len(IPIndex.Lookup(sharedIP)) }, 1)
	assert.EventuallyEqual(t, func() int { return len(IPIndex.Lookup("10.0.0.2")) }, 1)

	// Verify events were emitted correctly
	eventsMu.Lock()
	defer eventsMu.Unlock()

	// Find all events for sharedIP
	var sharedIPEvents []eventSnapshot
	for _, e := range allEvents {
		if e.key == sharedIP {
			sharedIPEvents = append(sharedIPEvents, e)
		}
	}

	// The key invariant: we should see object counts decrease smoothly (3 -> 2 -> 1)
	// If the race occurred, we might see a jump (3 -> 1) because GetKey returned future state
	if len(sharedIPEvents) > 1 {
		for i := 1; i < len(sharedIPEvents); i++ {
			prev := len(sharedIPEvents[i-1].objectKeys)
			curr := len(sharedIPEvents[i].objectKeys)
			// Allow +1/-1 changes, but not larger jumps that indicate race condition
			if curr > 0 && prev > 0 { // Ignore delete events
				diff := prev - curr
				if diff > 1 {
					t.Errorf("Object count jumped from %d to %d (possible race: GetKey returned future state)", prev, curr)
					t.Logf("Previous event: %+v", sharedIPEvents[i-1])
					t.Logf("Current event: %+v", sharedIPEvents[i])
				}
			}
		}
	}

	// Verify final state
	result := indexColl.GetKey(sharedIP)
	if result == nil || len(result.Objects) != 1 {
		t.Errorf("Expected 1 object for sharedIP, got %v", result)
	}
}

// TestIndexMergeConsistency validates internal state consistency during event processing.
func TestIndexMergeConsistency(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	pc := clienttest.Wrap(t, kpc)
	pods := krt.WrapClient[*corev1.Pod](kpc, opts.WithName("Pods")...)
	c.RunAndWait(stop)
	SimplePods := SimplePodCollection(pods, opts)

	// Create an index by IP
	IPIndex := krt.NewIndex[string, SimplePod](SimplePods, "ip", func(o SimplePod) []string {
		return []string{o.IP}
	})

	// Track merged state directly from the index collection
	// This mimics the HTTPRoute merging pattern
	type MergedState struct {
		IP       string
		PodCount int
		PodNames []string
	}
	mergedResults := make(map[string]MergedState)
	var mu sync.Mutex

	indexColl := IPIndex.AsCollection(krt.WithName("IPIndexCollection"))
	indexColl.Register(func(ev krt.Event[krt.IndexObject[string, SimplePod]]) {
		mu.Lock()
		defer mu.Unlock()

		if ev.New != nil && len(ev.New.Objects) > 0 {
			podNames := slices.Map(ev.New.Objects, func(p SimplePod) string {
				return p.ResourceName()
			})
			slices.Sort(podNames)

			mergedResults[ev.New.Key] = MergedState{
				IP:       ev.New.Key,
				PodCount: len(ev.New.Objects),
				PodNames: podNames,
			}
		} else if ev.Old != nil {
			delete(mergedResults, ev.Old.Key)
		}
	})

	indexColl.WaitUntilSynced(stop)

	// Create multiple pods sharing the same IP (simulating multiple HTTPRoutes for same gateway+hostname)
	const sharedIP = "192.168.1.1"
	podNames := []string{"pod-a", "pod-b", "pod-c", "pod-d", "pod-e"}

	for _, name := range podNames {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "test",
			},
			Status: corev1.PodStatus{
				PodIP: sharedIP,
			},
		}
		pc.CreateOrUpdateStatus(pod)
	}

	// Wait for all pods to be indexed and merged
	assert.EventuallyEqual(t, func() int {
		mu.Lock()
		defer mu.Unlock()
		if m, ok := mergedResults[sharedIP]; ok {
			return m.PodCount
		}
		return 0
	}, len(podNames))

	// Verify the merged result contains ALL pods
	var merged MergedState
	var hasMerged bool
	mu.Lock()
	merged, hasMerged = mergedResults[sharedIP]
	mu.Unlock()

	if !hasMerged {
		t.Fatal("No merged result found for shared IP")
	}

	assert.Equal(t, merged.PodCount, len(podNames), "Merged pod count should match")
	assert.Equal(t, len(merged.PodNames), len(podNames), "Merged pod names length should match")

	expectedNames := slices.Sort(slices.Map(podNames, func(name string) string {
		return "test/" + name
	}))
	assert.Equal(t, merged.PodNames, expectedNames, "Merged pod names should include all pods")

	// Now update pods to remove them from the shared IP one by one
	// This simulates removing hostnames from HTTPRoute spec.hostnames
	for i, name := range podNames[:3] { // Remove first 3 pods
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "test",
			},
			Status: corev1.PodStatus{
				PodIP: fmt.Sprintf("192.168.2.%d", i), // Move to different IP
			},
		}
		pc.UpdateStatus(pod)
	}

	// The merged result should now have exactly 2 pods (pod-d and pod-e)
	assert.EventuallyEqual(t, func() int {
		mu.Lock()
		defer mu.Unlock()
		if m, ok := mergedResults[sharedIP]; ok {
			return m.PodCount
		}
		return 0
	}, 2)

	var finalMerged MergedState
	var hasFinal bool
	mu.Lock()
	finalMerged, hasFinal = mergedResults[sharedIP]
	mu.Unlock()

	if !hasFinal {
		t.Fatal("No merged result found for shared IP after updates")
	}

	// Verify the remaining pods are correct.
	expectedRemaining := []string{"test/pod-d", "test/pod-e"}
	assert.Equal(t, finalMerged.PodNames, expectedRemaining)
	assert.Equal(t, finalMerged.PodCount, 2)
}
