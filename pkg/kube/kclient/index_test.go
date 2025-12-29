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

package kclient_test

import (
	"context"
	"testing"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

type SaNode struct {
	ServiceAccount types.NamespacedName
	Node           string
}

func (s SaNode) String() string {
	return s.Node + "/" + s.ServiceAccount.String()
}

func TestIndex(t *testing.T) {
	c := kube.NewFakeClient()
	pods := kclient.New[*corev1.Pod](c)
	c.RunAndWait(test.NewStop(t))
	index := kclient.CreateIndex[SaNode, *corev1.Pod](pods, "saNode", func(pod *corev1.Pod) []SaNode {
		if len(pod.Spec.NodeName) == 0 {
			return nil
		}
		if len(pod.Spec.ServiceAccountName) == 0 {
			return nil
		}
		return []SaNode{{
			ServiceAccount: types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      pod.Spec.ServiceAccountName,
			},
			Node: pod.Spec.NodeName,
		}}
	})
	k1 := SaNode{
		ServiceAccount: types.NamespacedName{
			Namespace: "ns",
			Name:      "sa",
		},
		Node: "node",
	}
	k2 := SaNode{
		ServiceAccount: types.NamespacedName{
			Namespace: "ns",
			Name:      "sa2",
		},
		Node: "node",
	}
	assert.Equal(t, index.Lookup(k1), nil)
	assert.Equal(t, index.Lookup(k2), nil)
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "ns",
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "sa",
			NodeName:           "node",
		},
	}
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod2",
			Namespace: "ns",
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "sa2",
			NodeName:           "node",
		},
	}
	pod3 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod3",
			Namespace: "ns",
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "sa",
			NodeName:           "node",
		},
	}

	assertIndex := func(k SaNode, pods ...*corev1.Pod) {
		t.Helper()
		assert.EventuallyEqual(t, func() []*corev1.Pod { return index.Lookup(k) }, pods, retry.Timeout(time.Second*5))
	}

	// When we create a pod, we should (eventually) see it in the index
	c.Kube().CoreV1().Pods("ns").Create(context.Background(), pod1, metav1.CreateOptions{})
	assertIndex(k1, pod1)
	assertIndex(k2)

	// Create another pod; we ought to find it as well now.
	c.Kube().CoreV1().Pods("ns").Create(context.Background(), pod2, metav1.CreateOptions{})
	assertIndex(k1, pod1) // Original one must still persist
	assertIndex(k2, pod2) // New one should be there, eventually

	// Create another pod with the same SA; we ought to find multiple now.
	c.Kube().CoreV1().Pods("ns").Create(context.Background(), pod3, metav1.CreateOptions{})
	assertIndex(k1, pod1, pod3) // Original one must still persist
	assertIndex(k2, pod2)       // New one should be there, eventually

	pod1Alt := pod1.DeepCopy()
	// This can't happen in practice with Pod, but Index supports arbitrary types
	pod1Alt.Spec.ServiceAccountName = "new-sa"

	keyNew := SaNode{
		ServiceAccount: types.NamespacedName{
			Namespace: "ns",
			Name:      "new-sa",
		},
		Node: "node",
	}
	c.Kube().CoreV1().Pods("ns").Update(context.Background(), pod1Alt, metav1.UpdateOptions{})
	assertIndex(k1, pod3)        // Pod should be dropped from the index
	assertIndex(keyNew, pod1Alt) // And added under the new key

	c.Kube().CoreV1().Pods("ns").Delete(context.Background(), pod1Alt.Name, metav1.DeleteOptions{})
	assertIndex(k1, pod3) // Shouldn't impact others
	assertIndex(keyNew)   // but should be removed

	// Should fully cleanup the index on deletes
	c.Kube().CoreV1().Pods("ns").Delete(context.Background(), pod2.Name, metav1.DeleteOptions{})
	c.Kube().CoreV1().Pods("ns").Delete(context.Background(), pod3.Name, metav1.DeleteOptions{})
	assertIndex(keyNew)
	assertIndex(k1)
	assertIndex(k2)
}

func TestIndexFilters(t *testing.T) {
	c := kube.NewFakeClient()

	currentAllowedNamespace := atomic.NewString("a")
	filter := kubetypes.NewStaticObjectFilter(func(obj any) bool {
		return controllers.ExtractObject(obj).GetNamespace() == currentAllowedNamespace.Load()
	})
	pods := kclient.NewFiltered[*corev1.Pod](c, kubetypes.Filter{
		ObjectFilter: filter,
	})
	pc := clienttest.NewWriter[*corev1.Pod](t, c)
	c.RunAndWait(test.NewStop(t))
	index := kclient.CreateStringIndex[*corev1.Pod](pods, "podIp", func(pod *corev1.Pod) []string {
		if pod.Status.PodIP == "" {
			return nil
		}
		return []string{pod.Status.PodIP}
	})

	// Initial state should be empty
	assert.Equal(t, index.Lookup("1.1.1.1"), nil)

	assertIndex := func(k string, pods ...*corev1.Pod) {
		t.Helper()
		assert.EventuallyEqual(t, func() []*corev1.Pod { return index.Lookup(k) }, pods, retry.Timeout(time.Second*5))
	}

	// Add a pod matching the filter, we should see it.
	podA1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "a",
		},
		Status: corev1.PodStatus{PodIP: "1.1.1.1"},
	}
	pc.CreateOrUpdateStatus(podA1)
	assertIndex("1.1.1.1", podA1)

	podA2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod2",
			Namespace: "a",
		},
		Status: corev1.PodStatus{PodIP: "2.2.2.2"},
	}
	pc.CreateOrUpdateStatus(podA2)
	assertIndex("1.1.1.1", podA1)
	assertIndex("2.2.2.2", podA2)

	// Create a pod not matching the filter with overlapping IP
	podB1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "b",
		},
		Status: corev1.PodStatus{PodIP: "1.1.1.1"},
	}
	pc.CreateOrUpdateStatus(podB1)
	assertIndex("1.1.1.1", podA1)
	assertIndex("2.2.2.2", podA2)

	// Another pod, not matching filter with distinct IP
	podB2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod2",
			Namespace: "b",
		},
		Status: corev1.PodStatus{PodIP: "3.3.3.3"},
	}
	pc.CreateOrUpdateStatus(podB2)
	assertIndex("1.1.1.1", podA1)
	assertIndex("2.2.2.2", podA2)
	assertIndex("3.3.3.3")

	// Switch the filter
	currentAllowedNamespace.Store("b")
	assertIndex("1.1.1.1", podB1)
	assertIndex("2.2.2.2")
	assertIndex("3.3.3.3", podB2)

	// Switch the filter again
	currentAllowedNamespace.Store("c")
	assertIndex("1.1.1.1")
	assertIndex("2.2.2.2")
	assertIndex("3.3.3.3")
}
