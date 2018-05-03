//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package client

import (
	"reflect"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/galley/pkg/change"
	"istio.io/istio/galley/pkg/kube"
	"istio.io/istio/galley/pkg/kube/schema"
	"istio.io/istio/pkg/log"
)

// ChangeProcessorFn is a callback function that will receive change events back from accessor.
type ChangeProcessorFn func(c *change.Info)

// Accessor is a simplified client interface for listening/getting Kubernetes resources in an unstructured way.
type Accessor struct {
	// Lock for changing the running state of the Accessor
	stateLock sync.Mutex

	t *schema.Type

	resyncPeriod time.Duration

	// Client for accessing the resources dynamically
	Client dynamic.Interface

	// The dynamic resource interface for accessing custom resources dynamically.
	iface dynamic.ResourceInterface

	// stopCh is used to quiesce the background activity during shutdown
	stopCh chan struct{}

	// SharedIndexInformer for watching/caching resources
	informer cache.SharedIndexInformer

	// The processor function to invoke to send the incoming changes.
	processor ChangeProcessorFn
}

// NewAccessor returns a new instance of an Accessor.
func NewAccessor(kube kube.Kube, resyncPeriod time.Duration, t *schema.Type, processor ChangeProcessorFn) (*Accessor, error) {

	log.Debugf("Creating a new resource Accessor for: name='%s', gv:'%v'", t.Singular, t.GroupVersion())

	var client dynamic.Interface
	client, err := kube.DynamicInterface(t.GroupVersion(), t.Kind, t.ListKind)
	if err != nil {
		return nil, err
	}

	iface := client.Resource(t.APIResource(), "")

	return &Accessor{
		t:            t,
		resyncPeriod: resyncPeriod,
		iface:        iface,
		Client:       client,
		processor:    processor,
	}, nil
}

// Start the accessor. This will commence listening and dispatching of events.
func (c *Accessor) Start() {
	c.stateLock.Lock()
	defer c.stateLock.Unlock()

	if c.stopCh != nil {
		log.Errorf("already synchronizing resources: name='%s', gv='%v'", c.t.Singular, c.t.GroupVersion())
		return
	}

	log.Debugf("Starting Accessor for %s(%v)", c.t.Singular, c.t.GroupVersion())

	c.stopCh = make(chan struct{})

	c.informer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.iface.List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.Watch = true
				return c.iface.Watch(options)
			},
		},
		&unstructured.Unstructured{},
		c.resyncPeriod,
		cache.Indexers{})

	c.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) { c.handleEvent(change.Add, obj) },
		UpdateFunc: func(old, new interface{}) {
			newRes := new.(*unstructured.Unstructured)
			oldRes := old.(*unstructured.Unstructured)
			if newRes.GetResourceVersion() == oldRes.GetResourceVersion() {
				// Periodic resync will send update events for all known resources.
				// Two different versions of the same resource will always have different RVs.
				return
			}
			c.handleEvent(change.Update, new)
		},
		DeleteFunc: func(obj interface{}) { c.handleEvent(change.Delete, obj) },
	})

	// Start CRD shared informer background process.
	go c.informer.Run(c.stopCh)

	// Wait for CRD cache sync.
	if !cache.WaitForCacheSync(c.stopCh, c.informer.HasSynced) {
		log.Warnf("Shutting down while waiting for Accessor cache sync %c(%v)", c.t.Singular, c.t.GroupVersion())
	}
	log.Debugf("Completed cache sync and listening. %s(%v)", c.t.Singular, c.t.GroupVersion())

	// Signal that full sync is done.
	info := &change.Info{
		Type: change.FullSync,
	}

	c.processor(info)
}

// Stop the accessor. This will stop publishing of events.
func (c *Accessor) Stop() {
	c.stateLock.Lock()
	defer c.stateLock.Unlock()

	if c.stopCh == nil {
		log.Errorf("already stopped")
		return
	}

	close(c.stopCh)
	c.stopCh = nil
}

func (c *Accessor) handleEvent(t change.Type, obj interface{}) {
	object, ok := obj.(metav1.Object)
	if !ok {
		var tombstone cache.DeletedFinalStateUnknown
		if tombstone, ok = obj.(cache.DeletedFinalStateUnknown); !ok {
			log.Errorf("error decoding object, invalid type: %v", reflect.TypeOf(obj))
			return
		}
		if object, ok = tombstone.Obj.(metav1.Object); !ok {
			log.Errorf("error decoding object tombstone, invalid type: %v", reflect.TypeOf(tombstone.Obj))
			return
		}
		log.Infof("Recovered deleted object '%c' from tombstone", object.GetName())
	}

	key, err := cache.MetaNamespaceKeyFunc(object)
	if err != nil {
		log.Errorf("Error creating a MetaNamespaceKey from object: %v", object)
		return
	}

	info := &change.Info{
		Type:         t,
		Name:         key,
		Version:      object.GetResourceVersion(),
		GroupVersion: c.t.GroupVersion(),
	}

	log.Debugf("Dispatching Accessor event: %v", info)
	c.processor(info)
}
