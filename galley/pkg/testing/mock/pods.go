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

package mock

import (
	"fmt"
	"sync"

	apicorev1 "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

var _ corev1.PodInterface = &podImpl{}

type podImpl struct {
	mux     sync.Mutex
	pods    map[string]*apicorev1.Pod
	watches Watches
}

func newPodInterface() corev1.PodInterface {
	return &podImpl{
		pods: make(map[string]*apicorev1.Pod),
	}
}

func (p *podImpl) Create(obj *apicorev1.Pod) (*apicorev1.Pod, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	p.pods[obj.Name] = obj

	p.watches.Send(watch.Event{
		Type:   watch.Added,
		Object: obj,
	})
	return obj, nil
}

func (p *podImpl) Update(obj *apicorev1.Pod) (*apicorev1.Pod, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	p.pods[obj.Name] = obj

	p.watches.Send(watch.Event{
		Type:   watch.Modified,
		Object: obj,
	})
	return obj, nil
}

func (p *podImpl) Delete(name string, options *metav1.DeleteOptions) error {
	p.mux.Lock()
	defer p.mux.Unlock()

	obj := p.pods[name]
	if obj == nil {
		return fmt.Errorf("unable to delete pod %s", name)
	}

	delete(p.pods, name)

	p.watches.Send(watch.Event{
		Type:   watch.Deleted,
		Object: obj,
	})
	return nil
}

func (p *podImpl) List(opts metav1.ListOptions) (*apicorev1.PodList, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	out := &apicorev1.PodList{}

	for _, v := range p.pods {
		out.Items = append(out.Items, *v)
	}

	return out, nil
}

func (p *podImpl) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	w := NewWatch()
	p.watches = append(p.watches, w)

	// Send add events for all current resources.
	for _, pod := range p.pods {
		w.Send(watch.Event{
			Type:   watch.Added,
			Object: pod,
		})
	}

	return w, nil
}

func (p *podImpl) UpdateStatus(*apicorev1.Pod) (*apicorev1.Pod, error) {
	panic("not implemented")
}

func (p *podImpl) DeleteCollection(options *metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	panic("not implemented")
}

func (p *podImpl) Get(name string, options metav1.GetOptions) (*apicorev1.Pod, error) {
	panic("not implemented")
}

func (p *podImpl) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *apicorev1.Pod, err error) {
	panic("not implemented")
}

func (p *podImpl) Bind(binding *apicorev1.Binding) error {
	panic("not implemented")
}

func (p *podImpl) Evict(eviction *policy.Eviction) error {
	panic("not implemented")
}

func (p *podImpl) GetLogs(name string, opts *apicorev1.PodLogOptions) *rest.Request {
	panic("not implemented")
}
