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

package mock

import (
	"context"
	"fmt"
	"sync"

	apicorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

var _ corev1.EndpointsInterface = &endpointsImpl{}

type endpointsImpl struct {
	mux       sync.Mutex
	endpoints map[string]*apicorev1.Endpoints
	watches   Watches
}

func newEndpointsInterface() corev1.EndpointsInterface {
	return &endpointsImpl{
		endpoints: make(map[string]*apicorev1.Endpoints),
	}
}

func (e *endpointsImpl) Create(ctx context.Context, obj *apicorev1.Endpoints, opts metav1.CreateOptions) (*apicorev1.Endpoints, error) {
	e.mux.Lock()
	defer e.mux.Unlock()

	e.endpoints[obj.Name] = obj

	e.watches.Send(watch.Event{
		Type:   watch.Added,
		Object: obj,
	})
	return obj, nil
}

func (e *endpointsImpl) Update(ctx context.Context, obj *apicorev1.Endpoints, opts metav1.UpdateOptions) (*apicorev1.Endpoints, error) {
	e.mux.Lock()
	defer e.mux.Unlock()

	e.endpoints[obj.Name] = obj

	e.watches.Send(watch.Event{
		Type:   watch.Modified,
		Object: obj,
	})
	return obj, nil
}

func (e *endpointsImpl) Delete(ctx context.Context, name string, options metav1.DeleteOptions) error {
	e.mux.Lock()
	defer e.mux.Unlock()

	obj := e.endpoints[name]
	if obj == nil {
		return fmt.Errorf("unable to delete endpoints %s", name)
	}

	delete(e.endpoints, name)

	e.watches.Send(watch.Event{
		Type:   watch.Deleted,
		Object: obj,
	})
	return nil
}

func (e *endpointsImpl) List(ctx context.Context, opts metav1.ListOptions) (*apicorev1.EndpointsList, error) {
	e.mux.Lock()
	defer e.mux.Unlock()

	out := &apicorev1.EndpointsList{}

	for _, v := range e.endpoints {
		out.Items = append(out.Items, *v)
	}

	return out, nil
}

func (e *endpointsImpl) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	e.mux.Lock()
	defer e.mux.Unlock()

	w := NewWatch()
	e.watches = append(e.watches, w)

	// Send add events for all current resources.
	for _, pod := range e.endpoints {
		w.Send(watch.Event{
			Type:   watch.Added,
			Object: pod,
		})
	}

	return w, nil
}

func (e *endpointsImpl) DeleteCollection(ctx context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	panic("not implemented")
}

func (e *endpointsImpl) Get(ctx context.Context, name string, options metav1.GetOptions) (*apicorev1.Endpoints, error) {
	panic("not implemented")
}

func (e *endpointsImpl) Patch(ctx context.Context, name string, pt types.PatchType,
	data []byte, opts metav1.PatchOptions, subresources ...string) (result *apicorev1.Endpoints, err error) {
	panic("not implemented")
}
