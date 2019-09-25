// Copyright 2019 Istio Authors
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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/testing/mock"
)

func TestBasicStartStop(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController()
	k := mock.NewKube()

	cl := fake.NewSimpleDynamicClient(k8sRuntime.NewScheme())
	k.AddResponse(cl, nil)

	c.Start(rt.NewProvider(k, 0), basicmeta.MustGet().KubeSource().Resources())
	defer c.Stop()

	c.Report(diag.Messages{})
	g.Consistently(cl.Actions).Should(BeEmpty())
}

func TestDoubleStart(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController()
	k := mock.NewKube()

	cl := fake.NewSimpleDynamicClient(k8sRuntime.NewScheme())
	k.AddResponse(cl, nil)

	c.Start(rt.NewProvider(k, 0), basicmeta.MustGet().KubeSource().Resources())
	c.Start(rt.NewProvider(k, 0), basicmeta.MustGet().KubeSource().Resources())
	defer c.Stop()

	c.Report(diag.Messages{})
	g.Consistently(cl.Actions).Should(BeEmpty())
}

func TestDoubleStop(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController()
	k := mock.NewKube()

	cl := fake.NewSimpleDynamicClient(k8sRuntime.NewScheme())
	k.AddResponse(cl, nil)

	c.Start(rt.NewProvider(k, 0), basicmeta.MustGet().KubeSource().Resources())
	c.Report(diag.Messages{})
	g.Consistently(cl.Actions).Should(BeEmpty())
	c.Stop()
	c.Stop()
}

func TestNoReconcilation(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController()
	k := mock.NewKube()

	cl := fake.NewSimpleDynamicClient(k8sRuntime.NewScheme())
	k.AddResponse(cl, nil)

	c.Start(rt.NewProvider(k, 0), basicmeta.MustGet().KubeSource().Resources())
	c.UpdateResourceStatus(basicmeta.Collection1, resource.NewName("foo", "bar"), "v1", "s1")
	defer c.Stop()

	g.Consistently(cl.Actions).Should(BeEmpty())
}

func TestBasicReconcilation_BeforeUpdate(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController()
	k := mock.NewKube()

	cl := fake.NewSimpleDynamicClient(k8sRuntime.NewScheme())
	k.AddResponse(cl, nil)

	r := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": "s1",
		},
	}

	cl.ReactionChain = nil
	cl.AddReactor("get", "Kind1s", func(action k8stesting.Action) (handled bool, ret k8sRuntime.Object, err error) {
		handled = true
		ret = r
		return
	})

	cl.AddReactor("update", "Kind1s", func(action k8stesting.Action) (handled bool, ret k8sRuntime.Object, err error) {
		handled = true
		return
	})

	c.Start(rt.NewProvider(k, 0), basicmeta.MustGet().KubeSource().Resources())
	c.UpdateResourceStatus(basicmeta.Collection1, resource.NewName("foo", "bar"), "v1", "s1")
	c.Report(diag.Messages{})
	defer c.Stop()

	g.Eventually(cl.Actions).Should(HaveLen(2))
	g.Expect(cl.Actions()[1]).To(BeAssignableToTypeOf(k8stesting.UpdateActionImpl{}))
	u := cl.Actions()[1].(k8stesting.UpdateActionImpl).Object.(*unstructured.Unstructured)
	g.Expect(u.Object["status"]).To(BeNil())
}

func TestBasicReconcilation_AfterUpdate(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController()
	k := mock.NewKube()

	cl := fake.NewSimpleDynamicClient(k8sRuntime.NewScheme())
	k.AddResponse(cl, nil)

	r := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": "s1",
		},
	}

	cl.ReactionChain = nil
	cl.AddReactor("get", "Kind1s", func(action k8stesting.Action) (
		handled bool, ret k8sRuntime.Object, err error) {
		handled = true
		ret = r
		return
	})

	cl.AddReactor("update", "Kind1s", func(action k8stesting.Action) (
		handled bool, ret k8sRuntime.Object, err error) {
		handled = true
		return
	})

	c.Start(rt.NewProvider(k, 0), basicmeta.MustGet().KubeSource().Resources())
	c.Report(diag.Messages{})
	c.UpdateResourceStatus(
		basicmeta.Collection1, resource.NewName("foo", "bar"), "v1", "s1")
	defer c.Stop()

	g.Eventually(cl.Actions).Should(HaveLen(2))
	g.Expect(cl.Actions()[1]).To(BeAssignableToTypeOf(k8stesting.UpdateActionImpl{}))
	u := cl.Actions()[1].(k8stesting.UpdateActionImpl).Object.(*unstructured.Unstructured)
	g.Expect(u.Object["status"]).To(BeNil())
}

