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

package configmapwatcher

import (
	"go.uber.org/atomic"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
)

// Controller watches a ConfigMap and calls the given callback when the ConfigMap changes.
// The ConfigMap is passed to the callback, or nil if it doesn't exist.
type Controller struct {
	configmaps kclient.Client[*v1.ConfigMap]
	queue      controllers.Queue

	configMapNamespace string
	configMapName      string
	callback           func(*v1.ConfigMap)

	hasSynced atomic.Bool
}

// NewController returns a new ConfigMap watcher controller.
func NewController(client kube.Client, namespace, name string, callback func(*v1.ConfigMap)) *Controller {
	c := &Controller{
		configMapNamespace: namespace,
		configMapName:      name,
		callback:           callback,
	}

	c.configmaps = kclient.NewFiltered[*v1.ConfigMap](client, kclient.Filter{
		Namespace:     namespace,
		FieldSelector: fields.OneTermEqualSelector(metav1.ObjectNameField, name).String(),
	})

	c.queue = controllers.NewQueue("configmap "+name, controllers.WithReconciler(c.processItem))
	c.configmaps.AddEventHandler(controllers.FilteredObjectSpecHandler(c.queue.AddObject, func(o controllers.Object) bool {
		// Filter out other configmaps
		return o.GetName() == name && o.GetNamespace() == namespace
	}))

	return c
}

func (c *Controller) Run(stop <-chan struct{}) {
	// Start informer immediately instead of with the rest. This is because we use configmapwatcher for
	// single types (so its never shared), and for use cases where we need the results immediately
	// during startup.
	c.configmaps.Start(stop)
	if !kube.WaitForCacheSync("configmap "+c.configMapName, stop, c.configmaps.HasSynced) {
		c.queue.ShutDownEarly()
		return
	}
	c.queue.Run(stop)
}

// HasSynced returns whether the underlying cache has synced and the callback has been called at least once.
func (c *Controller) HasSynced() bool {
	return c.queue.HasSynced()
}

func (c *Controller) processItem(name types.NamespacedName) error {
	cm := c.configmaps.Get(name.Name, name.Namespace)
	c.callback(cm)

	c.hasSynced.Store(true)
	return nil
}
