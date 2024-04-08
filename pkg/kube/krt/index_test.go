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
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	pc := clienttest.Wrap(t, kpc)
	pods := krt.WrapClient[*corev1.Pod](kpc)
	stop := test.NewStop(t)
	c.RunAndWait(stop)
	SimplePods := SimplePodCollection(pods)
	tt := assert.NewTracker[string](t)
	IPIndex := krt.NewIndex[SimplePod, string](SimplePods, func(o SimplePod) []string {
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
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	pc := clienttest.Wrap(t, kpc)
	pods := krt.WrapClient[*corev1.Pod](kpc)
	stop := test.NewStop(t)
	c.RunAndWait(stop)
	SimplePods := SimplePodCollection(pods)
	tt := assert.NewTracker[string](t)
	IPIndex := krt.NewIndex[SimplePod, string](SimplePods, func(o SimplePod) []string {
		return []string{o.IP}
	})
	Collection := krt.NewSingleton[string](func(ctx krt.HandlerContext) *string {
		pods := krt.Fetch(ctx, SimplePods, krt.FilterIndex(IPIndex, "1.2.3.5"))
		names := slices.Sort(slices.Map(pods, SimplePod.ResourceName))
		return ptr.Of(strings.Join(names, ","))
	})
	Collection.AsCollection().Synced().WaitUntilSynced(stop)
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
	assert.Equal(t, Collection.Get(), ptr.Of(""))

	pod.Status.PodIP = "1.2.3.5"
	pc.UpdateStatus(pod)
	tt.WaitUnordered("update/namespace/name")
	assert.Equal(t, Collection.Get(), ptr.Of("namespace/name"))

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name2",
			Namespace: "namespace",
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.5"},
	}
	pc.CreateOrUpdateStatus(pod2)
	tt.WaitUnordered("add/namespace/name2")
	assert.Equal(t, Collection.Get(), ptr.Of("namespace/name,namespace/name2"))

	pc.Delete(pod.Name, pod.Namespace)
	pc.Delete(pod2.Name, pod2.Namespace)
	tt.WaitUnordered("delete/namespace/name", "delete/namespace/name2")
	assert.Equal(t, fetchSorted("1.2.3.4"), []SimplePod{})
}
