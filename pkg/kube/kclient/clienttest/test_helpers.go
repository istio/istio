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
	klabels "k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

type TestClient[T controllers.Object] struct {
	c kclient.ReadWriter[T]
	t test.Failer
	TestWriter[T]
}

type TestWriter[T controllers.Object] struct {
	c kclient.Writer[T]
	t test.Failer
}

func (t TestClient[T]) Get(name, namespace string) T {
	return t.c.Get(name, namespace)
}

func (t TestClient[T]) List(namespace string, selector klabels.Selector) []T {
	return t.c.List(namespace, selector)
}

func (t TestWriter[T]) Create(object T) T {
	t.t.Helper()
	res, err := t.c.Create(object)
	if err != nil {
		t.t.Fatalf("create %v/%v: %v", object.GetNamespace(), object.GetName(), err)
	}
	return res
}

func (t TestWriter[T]) Update(object T) T {
	t.t.Helper()
	res, err := t.c.Update(object)
	if err != nil {
		t.t.Fatalf("update %v/%v: %v", object.GetNamespace(), object.GetName(), err)
	}
	return res
}

func (t TestWriter[T]) UpdateStatus(object T) T {
	t.t.Helper()
	res, err := t.c.UpdateStatus(object)
	if err != nil {
		t.t.Fatalf("update status %v/%v: %v", object.GetNamespace(), object.GetName(), err)
	}
	return res
}

func (t TestWriter[T]) CreateOrUpdate(object T) T {
	t.t.Helper()
	res, err := kclient.CreateOrUpdate[T](t.c, object)
	if err != nil {
		t.t.Fatalf("createOrUpdate %v/%v: %v", object.GetNamespace(), object.GetName(), err)
	}
	return res
}

func (t TestWriter[T]) CreateOrUpdateStatus(object T) T {
	t.t.Helper()
	_, err := kclient.CreateOrUpdate(t.c, object)
	if err != nil {
		t.t.Fatalf("createOrUpdate %v/%v: %v", object.GetNamespace(), object.GetName(), err)
	}
	return t.UpdateStatus(object)
}

func (t TestWriter[T]) Delete(name, namespace string) {
	t.t.Helper()
	err := t.c.Delete(name, namespace)
	if err != nil {
		t.t.Fatalf("delete %v/%v: %v", namespace, name, err)
	}
}

// WrapReadWriter returns a client that calls t.Fatal on errors.
// Reads may be cached or uncached, depending on the input client.
func WrapReadWriter[T controllers.Object](t test.Failer, c kclient.ReadWriter[T]) TestClient[T] {
	return TestClient[T]{
		c: c,
		t: t,
		TestWriter: TestWriter[T]{
			c: c,
			t: t,
		},
	}
}

// Wrap returns a client that calls t.Fatal on errors.
// Reads may be cached or uncached, depending on the input client.
// Note: this is identical to WrapReadWriter but works around Go limitations, allowing calling w/o specifying
// generic parameters in the common case.
func Wrap[T controllers.Object](t test.Failer, c kclient.Client[T]) TestClient[T] {
	return WrapReadWriter[T](t, c)
}

// TrackerHandler returns an object handler that records each event
func TrackerHandler(tracker *assert.Tracker[string]) controllers.EventHandler[controllers.Object] {
	return controllers.EventHandler[controllers.Object]{
		AddFunc: func(obj controllers.Object) {
			tracker.Record("add/" + obj.GetName())
		},
		UpdateFunc: func(oldObj, newObj controllers.Object) {
			tracker.Record("update/" + newObj.GetName())
		},
		DeleteFunc: func(obj controllers.Object) {
			tracker.Record("delete/" + obj.GetName())
		},
	}
}

func Names[T controllers.Object](list []T) sets.String {
	return sets.New(slices.Map(list, func(a T) string { return a.GetName() })...)
}
