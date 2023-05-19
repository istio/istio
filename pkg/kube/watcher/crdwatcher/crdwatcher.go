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

	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/pkg/log"
)

// Controller watches a ConfigMap and calls the given callback when the ConfigMap changes.
// The ConfigMap is passed to the callback, or nil if it doesn't exist.
type Controller struct {
	mutex     sync.RWMutex
	callbacks []func(name string)
	crds      kclient.Untyped
}

// NewController returns a new CRD watcher controller.
func NewController(client kube.Client, callbacks ...func(name string)) *Controller {
	c := &Controller{
		callbacks: callbacks,
	}

	c.crds = kclient.NewMetadata(client, gvr.CustomResourceDefinition, kclient.Filter{})

	c.crds.AddEventHandler(cache.ResourceEventHandlerFuncs{
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

	return c
}

// HasSynced returns whether the underlying cache has synced and the callback has been called at least once.
func (c *Controller) HasSynced() bool {
	return c.crds.HasSynced()
}

// Exists returns whether the CRD exists
func (c *Controller) Exists(key string) bool {
	return c.crds.Get(key, metav1.NamespaceAll) != nil
}

func (c *Controller) AddCallBack(cb func(name string)) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.callbacks = append(c.callbacks, cb)
}
