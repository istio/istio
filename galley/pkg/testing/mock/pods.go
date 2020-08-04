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

func (p *podImpl) Create(_ context.Context, obj *apicorev1.Pod, opts metav1.CreateOptions) (*apicorev1.Pod, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	p.pods[obj.Name] = obj

	p.watches.Send(watch.Event{
		Type:   watch.Added,
		Object: obj,
	})
	return obj, nil
}

func (p *podImpl) Update(_ context.Context, obj *apicorev1.Pod, opts metav1.UpdateOptions) (*apicorev1.Pod, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	p.pods[obj.Name] = obj

	p.watches.Send(watch.Event{
		Type:   watch.Modified,
		Object: obj,
	})
	return obj, nil
}

func (p *podImpl) Delete(_ context.Context, name string, options metav1.DeleteOptions) error {
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

func (p *podImpl) List(_ context.Context, opts metav1.ListOptions) (*apicorev1.PodList, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	out := &apicorev1.PodList{}

	for _, v := range p.pods {
		out.Items = append(out.Items, *v)
	}

	return out, nil
}

func (p *podImpl) Watch(_ context.Context, opts metav1.ListOptions) (watch.Interface, error) {
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

func (p *podImpl) UpdateStatus(context.Context, *apicorev1.Pod, metav1.UpdateOptions) (*apicorev1.Pod, error) {
	panic("not implemented")
}

func (p *podImpl) DeleteCollection(_ context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	panic("not implemented")
}

func (p *podImpl) Get(_ context.Context, name string, options metav1.GetOptions) (*apicorev1.Pod, error) {
	panic("not implemented")
}

func (p *podImpl) Patch(_ context.Context, name string, pt types.PatchType, data []byte,
	opts metav1.PatchOptions, subresources ...string) (result *apicorev1.Pod, err error) {
	panic("not implemented")
}

func (p *podImpl) Bind(_ context.Context, binding *apicorev1.Binding, opts metav1.CreateOptions) error {
	panic("not implemented")
}

func (p *podImpl) Evict(_ context.Context, eviction *policy.Eviction) error {
	panic("not implemented")
}

func (p *podImpl) GetLogs(name string, opts *apicorev1.PodLogOptions) *rest.Request {
	panic("not implemented")
}

func (p *podImpl) GetEphemeralContainers(_ context.Context, podName string,
	options metav1.GetOptions) (*apicorev1.EphemeralContainers, error) {
	panic("not implemented")
}

func (p *podImpl) UpdateEphemeralContainers(_ context.Context, podName string, ephemeralContainers *apicorev1.EphemeralContainers,
	opts metav1.UpdateOptions) (*apicorev1.EphemeralContainers, error) {
	panic("not implemented")
}
