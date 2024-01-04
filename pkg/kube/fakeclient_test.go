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

package kube

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/test/util/assert"
)

func TestFakeApply(t *testing.T) {
	c := NewFakeClient()
	g := gomega.NewWithT(t)
	// create a cm by applying
	cma := corev1ac.ConfigMap("name", "default").
		WithData(map[string]string{
			"key": string("value"),
		})
	out, err := c.Kube().CoreV1().ConfigMaps("default").Apply(context.Background(), cma, metav1.ApplyOptions{
		FieldManager: "istio",
	})
	assert.NoError(t, err)
	outac, err := corev1ac.ExtractConfigMap(out, "istio")
	assert.NoError(t, err)
	g.Expect(outac).To(gomega.Equal(cma))
	// modify the cm by applying
	cma.Data["key2"] = "value2"
	out, err = c.Kube().CoreV1().ConfigMaps("default").Apply(context.Background(), cma, metav1.ApplyOptions{
		FieldManager: "istio",
	})
	assert.NoError(t, err)
	outac, err = corev1ac.ExtractConfigMap(out, "istio")
	assert.NoError(t, err)
	g.Expect(outac).To(gomega.Equal(cma))
}

func TestMergeDynamicToTyped(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	f := fake.NewSimpleClientset()
	i := istiofake.NewSimpleClientset()
	df := dynamicfake.NewSimpleDynamicClient(FakeIstioScheme)
	fm := fakeMerger{
		scheme: FakeIstioScheme,
	}

	fm.Merge(f)
	fm.Merge(i)
	fm.MergeDynamic(df)

	cm := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"data": map[string]interface{}{
				"foo": "bar",
			},
		},
	}

	cm.SetGroupVersionKind(gvk.ConfigMap.Kubernetes())
	cm.SetName("second")

	z := atomic.Int32{}
	inf := informers.NewSharedInformerFactory(f, 0).Core().V1().ConfigMaps().Informer()
	inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) { z.Add(1) },
	})
	stopper := make(chan struct{})
	go inf.Run(stopper)
	g.Eventually(inf.HasSynced).Should(gomega.BeTrue())

	_, err := df.Resource(gvr.ConfigMap).Namespace("default").Create(context.Background(), cm, metav1.CreateOptions{})
	assert.NoError(t, err)

	ro, err := f.Tracker().Get(gvr.ConfigMap, "default", "second")
	ucm := ro.(*corev1.ConfigMap)
	assert.NoError(t, err)
	g.Expect(ucm.Data).To(gomega.HaveKey("foo"))

	g.Eventually(z.Load).Should(gomega.Equal(int32(1)))
}

func TestNameGeneration(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := NewFakeClient()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "foo",
		},
	}
	unsObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cm)
	assert.NoError(t, err)
	ucm := &unstructured.Unstructured{Object: unsObj}
	out, err := c.Kube().CoreV1().ConfigMaps("default").Create(context.Background(), cm, metav1.CreateOptions{})
	assert.NoError(t, err)
	g.Expect(out.Name).To(gomega.HavePrefix("foo"))

	out2, err := c.Dynamic().Resource(gvr.ConfigMap).Namespace("default").Create(context.Background(), ucm, metav1.CreateOptions{})
	assert.NoError(t, err)
	g.Expect(out2.GetName()).To(gomega.HavePrefix("foo"))
}
