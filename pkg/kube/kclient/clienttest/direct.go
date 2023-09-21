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
	"context"
	"reflect"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/istio/pkg/config/schema/kubeclient"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/test"
)

type directClient[T controllers.Object, PT any, TL runtime.Object] struct {
	kclient.Writer[T]
	t      test.Failer
	client kube.Client
}

func (d *directClient[T, PT, TL]) Get(name, namespace string) T {
	api := kubeclient.GetClient[T, TL](d.client, namespace)
	res, err := api.Get(context.Background(), name, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		d.t.Fatalf("get: %v", err)
	}
	return res
}

func (d *directClient[T, PT, TL]) List(namespace string, selector klabels.Selector) []T {
	api := kubeclient.GetClient[T, TL](d.client, namespace)
	res, err := api.List(context.Background(), metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		d.t.Fatalf("list: %v", err)
	}
	items := reflect.ValueOf(res).Elem().FieldByName("Items")
	ret := make([]T, 0, items.Len())
	for i := 0; i < items.Len(); i++ {
		itm := items.Index(i).Interface().(PT)
		ret = append(ret, any(&itm).(T))
	}
	return ret
}

var _ kclient.ReadWriter[controllers.Object] = &directClient[controllers.Object, any, controllers.Object]{}

// NewWriter returns a new client for the given type.
// Any errors will call t.Fatal.
func NewWriter[T controllers.ComparableObject](t test.Failer, c kube.Client) TestWriter[T] {
	return TestWriter[T]{t: t, c: kclient.NewWriteClient[T](c)}
}

// NewDirectClient returns a new client for the given type. Reads are directly to the API server.
// Any errors will call t.Fatal.
// Typically, clienttest.WrapReadWriter should be used to simply wrap an existing client when testing an informer.
// However, NewDirectClient can be useful if we do not need/want an informer and need direct reads.
// Generic parameters represent the type with and without a pointer, and the list type.
// Example: NewDirectClient[*Pod, Pod, PodList]
func NewDirectClient[T controllers.ComparableObject, PT any, TL runtime.Object](t test.Failer, c kube.Client) TestClient[T] {
	return WrapReadWriter[T](t, &directClient[T, PT, TL]{
		t:      t,
		client: c,
		Writer: kclient.NewWriteClient[T](c),
	})
}
