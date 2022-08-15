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

package controllers

import (
	"testing"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

func TestEnqueueForParentHandler(t *testing.T) {
	var written atomic.String
	q := NewQueue("test", WithReconciler(func(key types.NamespacedName) error {
		t.Logf("got event %v", key)
		written.Store(key.String())
		return nil
	}))
	go q.Run(test.NewStop(t))
	handler := EnqueueForParentHandler(q, gvk.Deployment)
	handler(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "pod",
			Namespace:       "ns",
			OwnerReferences: []metav1.OwnerReference{},
		},
	})
	if got := written.Load(); got != "" {
		t.Fatalf("unexpectedly enqueued %v", got)
	}

	handler(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "ns",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: gvk.Deployment.GroupVersion(),
				Kind:       gvk.Deployment.Kind,
				Name:       "deployment",
				UID:        "1234",
			}},
		},
	})
	retry.UntilOrFail(t, func() bool {
		return written.Load() == "ns/deployment"
	}, retry.Timeout(time.Second*5))
	written.Store("")

	handler(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "ns",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: gvk.ReferencePolicy.GroupVersion(),
				Kind:       gvk.ReferencePolicy.Kind,
				Name:       "wrong-type",
				UID:        "1234",
			}},
		},
	})
	if got := written.Load(); got != "" {
		t.Fatalf("unexpectedly enqueued %v", got)
	}
}
