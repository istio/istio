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

package status

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/testing/mock"
	"istio.io/istio/pkg/config/resource"
)

const subfield = "testMessages"

func TestBasicStartStop(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController(subfield)
	k, cl := setupClient()

	c.Start(rt.NewProvider(k, metav1.NamespaceAll, 0), basicmeta.MustGet().KubeCollections().All())
	defer c.Stop()

	c.Report(diag.Messages{})
	g.Consistently(cl.Actions).Should(BeEmpty())
}

func TestDoubleStart(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController(subfield)
	k, cl := setupClient()

	c.Start(rt.NewProvider(k, metav1.NamespaceAll, 0), basicmeta.MustGet().KubeCollections().All())
	c.Start(rt.NewProvider(k, metav1.NamespaceAll, 0), basicmeta.MustGet().KubeCollections().All())
	defer c.Stop()

	c.Report(diag.Messages{})
	g.Consistently(cl.Actions).Should(BeEmpty())
}

func TestDoubleStop(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController(subfield)
	k, cl := setupClient()

	c.Start(rt.NewProvider(k, metav1.NamespaceAll, 0), basicmeta.MustGet().KubeCollections().All())
	c.Report(diag.Messages{})
	g.Consistently(cl.Actions).Should(BeEmpty())
	c.Stop()
	c.Stop()
}

func TestNoReconcilation(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController(subfield)
	k, cl := setupClient()

	c.Start(rt.NewProvider(k, metav1.NamespaceAll, 0), basicmeta.MustGet().KubeCollections().All())
	c.UpdateResourceStatus(basicmeta.K8SCollection1.Name(), resource.NewFullName("foo", "bar"), "v1", "s1")
	defer c.Stop()

	g.Consistently(cl.Actions).Should(BeEmpty())
}

func TestBasicReconcilation_BeforeUpdate(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController(subfield)

	s := map[string]interface{}{
		subfield: "s1",
	}

	r := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": s,
		},
	}

	k, cl := setupClientWithReactors(r, nil)

	c.Start(rt.NewProvider(k, metav1.NamespaceAll, 0), basicmeta.MustGet().KubeCollections().All())
	c.UpdateResourceStatus(basicmeta.K8SCollection1.Name(), resource.NewFullName("foo", "bar"), "v1", s)
	c.Report(diag.Messages{})
	defer c.Stop()

	g.Eventually(cl.Actions).Should(HaveLen(2))
	g.Expect(cl.Actions()[1]).To(BeAssignableToTypeOf(k8stesting.UpdateActionImpl{}))
	u := cl.Actions()[1].(k8stesting.UpdateActionImpl).Object.(*unstructured.Unstructured)
	g.Expect(u.Object["status"]).To(BeNil())
}

func TestBasicReconcilation_AfterUpdate(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController(subfield)

	s := map[string]interface{}{
		subfield: "s1",
	}

	r := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": s,
		},
	}

	k, cl := setupClientWithReactors(r, nil)

	c.Start(rt.NewProvider(k, metav1.NamespaceAll, 0), basicmeta.MustGet().KubeCollections().All())
	c.Report(diag.Messages{})
	c.UpdateResourceStatus(
		basicmeta.K8SCollection1.Name(), resource.NewFullName("foo", "bar"), "v1", s)
	defer c.Stop()

	g.Eventually(cl.Actions).Should(HaveLen(2))
	g.Expect(cl.Actions()[1]).To(BeAssignableToTypeOf(k8stesting.UpdateActionImpl{}))
	u := cl.Actions()[1].(k8stesting.UpdateActionImpl).Object.(*unstructured.Unstructured)
	g.Expect(u.Object["status"]).To(BeNil())
}

func TestBasicReconcilation_AfterUpdate_Othersubfield(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController(subfield)

	otherSubfield := "otherMessages"
	s := map[string]interface{}{
		subfield:      "s1",
		otherSubfield: "s2",
	}

	r := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": s,
		},
	}

	k, cl := setupClientWithReactors(r, nil)

	c.Start(rt.NewProvider(k, metav1.NamespaceAll, 0), basicmeta.MustGet().KubeCollections().All())
	c.Report(diag.Messages{})
	c.UpdateResourceStatus(
		basicmeta.K8SCollection1.Name(), resource.NewFullName("foo", "bar"), "v1", s)
	defer c.Stop()

	g.Eventually(cl.Actions).Should(HaveLen(2))
	g.Expect(cl.Actions()[1]).To(BeAssignableToTypeOf(k8stesting.UpdateActionImpl{}))
	u := cl.Actions()[1].(k8stesting.UpdateActionImpl).Object.(*unstructured.Unstructured)

	g.Expect(u.Object["status"]).To(Not(BeNil()))
	actualStatusMap := u.Object["status"].(map[string]interface{})
	g.Expect(actualStatusMap).To(Not(HaveKey(subfield)))
	g.Expect(actualStatusMap).To(HaveKeyWithValue(otherSubfield, "s2"))
}

func TestBasicReconcilation_NewStatus(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController(subfield)

	r := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":            "foo",
				"namespace":       "bar",
				"resourceVersion": "v1",
			},
		},
	}

	k, cl := setupClientWithReactors(r, nil)

	e := resource.Instance{
		Origin: &rt.Origin{
			Collection: basicmeta.K8SCollection1.Name(),
			FullName:   resource.NewFullName("foo", "bar"),
			Version:    resource.Version("v1"),
		},
	}

	c.Start(rt.NewProvider(k, metav1.NamespaceAll, 0), basicmeta.MustGet().KubeCollections().All())
	m := msg.NewInternalError(&e, "foo")
	c.Report(diag.Messages{m})
	defer c.Stop()

	g.Eventually(cl.Actions).Should(HaveLen(2))
	g.Expect(cl.Actions()[1]).To(BeAssignableToTypeOf(k8stesting.UpdateActionImpl{}))
	u := cl.Actions()[1].(k8stesting.UpdateActionImpl).Object.(*unstructured.Unstructured)

	actualStatusMap := u.Object["status"].(map[string]interface{})

	g.Expect(actualStatusMap[subfield]).To(ConsistOf(expectedMessage(m).Unstructured(false)))
}

