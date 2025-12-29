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

package clienttest

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"

	clientnetworking "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
)

func TestDirectClient(t *testing.T) {
	kc := kube.NewFakeClient()
	t.Run("core", func(t *testing.T) {
		dc := NewDirectClient[*corev1.Pod, corev1.Pod, *corev1.PodList](t, kc)
		dc.Create(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "x"}})
		assert.Equal(t, len(dc.List("", klabels.Everything())), 1)
	})
	t.Run("istio", func(t *testing.T) {
		dc := NewDirectClient[*clientnetworking.ServiceEntry, *clientnetworking.ServiceEntry, *clientnetworking.ServiceEntryList](t, kc)
		dc.Create(&clientnetworking.ServiceEntry{ObjectMeta: metav1.ObjectMeta{Name: "x"}})
		assert.Equal(t, len(dc.List("", klabels.Everything())), 1)
	})
}
