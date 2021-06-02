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

package crdclient

import (
	"reflect"
	"sync"
	"time"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/util/workqueue"

	//  import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	//  import OIDC cluster authentication plugin, e.g. for Tectonic
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/pkg/log"
)

// cacheHandler abstracts the logic of an informer with a set of handlers. Handlers can be added at runtime
// and will be invoked on each informer event.
type cacheHandler struct {
	client              *Client
	informer            cache.SharedIndexInformer
	handlers            []func(config.Config, config.Config, model.Event)
	schema              collection.Schema
	lister              func(namespace string) cache.GenericNamespaceLister
	currentObjMap       sync.Map
	queue               workqueue.RateLimitingInterface
	jitterPeriod        time.Duration
	maxConcurrentWorker int
}

func (h *cacheHandler) onEvent(old interface{}, curr interface{}, event model.Event) error {
	if err := h.client.checkReadyForEvents(curr); err != nil {
		return err
	}

	currItem, ok := curr.(runtime.Object)
	if !ok {
		scope.Warnf("New Object can not be converted to runtime Object %v, is type %T", curr, curr)
		return nil
	}
	currConfig := *TranslateObject(currItem, h.schema.Resource().GroupVersionKind(), h.client.domainSuffix)

	var oldConfig config.Config
	if old != nil {
		oldItem, ok := old.(runtime.Object)
		if !ok {
			log.Warnf("Old Object can not be converted to runtime Object %v, is type %T", old, old)
			return nil
		}
		oldConfig = *TranslateObject(oldItem, h.schema.Resource().GroupVersionKind(), h.client.domainSuffix)
	}

	// TODO we may consider passing a pointer to handlers instead of the value. While spec is a pointer, the meta will be copied
	for _, f := range h.handlers {
		f(oldConfig, currConfig, event)
	}
	return nil
}

func (h *cacheHandler) StartWorker(stop <-chan struct{}) {
	for i := 0; i < h.maxConcurrentWorker; i++ {
		go wait.Until(h.worker, h.jitterPeriod, stop)
	}
}

func (h *cacheHandler) worker() {
	for {
		h.processWorkerItem()
	}
}

func (h *cacheHandler) processWorkerItem() {
	obj, shutdown := h.queue.Get()
	if shutdown {
		scope.Infof("kind :%+v queue len: %d shutdown", h.schema.Resource().Kind(), h.queue.Len())
		return
	}
	namespacedObj, ok := obj.(types.NamespacedName)
	if !ok {
		h.queue.Done(obj)
		return
	}
	err := h.onEventNew(namespacedObj)
	if err != nil {
		h.queue.AddRateLimited(namespacedObj)
		return
	}
	h.queue.Done(obj)
}

func (h *cacheHandler) onEventNew(obj types.NamespacedName) error {
	// get current obj
	event := model.EventAdd
	oldConfig := config.Config{}
	currConfig := config.Config{}

	currObject, err := h.lister(obj.Namespace).Get(obj.Name)
	// if current obj is not exists , it is a delete event
	if err != nil {
		if kerrors.IsNotFound(err) {
			event = model.EventDelete
			currStore, ok := h.currentObjMap.Load(types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace})
			if !ok {
				return nil
			}
			currObject, ok = currStore.(runtime.Object)
			if !ok {
				scope.Warnf("New Object can not be converted to runtime Object %+v", currStore)
				return nil
			}
		} else {
			scope.Errorf("Get Object can not be converted to runtime Object %+v", obj)
			return err
		}
	}

	if err := h.client.checkReadyForEvents(currObject); err != nil {
		return err
	}

	currConfig = *TranslateObject(currObject, h.schema.Resource().GroupVersionKind(), h.client.domainSuffix)

	if event != model.EventDelete {
		oldStore, ok := h.currentObjMap.Load(types.NamespacedName{Name: currConfig.Name, Namespace: currConfig.Namespace})
		if ok {
			event = model.EventUpdate
			oldItem, ok := oldStore.(runtime.Object)
			if !ok {
				log.Warnf("Old Object can not be converted to runtime Object %+v", oldStore)
				return nil
			}
			oldConfig = *TranslateObject(oldItem, h.schema.Resource().GroupVersionKind(), h.client.domainSuffix)
		}
	}

	for _, f := range h.handlers {
		f(oldConfig, currConfig, event)
	}
	h.currentObjMap.Store(types.NamespacedName{Name: currConfig.Name, Namespace: currConfig.Namespace}, currObject)
	return nil
}

func createCacheHandler(cl *Client, schema collection.Schema, i informers.GenericInformer) *cacheHandler {
	h := &cacheHandler{
		client:              cl,
		schema:              schema,
		informer:            i.Informer(),
		queue:               workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), schema.Resource().Kind()),
		jitterPeriod:        time.Millisecond * 100,
		maxConcurrentWorker: 1,
	}
	h.lister = func(namespace string) cache.GenericNamespaceLister {
		if schema.Resource().IsClusterScoped() {
			return i.Lister()
		}
		return i.Lister().ByNamespace(namespace)
	}
	kind := schema.Resource().GroupVersionKind()
	i.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			incrementEvent(kind.Kind, "add")
			newObj, ok := obj.(metav1.Object)
			if !ok {
				log.Errorf("eventHandler %s parse obj err", kind)
				return
			}
			h.queue.Add(types.NamespacedName{Name: newObj.GetName(), Namespace: newObj.GetNamespace()})
		},
		UpdateFunc: func(old, cur interface{}) {
			if !reflect.DeepEqual(old, cur) {
				incrementEvent(kind.Kind, "update")
				newObj, ok := cur.(metav1.Object)
				if !ok {
					log.Errorf("eventHandler %s parse obj err", kind)
					return
				}
				h.queue.Add(types.NamespacedName{Name: newObj.GetName(), Namespace: newObj.GetNamespace()})
			} else {
				incrementEvent(kind.Kind, "updatesame")
			}
		},
		DeleteFunc: func(obj interface{}) {
			incrementEvent(kind.Kind, "delete")
			newObj, ok := obj.(metav1.Object)
			if !ok {
				log.Errorf("eventHandler %s parse obj err", kind)
				return
			}
			h.queue.Add(types.NamespacedName{Name: newObj.GetName(), Namespace: newObj.GetNamespace()})
		},
	})
	return h
}
