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
	"k8s.io/client-go/rest"
)

var _ corev1.ServiceInterface = &serviceImpl{}

type serviceImpl struct {
	mux      sync.Mutex
	services map[string]*apicorev1.Service
	watches  Watches
}

func newServiceInterface() corev1.ServiceInterface {
	return &serviceImpl{
		services: make(map[string]*apicorev1.Service),
	}
}

func (s *serviceImpl) Create(ctx context.Context, obj *apicorev1.Service, opts metav1.CreateOptions) (*apicorev1.Service, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.services[obj.Name] = obj

	s.watches.Send(watch.Event{
		Type:   watch.Added,
		Object: obj,
	})
	return obj, nil
}

func (s *serviceImpl) Update(ctx context.Context, obj *apicorev1.Service, opts metav1.UpdateOptions) (*apicorev1.Service, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.services[obj.Name] = obj

	s.watches.Send(watch.Event{
		Type:   watch.Modified,
		Object: obj,
	})
	return obj, nil
}

func (s *serviceImpl) Delete(ctx context.Context, name string, options metav1.DeleteOptions) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	obj := s.services[name]
	if obj == nil {
		return fmt.Errorf("unable to delete service %s", name)
	}

	delete(s.services, name)

	s.watches.Send(watch.Event{
		Type:   watch.Deleted,
		Object: obj,
	})
	return nil
}

func (s *serviceImpl) List(ctx context.Context, opts metav1.ListOptions) (*apicorev1.ServiceList, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	out := &apicorev1.ServiceList{}

	for _, v := range s.services {
		out.Items = append(out.Items, *v)
	}

	return out, nil
}

func (s *serviceImpl) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	w := NewWatch()
	s.watches = append(s.watches, w)

	// Send add events for all current resources.
	for _, pod := range s.services {
		w.Send(watch.Event{
			Type:   watch.Added,
			Object: pod,
		})
	}

	return w, nil
}

func (s *serviceImpl) UpdateStatus(context.Context, *apicorev1.Service, metav1.UpdateOptions) (*apicorev1.Service, error) {
	panic("not implemented")
}

func (s *serviceImpl) Get(ctx context.Context, name string, options metav1.GetOptions) (*apicorev1.Service, error) {
	panic("not implemented")
}

func (s *serviceImpl) Patch(ctx context.Context, name string, pt types.PatchType, data []byte,
	opts metav1.PatchOptions, subresources ...string) (result *apicorev1.Service, err error) {
	panic("not implemented")
}

func (s *serviceImpl) ProxyGet(scheme, name, port, path string, params map[string]string) rest.ResponseWrapper {
	panic("not implemented")
}
