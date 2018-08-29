/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package watchers

import (
	"reflect"
	"time"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	cache "k8s.io/client-go/tools/cache"
)

// ServiceUpdate struct
type ServiceUpdate struct {
	Service *api.Service
	Op      Operation
}

// ServiceWatcher struct
type ServiceWatcher struct {
	serviceController cache.Controller
	serviceLister     cache.Indexer
	broadcaster       *Broadcaster
}

// ServiceUpdatesHandler interface
type ServiceUpdatesHandler interface {
	OnServiceUpdate(serviceUpdate *ServiceUpdate)
}

func (svcw *ServiceWatcher) addEventHandler(obj interface{}) {
	service, ok := obj.(*api.Service)
	if !ok {
		return
	}
	svcw.broadcaster.Notify(&ServiceUpdate{Op: ADD, Service: service})
}

func (svcw *ServiceWatcher) deleteEventHandler(obj interface{}) {
	service, ok := obj.(*api.Service)
	if !ok {
		return
	}
	svcw.broadcaster.Notify(&ServiceUpdate{Op: REMOVE, Service: service})
}

func (svcw *ServiceWatcher) updateEventHandler(oldObj, newObj interface{}) {
	service, ok := newObj.(*api.Service)
	if !ok {
		return
	}
	if !reflect.DeepEqual(newObj, oldObj) {
		svcw.broadcaster.Notify(&ServiceUpdate{Op: UPDATE, Service: service})
	}
}

// RegisterHandler for register service update interface
func (svcw *ServiceWatcher) RegisterHandler(handler ServiceUpdatesHandler) {
	svcw.broadcaster.Add(ListenerFunc(func(instance interface{}) {
		handler.OnServiceUpdate(instance.(*ServiceUpdate))
	}))
}

// ListBySelector for list services with labels
func (svcw *ServiceWatcher) ListBySelector(set map[string]string) (ret []*api.Service, err error) {
	selector := labels.SelectorFromSet(set)
	err = cache.ListAll(svcw.serviceLister, selector, func(m interface{}) {
		ret = append(ret, m.(*api.Service))
	})
	return ret, err
}

// HasSynced return true if serviceController.HasSynced()
func (svcw *ServiceWatcher) HasSynced() bool {
	return svcw.serviceController.HasSynced()
}

// StartServiceWatcher start watching updates for services from Kuberentes API server
func StartServiceWatcher(clientset kubernetes.Interface, resyncPeriod time.Duration, stopCh <-chan struct{}) (*ServiceWatcher, error) {
	svcw := ServiceWatcher{}

	eventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc:    svcw.addEventHandler,
		DeleteFunc: svcw.deleteEventHandler,
		UpdateFunc: svcw.updateEventHandler,
	}

	svcw.broadcaster = NewBroadcaster()
	lw := cache.NewListWatchFromClient(clientset.CoreV1().RESTClient(), "services", metav1.NamespaceAll, fields.Everything())
	svcw.serviceLister, svcw.serviceController = cache.NewIndexerInformer(
		lw,
		&api.Service{}, resyncPeriod, eventHandler,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	go svcw.serviceController.Run(stopCh)
	return &svcw, nil
}
