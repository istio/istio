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
	"istio.io/istio/pkg/test"
)

type TestCached[T controllers.Object] interface {
	Get(name, namespace string) T
	List(namespace string, selector klabels.Selector) []T
	Create(object T) T
	Update(object T) T
	CreateOrUpdate(object T) T
	Delete(name, namespace string)
}

type testCached[T controllers.Object] struct {
	c kclient.Client[T]
	t test.Failer
}

func (t testCached[T]) Get(name, namespace string) T {
	return t.c.Get(name, namespace)
}

func (t testCached[T]) List(namespace string, selector klabels.Selector) []T {
	return t.c.List(namespace, selector)
}

func (t testCached[T]) Create(object T) T {
	res, err := t.c.Create(object)
	if err != nil {
		t.t.Fatalf("create %v/%v: %v", object.GetNamespace(), object.GetName(), err)
	}
	return res
}

func (t testCached[T]) Update(object T) T {
	res, err := t.c.Update(object)
	if err != nil {
		t.t.Fatalf("update %v/%v: %v", object.GetNamespace(), object.GetName(), err)
	}
	return res
}

func (t testCached[T]) CreateOrUpdate(object T) T {
	res, err := kclient.CreateOrUpdate(t.c, object)
	if err != nil {
		t.t.Fatalf("createOrUpdate %v/%v: %v", object.GetNamespace(), object.GetName(), err)
	}
	return res
}

func (t testCached[T]) Delete(name, namespace string) {
	err := t.c.Delete(name, namespace)
	if err != nil {
		t.t.Fatalf("delete %v/%v: %v", namespace, name, err)
	}
}

// Wrap returns a kclient.Client that calls t.Fatal on errors
func Wrap[T controllers.Object](t test.Failer, c kclient.Client[T]) TestCached[T] {
	return testCached[T]{
		c: c,
		t: t,
	}
}
