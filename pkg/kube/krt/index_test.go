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
	Collection := krt.NewSingleton[string](func(ctx krt.HandlerContext) *string {
		a := krt.Fetch(ctx, SimplePods, krt.FilterIndex(IPIndex, "2.2.2.2"))
		b := krt.Fetch(ctx, SimplePods, krt.FilterIndex(IPIndex, "3.3.3.3"))
		pods := append(a, b...)
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
	for i := range 3 {
		pods = append(pods, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "name" + strconv.Itoa(i+1),
				Namespace: "namespace",
			},
			Status: corev1.PodStatus{PodIP: fmt.Sprintf("%d.%d.%d.%d", i+1, i+1, i+1, i+1)},
		})

		println("make ", pods[len(pods)-1].Name)
	}
	pod := pods[0]
	pod2 := pods[1]
	pod3 := pods[2]

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

	// delete everything
	pc.Delete(pod.Name, pod.Namespace)
	pc.Delete(pod2.Name, pod2.Namespace)
	pc.Delete(pod3.Name, pod3.Namespace)
	tt.WaitUnordered("delete/namespace/name1", "delete/namespace/name2", "delete/namespace/name3")
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
