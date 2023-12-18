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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestNewInformer(t *testing.T) {
	c := kube.NewFakeClient()
	ConfigMaps := krt.NewInformer[*corev1.ConfigMap](c)
	c.RunAndWait(test.NewStop(t))
	cmt := clienttest.NewWriter[*corev1.ConfigMap](t, c)
	tt := assert.NewTracker[string](t)
	ConfigMaps.Register(TrackerHandler[*corev1.ConfigMap](tt))

	assert.Equal(t, ConfigMaps.List(""), nil)

	cmA := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "a",
			Namespace: "ns",
		},
	}
	cmA2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "a",
			Namespace: "ns",
		},
		Data: map[string]string{"foo": "bar"},
	}
	cmB := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "b",
			Namespace: "ns",
		},
	}
	cmt.Create(cmA)
	tt.WaitOrdered("add/ns/a")
	assert.Equal(t, ConfigMaps.List(""), []*corev1.ConfigMap{cmA})

	cmt.Update(cmA2)
	tt.WaitOrdered("update/ns/a")
	assert.Equal(t, ConfigMaps.List(""), []*corev1.ConfigMap{cmA2})

	cmt.Create(cmB)
	tt.WaitOrdered("add/ns/b")
	assert.Equal(t, slices.SortBy(ConfigMaps.List(""), func(a *corev1.ConfigMap) string { return a.Name }), []*corev1.ConfigMap{cmA2, cmB})

	assert.Equal(t, ConfigMaps.GetKey("ns/b"), &cmB)
	assert.Equal(t, ConfigMaps.GetKey("ns/a"), &cmA2)

	tt2 := assert.NewTracker[string](t)
	ConfigMaps.Register(TrackerHandler[*corev1.ConfigMap](tt2))
	tt2.WaitUnordered("add/ns/a", "add/ns/b")

	cmt.Delete(cmB.Name, cmB.Namespace)
	tt.WaitOrdered("delete/ns/b")
}
