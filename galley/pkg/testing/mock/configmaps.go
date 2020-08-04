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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

var _ corev1.ConfigMapInterface = &configMapImpl{}

type configMapImpl struct {
	mux        sync.Mutex
	configMaps map[string]*v1.ConfigMap
	watches    Watches
}

func newConfigMapInterface() corev1.ConfigMapInterface {
	return &configMapImpl{
		configMaps: make(map[string]*v1.ConfigMap),
	}
}

func (c *configMapImpl) Create(ctx context.Context, obj *v1.ConfigMap, opts metav1.CreateOptions) (*v1.ConfigMap, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.configMaps[obj.Name] = obj

	c.watches.Send(watch.Event{
		Type:   watch.Added,
		Object: obj,
	})

	return obj, nil
}

func (c *configMapImpl) Update(ctx context.Context, obj *v1.ConfigMap, opts metav1.UpdateOptions) (*v1.ConfigMap, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.configMaps[obj.Name] = obj

	c.watches.Send(watch.Event{
		Type:   watch.Modified,
		Object: obj,
	})

	return obj, nil
}

func (c *configMapImpl) Delete(ctx context.Context, name string, options metav1.DeleteOptions) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	obj := c.configMaps[name]
	if obj == nil {
		return fmt.Errorf("unable to delete configMap %s", name)
	}

	delete(c.configMaps, name)

	c.watches.Send(watch.Event{
		Type:   watch.Deleted,
		Object: obj,
	})
	return nil
}

func (c *configMapImpl) List(ctx context.Context, opts metav1.ListOptions) (*v1.ConfigMapList, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	out := &v1.ConfigMapList{}

	for _, v := range c.configMaps {
		out.Items = append(out.Items, *v)
	}

	return out, nil
}

func (c *configMapImpl) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	w := NewWatch()
	c.watches = append(c.watches, w)

	// Send add events for all current resources.
	for _, cm := range c.configMaps {
		w.Send(watch.Event{
			Type:   watch.Added,
			Object: cm,
		})
	}

	return w, nil
}

func (c *configMapImpl) Get(ctx context.Context, name string, options metav1.GetOptions) (*v1.ConfigMap, error) {
	obj, ok := c.configMaps[name]
	if !ok {
		return nil, fmt.Errorf("configmap %q not found", name)
	}
	return obj, nil
}

func (c *configMapImpl) DeleteCollection(ctx context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	panic("not implemented")
}

func (c *configMapImpl) Patch(ctx context.Context, name string, pt types.PatchType,
	data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.ConfigMap, err error) {
	panic("not implemented")
}
