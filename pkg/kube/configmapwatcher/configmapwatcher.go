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
	"fmt"
	"time"

	"go.uber.org/atomic"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	infomersv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

// Controller watches a ConfigMap and calls the given callback when the ConfigMap changes.
// The ConfigMap is passed to the callback, or nil if it doesn't exist.
type Controller struct {
	informer infomersv1.ConfigMapInformer
	queue    workqueue.RateLimitingInterface

	configMapNamespace string
	configMapName      string
	callback           func(*v1.ConfigMap)

	hasSynced atomic.Bool
}

// NewController returns a new ConfigMap watcher controller.
func NewController(client kube.Client, namespace, name string, callback func(*v1.ConfigMap)) *Controller {
	c := &Controller{
		queue:              workqueue.NewRateLimitingQueue(workqueue.DefaultItemBasedRateLimiter()),
		configMapNamespace: namespace,
		configMapName:      name,
		callback:           callback,
	}

	// Although using a separate informer factory isn't ideal,
	// this does so to limit watching to only the specified ConfigMap.
	c.informer = informers.NewSharedInformerFactoryWithOptions(client.Kube(), 12*time.Hour,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			listOptions.FieldSelector = fields.OneTermEqualSelector("metadata.name", name).String()
		})).
		Core().V1().ConfigMaps()

	c.informer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				return false
			}
			return key == namespace+"/"+name
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				c.queue.Add(struct{}{})
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldCM := oldObj.(*v1.ConfigMap)
				newCM := newObj.(*v1.ConfigMap)
				if oldCM.ResourceVersion == newCM.ResourceVersion {
					return
				}
				c.queue.Add(struct{}{})
			},
			DeleteFunc: func(obj interface{}) {
				c.queue.Add(struct{}{})
			},
		},
	})
	return c
}

func (c *Controller) Run(stop <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	go c.informer.Informer().Run(stop)
	if !cache.WaitForCacheSync(stop, c.informer.Informer().HasSynced) {
		log.Error("failed to wait for cache sync")
		return
	}

	// Trigger initial callback.
	c.queue.Add(struct{}{})
	wait.Until(c.runWorker, time.Second, stop)
}

// HasSynced returns whether the underlying cache has synced and the callback has been called at least once.
func (c *Controller) HasSynced() bool {
	return c.hasSynced.Load()
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	obj, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(obj)

	log.Debug("processing queue item")
	if err := c.processItem(); err != nil {
		log.Error("error processing queue item (retrying)")
		c.queue.AddRateLimited(struct{}{})
	} else {
		c.queue.Forget(obj)
	}
	return true
}

func (c *Controller) processItem() error {
	cm, err := c.informer.Lister().ConfigMaps(c.configMapNamespace).Get(c.configMapName)
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("error fetching object %s error: %v", c.configMapName, err)
		}
		cm = nil
	}
	c.callback(cm)

	c.hasSynced.Store(true)
	return nil
}
