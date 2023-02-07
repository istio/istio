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

package crdwatcher

import (
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

// Controller watches a ConfigMap and calls the given callback when the ConfigMap changes.
// The ConfigMap is passed to the callback, or nil if it doesn't exist.
type Controller struct {
	informer  cache.SharedIndexInformer
	mutex     sync.RWMutex
	callbacks []func(name string)
}

// NewController returns a new CRD watcher controller.
func NewController(client kube.Client, callbacks ...func(name string)) *Controller {
	c := &Controller{
		callbacks: callbacks,
	}

	crdMetadataInformer := client.MetadataInformer().ForResource(collections.K8SApiextensionsK8SIoV1Customresourcedefinitions.Resource().
		GroupVersionResource()).Informer()
	_ = crdMetadataInformer.SetTransform(kube.StripUnusedFields)
	_, _ = crdMetadataInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			crd, ok := obj.(*metav1.PartialObjectMetadata)
			if !ok {
				// Shouldn't happen
				log.Errorf("wrong type %T: %v", obj, obj)
				return
			}
			c.mutex.RLock()
			defer c.mutex.RUnlock()
			for _, handler := range c.callbacks {
				handler(crd.Name)
			}
		},
		UpdateFunc: nil,
		DeleteFunc: nil,
	})

	c.informer = crdMetadataInformer
	return c
}

func (c *Controller) Run(stop <-chan struct{}) {
	go c.informer.Run(stop)
}

// HasSynced returns whether the underlying cache has synced and the callback has been called at least once.
func (c *Controller) HasSynced() bool {
	return c.informer.HasSynced()
}

// List returns a list of all the currently non-empty accumulators
func (c *Controller) List() []any {
	return c.informer.GetIndexer().List()
}

// GetByKey returns the accumulator associated with the given key
func (c *Controller) GetByKey(key string) (item any, exists bool, err error) {
	return c.informer.GetIndexer().GetByKey(key)
}

func (c *Controller) AddCallBack(cb func(name string)) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.callbacks = append(c.callbacks, cb)
}
