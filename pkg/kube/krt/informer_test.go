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
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	"istio.io/istio/pkg/config/schema/kubeclient"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestNewInformer(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()
	ConfigMaps := krt.NewInformer[*corev1.ConfigMap](c, opts.WithName("ConfigMaps")...)
	c.RunAndWait(stop)
	cmt := clienttest.NewWriter[*corev1.ConfigMap](t, c)
	tt := assert.NewTracker[string](t)
	ConfigMaps.Register(TrackerHandler[*corev1.ConfigMap](tt))

	assert.Equal(t, ConfigMaps.List(), nil)

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
	assert.Equal(t, ConfigMaps.List(), []*corev1.ConfigMap{cmA})

	cmt.Update(cmA2)
	tt.WaitOrdered("update/ns/a")
	assert.Equal(t, ConfigMaps.List(), []*corev1.ConfigMap{cmA2})

	cmt.Create(cmB)
	tt.WaitOrdered("add/ns/b")
	assert.Equal(t, slices.SortBy(ConfigMaps.List(), func(a *corev1.ConfigMap) string { return a.Name }), []*corev1.ConfigMap{cmA2, cmB})

	assert.Equal(t, ConfigMaps.GetKey("ns/b"), &cmB)
	assert.Equal(t, ConfigMaps.GetKey("ns/a"), &cmA2)

	tt2 := assert.NewTracker[string](t)
	ConfigMaps.Register(TrackerHandler[*corev1.ConfigMap](tt2))
	tt2.WaitUnordered("add/ns/a", "add/ns/b")

	cmt.Delete(cmB.Name, cmB.Namespace)
	tt.WaitOrdered("delete/ns/b")
}

func TestUnregisteredTypeCollection(t *testing.T) {
	opts := testOptions(t)
	np := &v1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "netpol",
			Namespace: "default",
		},
	}
	c := kube.NewFakeClient(np)

	kubeclient.Register[*v1.NetworkPolicy](
		v1.SchemeGroupVersion.WithResource("networkpolicies"),
		v1.SchemeGroupVersion.WithKind("NetworkPolicy"),
		func(c kubeclient.ClientGetter, namespace string, o metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().NetworkingV1().NetworkPolicies(namespace).List(context.Background(), o)
		},
		func(c kubeclient.ClientGetter, namespace string, o metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().NetworkingV1().NetworkPolicies(namespace).Watch(context.Background(), o)
		},
	)
	npcoll := krt.NewInformer[*v1.NetworkPolicy](c, opts.WithName("NetworkPolicies")...)
	c.RunAndWait(test.NewStop(t))
	assert.Equal(t, npcoll.List(), []*v1.NetworkPolicy{np})
}

func TestInformerCollectionMetadata(t *testing.T) {
	opts := testOptions(t)
	c := kube.NewFakeClient()
	meta := krt.Metadata{
		"key1": "value1",
	}
	ConfigMaps := krt.NewInformer[*corev1.ConfigMap](c, opts.With(
		krt.WithName("ConfigMaps"),
		krt.WithMetadata(meta),
	)...)
	c.RunAndWait(opts.Stop())

	assert.Equal(t, ConfigMaps.Metadata(), meta)
}