func TestBasicReconcilation_NewStatusOldNonMap(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController(subfield)

	r := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":            "foo",
				"namespace":       "bar",
				"resourceVersion": "v1",
			},
			"status": "s1", // Should be overwritten without breaking
		},
	}

	k, cl := setupClientWithReactors(r, nil)

	e := resource.Instance{
		Origin: &rt.Origin{
			Collection: basicmeta.K8SCollection1.Name(),
			FullName:   resource.NewFullName("foo", "bar"),
			Version:    resource.Version("v1"),
		},
	}

	c.Start(rt.NewProvider(k, metav1.NamespaceAll, 0), basicmeta.MustGet().KubeCollections().All())
	m := msg.NewInternalError(&e, "foo")
	c.Report(diag.Messages{m})
	defer c.Stop()

	g.Eventually(cl.Actions).Should(HaveLen(2))
	g.Expect(cl.Actions()[1]).To(BeAssignableToTypeOf(k8stesting.UpdateActionImpl{}))
	u := cl.Actions()[1].(k8stesting.UpdateActionImpl).Object.(*unstructured.Unstructured)

	actualStatusMap := u.Object["status"].(map[string]interface{})
	g.Expect(actualStatusMap[subfield]).To(ConsistOf(expectedMessage(m).Unstructured(false)))
}

func TestBasicReconcilation_UpdateError(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController(subfield)

	r := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"resourceVersion": "v1",
			},
		},
	}

	k, cl := setupClientWithReactors(r, fmt.Errorf("cheese not found"))

	e := resource.Instance{
		Origin: &rt.Origin{
			Collection: basicmeta.K8SCollection1.Name(),
			FullName:   resource.NewFullName("foo", "bar"),
			Version:    resource.Version("v1"),
		},
	}

	c.Start(rt.NewProvider(k, metav1.NamespaceAll, 0), basicmeta.MustGet().KubeCollections().All())
	m := msg.NewInternalError(&e, "foo")
	c.Report(diag.Messages{m})
	defer c.Stop()

	g.Eventually(cl.Actions).Should(HaveLen(2))
	g.Expect(cl.Actions()[1]).To(BeAssignableToTypeOf(k8stesting.UpdateActionImpl{}))
	u := cl.Actions()[1].(k8stesting.UpdateActionImpl).Object.(*unstructured.Unstructured)

	actualStatusMap := u.Object["status"].(map[string]interface{})
	g.Expect(actualStatusMap[subfield]).To(ConsistOf(expectedMessage(m).Unstructured(false)))
}

func TestBasicReconcilation_GetError(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController(subfield)

	k, cl := setupClientWithReactors(nil, nil)

	cl.ReactionChain = nil
	cl.AddReactor("get", "Kind1s", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		handled = true
		err = fmt.Errorf("cheese not found")
		return
	})

	e := resource.Instance{
		Origin: &rt.Origin{
			Collection: basicmeta.K8SCollection1.Name(),
			FullName:   resource.NewFullName("foo", "bar"),
			Version:    resource.Version("v1"),
		},
	}

	c.Start(rt.NewProvider(k, metav1.NamespaceAll, 0), basicmeta.MustGet().KubeCollections().All())
	m := msg.NewInternalError(&e, "foo")
	c.Report(diag.Messages{m})
	defer c.Stop()

	g.Eventually(cl.Actions).Should(HaveLen(1))
	g.Consistently(cl.Actions).Should(HaveLen(1))
}

func TestBasicReconcilation_VersionMismatch(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController(subfield)

	r := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"resourceVersion": "v2",
			},
		},
	}

	k, cl := setupClientWithReactors(r, nil)

	e := resource.Instance{
		Origin: &rt.Origin{
			Collection: basicmeta.K8SCollection1.Name(),
			FullName:   resource.NewFullName("foo", "bar"),
			Version:    resource.Version("v1"), // message for an older version
		},
	}

	c.Start(rt.NewProvider(k, metav1.NamespaceAll, 0), basicmeta.MustGet().KubeCollections().All())
	m := msg.NewInternalError(&e, "foo")
	c.Report(diag.Messages{m})
	defer c.Stop()

	g.Eventually(cl.Actions).Should(HaveLen(1))
	g.Consistently(cl.Actions).Should(HaveLen(1))
}

func setupClient() (*mock.Kube, *fake.FakeDynamicClient) {
	k := mock.NewKube()

	cl := fake.NewSimpleDynamicClient(runtime.NewScheme())
	k.AddResponse(cl, nil)

	return k, cl
}

func setupClientWithReactors(retVal runtime.Object, updateErrVal error) (*mock.Kube, *fake.FakeDynamicClient) {
	k, cl := setupClient()

	cl.ReactionChain = nil
	cl.AddReactor("get", "Kind1s", func(action k8stesting.Action) (
		handled bool, ret runtime.Object, err error) {
		handled = true
		ret = retVal
		return
	})

	cl.AddReactor("update", "Kind1s", func(action k8stesting.Action) (
		handled bool, ret runtime.Object, err error) {
		handled = true
		err = updateErrVal
		return
	})

	return k, cl
}

func expectedMessage(m diag.Message) *diag.Message {
	return &diag.Message{
		Type:       m.Type,
		Parameters: m.Parameters,
		Resource:   m.Resource,
		DocRef:     DocRef,
	}
}
