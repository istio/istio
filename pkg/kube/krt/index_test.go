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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestLookupFiltered(t *testing.T) {
	opts := testOptions(t)
	objects := []Named{{Namespace: "ns", Name: "a"}, {Namespace: "ns", Name: "b"}, {Namespace: "other", Name: "c"}}
	static := krt.NewStaticCollection[Named](nil, objects, opts.WithName("static")...)
	left := krt.NewStaticCollection[Named](nil, objects[:1], opts.WithName("left")...)
	right := krt.NewStaticCollection[Named](nil, objects[1:], opts.WithName("right")...)
	collections := map[string]krt.Collection[Named]{
		"static": static,
		"derived": krt.NewCollection(static, func(_ krt.HandlerContext, n Named) *Named {
			return &n
		}, opts.WithName("derived")...),
		"joined": krt.JoinCollection([]krt.Collection[Named]{left, right}, opts.WithName("joined")...),
		"merged": krt.JoinWithMergeCollection([]krt.Collection[Named]{left, right}, func(ns []Named) *Named {
			return &ns[0]
		}, opts.WithName("merged")...),
	}
	for name, col := range collections {
		t.Run(name, func(t *testing.T) {
			col.WaitUntilSynced(opts.Stop())
			checkLookupFiltered(t, col, func(n Named) string { return n.Namespace })
		})
	}
	t.Run("mapped", func(t *testing.T) {
		col := krt.MapCollection(static, func(n Named) SimplePod { return SimplePod{Named: n} }, opts.WithName("mapped")...)
		col.WaitUntilSynced(opts.Stop())
		checkLookupFiltered(t, col, func(p SimplePod) string { return p.Namespace })
	})
	t.Run("informer", func(t *testing.T) {
		client := kube.NewFakeClient(
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "a"}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "b"}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "c"}},
		)
		col := krt.NewInformer[*corev1.ConfigMap](client, opts.WithName("informer")...)
		client.RunAndWait(opts.Stop())
		col.WaitUntilSynced(opts.Stop())
		checkLookupFiltered(t, col, func(n *corev1.ConfigMap) string { return n.Namespace })
	})
}

func checkLookupFiltered[T any](t *testing.T, col krt.Collection[T], namespace func(T) string) {
	t.Helper()
	idx := krt.NewIndex(col, "namespace", func(n T) []string { return []string{namespace(n)} })
	keys := func(objects []T) []string {
		return slices.Sort(slices.Map(objects, krt.GetKey[T]))
	}
	assert.Equal(t, keys(idx.LookupFiltered("ns", nil)), []string{"ns/a", "ns/b"})
	assert.Equal(t, keys(idx.LookupFiltered("ns", func(T) bool { return true })), []string{"ns/a", "ns/b"})
	assert.Equal(t, len(idx.LookupFiltered("ns", func(T) bool { return false })), 0)
	assert.Equal(t, len(idx.LookupFiltered("missing", func(T) bool {
		t.Fatal("filter called for missing key")
		return true
	})), 0)
	var calls int
	filter := func(obj T) bool {
		calls++
		assert.Equal(t, namespace(obj), "ns")
		return krt.GetKey(obj) == "ns/b"
	}
	assert.Equal(t, keys(idx.LookupFiltered("ns", filter)), []string{"ns/b"})
	assert.Equal(t, calls, 2)
	calls = 0
	assert.Equal(t, keys(krt.FetchOrList(nil, col, krt.FilterIndex(idx, "ns"), krt.FilterGeneric(func(obj any) bool {
		return filter(obj.(T))
	}))), []string{"ns/b"})
	assert.Equal(t, calls, 2)
	// Filtering must not change the index's stored results.
	assert.Equal(t, keys(idx.Lookup("ns")), []string{"ns/a", "ns/b"})
}

func BenchmarkLookupFiltered(b *testing.B) {
	scope := log.FindScope("krt")
	level := scope.GetOutputLevel()
	scope.SetOutputLevel(log.InfoLevel)
	b.Cleanup(func() { scope.SetOutputLevel(level) })
	for _, size := range []int{100, 10000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			objects := make([]Named, size)
			for i := range objects {
				objects[i] = Named{Namespace: "ns", Name: strconv.Itoa(i)}
			}
			col := krt.NewStaticCollection[Named](nil, objects, krt.WithStop(test.NewStop(b)))
			idx := krt.NewIndex(col, "namespace", func(n Named) []string { return []string{n.Namespace} })
			filter := func(n Named) bool { return n.Name == "0" }
			for name, lookup := range map[string]func() []Named{
				"Lookup":         func() []Named { return slices.FilterInPlace(idx.Lookup("ns"), filter) },
				"LookupFiltered": func() []Named { return idx.LookupFiltered("ns", filter) },
				"Fetch": func() []Named {
					return krt.FetchOrList(nil, col, krt.FilterIndex(idx, "ns"), krt.FilterGeneric(func(obj any) bool {
						return filter(obj.(Named))
					}))
				},
			} {
				b.Run(name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						if len(lookup()) != 1 {
							b.Fatal("expected one matching object")
						}
					}
				})
			}
		})
	}
}

func TestIndexFetchFilteredUpdates(t *testing.T) {
	opts := testOptions(t)
	pod := SimplePod{Named: Named{Namespace: "ns", Name: "a"}, IP: "unmatched"}
	pods := krt.NewMutableCollection[SimplePod](nil, []SimplePod{pod}, opts.WithName("pods")...)
	idx := krt.NewIndex(pods.AsCollection(), "namespace", func(p SimplePod) []string { return []string{p.Namespace} })
	result := krt.NewSingleton(func(ctx krt.HandlerContext) *string {
		matches := idx.Fetch(ctx, "ns", krt.FilterGeneric(func(obj any) bool { return obj.(SimplePod).IP == "matched" }))
		return ptr.Of(strconv.Itoa(len(matches)))
	}, opts.WithName("result")...)
	result.AsCollection().WaitUntilSynced(opts.Stop())
	assert.Equal(t, result.Get(), ptr.Of("0"))
	pod.IP = "matched"
	pods.UpdateObject(pod)
	assert.EventuallyEqual(t, result.Get, ptr.Of("1"))
	pod.IP = "unmatched"
	pods.UpdateObject(pod)
	assert.EventuallyEqual(t, result.Get, ptr.Of("0"))
}

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
		a := IPIndex.Fetch(ctx, "2.2.2.2")
		b := IPIndex.Fetch(ctx, "3.3.3.3")
		// a third fetch on the same SimplePods but with another index
		c := LabelIndex.Fetch(ctx, "marker=true")

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
		assert.EventuallyEqual(t, func() *PodCount {
			return Collection.GetKey("dummy")
		}, nil)
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

	indexList := slices.SortBy(IPIndex.AsCollection().List(), func(i krt.IndexObject[string, SimplePod]) string {
		return i.Key
	})
	for idx := range indexList {
		indexList[idx].Objects = slices.SortBy(indexList[idx].Objects, func(p SimplePod) string {
			return p.ResourceName()
		})
	}
	assert.Equal(t, indexList, []krt.IndexObject[string, SimplePod]{
		{
			Key: "1.2.3.5",
			Objects: []SimplePod{
				{NewNamed(pod), Labeled{}, "1.2.3.5"},
				{NewNamed(pod2), Labeled{}, "1.2.3.5"},
			},
		},
	})
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
		idxPods := IPIndex.Fetch(ctx, "1.2.3.5")
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