func TestBasicReconcilation_NewStatus(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController()
	k := mock.NewKube()

	cl := fake.NewSimpleDynamicClient(k8sRuntime.NewScheme())
	k.AddResponse(cl, nil)

	r := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":            "foo",
				"namespace":       "bar",
				"resourceVersion": "v1",
			},
		},
	}

	cl.ReactionChain = nil
	cl.AddReactor("get", "Kind1s", func(action k8stesting.Action) (
		handled bool, ret k8sRuntime.Object, err error) {
		handled = true
		ret = r
		return
	})

	cl.AddReactor("update", "Kind1s", func(action k8stesting.Action) (
		handled bool, ret k8sRuntime.Object, err error) {
		handled = true
		return
	})

	e := resource.Entry{
		Origin: &rt.Origin{
			Collection: basicmeta.Collection1,
			Name:       resource.NewName("foo", "bar"),
			Version:    resource.Version("v1"),
		},
	}

	c.Start(rt.NewProvider(k, 0), basicmeta.MustGet().KubeSource().Resources())
	m := msg.NewInternalError(&e, "foo")
	c.Report(diag.Messages{m})
	defer c.Stop()

	g.Eventually(cl.Actions).Should(HaveLen(2))
	g.Expect(cl.Actions()[1]).To(BeAssignableToTypeOf(k8stesting.UpdateActionImpl{}))
	u := cl.Actions()[1].(k8stesting.UpdateActionImpl).Object.(*unstructured.Unstructured)
	g.Expect(u.Object["status"]).To(HavePrefix("Error [IST0001] Internal error: foo"))
}

func TestBasicReconcilation_UpdateError(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController()
	k := mock.NewKube()

	cl := fake.NewSimpleDynamicClient(k8sRuntime.NewScheme())
	k.AddResponse(cl, nil)

	r := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"resourceVersion": "v1",
			},
		},
	}

	cl.ReactionChain = nil
	cl.AddReactor("get", "Kind1s", func(action k8stesting.Action) (handled bool, ret k8sRuntime.Object, err error) {
		handled = true
		ret = r
		return
	})

	cl.AddReactor("update", "Kind1s", func(action k8stesting.Action) (handled bool, ret k8sRuntime.Object, err error) {
		handled = true
		err = fmt.Errorf("cheese not found")
		return
	})

	e := resource.Entry{
		Origin: &rt.Origin{
			Collection: basicmeta.Collection1,
			Name:       resource.NewName("foo", "bar"),
			Version:    resource.Version("v1"),
		},
	}

	c.Start(rt.NewProvider(k, 0), basicmeta.MustGet().KubeSource().Resources())
	m := msg.NewInternalError(&e, "foo")
	c.Report(diag.Messages{m})
	defer c.Stop()

	g.Eventually(cl.Actions).Should(HaveLen(2))
	g.Expect(cl.Actions()[1]).To(BeAssignableToTypeOf(k8stesting.UpdateActionImpl{}))
	u := cl.Actions()[1].(k8stesting.UpdateActionImpl).Object.(*unstructured.Unstructured)
	g.Expect(u.Object["status"]).To(HavePrefix("Error [IST0001] Internal error: foo"))
}

func TestBasicReconcilation_GetError(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController()
	k := mock.NewKube()

	cl := fake.NewSimpleDynamicClient(k8sRuntime.NewScheme())
	k.AddResponse(cl, nil)

	cl.ReactionChain = nil
	cl.AddReactor("get", "Kind1s", func(action k8stesting.Action) (handled bool, ret k8sRuntime.Object, err error) {
		handled = true
		err = fmt.Errorf("cheese not found")
		return
	})

	e := resource.Entry{
		Origin: &rt.Origin{
			Collection: basicmeta.Collection1,
			Name:       resource.NewName("foo", "bar"),
			Version:    resource.Version("v1"),
		},
	}

	c.Start(rt.NewProvider(k, 0), basicmeta.MustGet().KubeSource().Resources())
	m := msg.NewInternalError(&e, "foo")
	c.Report(diag.Messages{m})
	defer c.Stop()

	g.Eventually(cl.Actions).Should(HaveLen(1))
	g.Consistently(cl.Actions).Should(HaveLen(1))
}

func TestBasicReconcilation_VersionMismatch(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewController()
	k := mock.NewKube()

	cl := fake.NewSimpleDynamicClient(k8sRuntime.NewScheme())
	k.AddResponse(cl, nil)

	r := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"resourceVersion": "v2",
			},
		},
	}

	cl.ReactionChain = nil
	cl.AddReactor("get", "Kind1s", func(action k8stesting.Action) (
		handled bool, ret k8sRuntime.Object, err error) {
		handled = true
		ret = r
		return
	})

	cl.AddReactor("update", "Kind1s", func(action k8stesting.Action) (
		handled bool, ret k8sRuntime.Object, err error) {
		handled = true
		return
	})

	e := resource.Entry{
		Origin: &rt.Origin{
			Collection: basicmeta.Collection1,
			Name:       resource.NewName("foo", "bar"),
			Version:    resource.Version("v1"), // message for an older version
		},
	}

	c.Start(rt.NewProvider(k, 0), basicmeta.MustGet().KubeSource().Resources())
	m := msg.NewInternalError(&e, "foo")
	c.Report(diag.Messages{m})
	defer c.Stop()

	g.Eventually(cl.Actions).Should(HaveLen(1))
	g.Consistently(cl.Actions).Should(HaveLen(1))
}
