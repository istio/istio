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

package kclient

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
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

type SaNode struct {
	ServiceAccount types.NamespacedName
	Node           string
}

func TestIndex(t *testing.T) {
	c := kube.NewFakeClient()
	pods := New[*corev1.Pod](c)
	c.RunAndWait(test.NewStop(t))
	index := CreateIndex[SaNode, *corev1.Pod](pods, func(pod *corev1.Pod) []SaNode {
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
	assert.EventuallyEqual(t, func() int {
		index.mu.RLock()
		defer index.mu.RUnlock()
		return len(index.objects)
	}, 0)
}

func TestIndexDelegate(t *testing.T) {
	c := kube.NewFakeClient()
	pods := New[*corev1.Pod](c)
	c.RunAndWait(test.NewStop(t))
	var index *Index[string, *corev1.Pod]
	adds := atomic.NewInt32(0)
	index = CreateIndexWithDelegate[string, *corev1.Pod](pods, func(pod *corev1.Pod) []string {
		return []string{pod.Spec.ServiceAccountName}
	}, controllers.EventHandler[*corev1.Pod]{
		AddFunc: func(obj *corev1.Pod) {
			// Assert that our handler sees the incoming update (and doesn't run before)
			sa := obj.Spec.ServiceAccountName
			got := index.Lookup(sa)
			for _, p := range got {
				if p.Name == obj.Name {
					adds.Inc()
					return
				}
			}
			t.Fatalf("pod %v/%v not found in index, have %v", obj.Name, sa, got)
		},
	})
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

	c.Kube().CoreV1().Pods("ns").Create(context.Background(), pod1, metav1.CreateOptions{})
	assert.EventuallyEqual(t, adds.Load, 1)

	c.Kube().CoreV1().Pods("ns").Create(context.Background(), pod2, metav1.CreateOptions{})
	assert.EventuallyEqual(t, adds.Load, 2)
}
