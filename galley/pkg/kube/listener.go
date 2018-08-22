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

package kube

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

	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/log"
)

// processorFn is a callback function that will receive change events back from listener.
type processorFn func(
	l *listener, eventKind resource.EventKind, key, version string, u *unstructured.Unstructured)

// listener is a simplified client interface for listening/getting Kubernetes resources in an unstructured way.
type listener struct {
	// Lock for changing the running state of the listener
	stateLock sync.Mutex

	spec ResourceSpec

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
	processor processorFn
}

// newListener returns a new instance of an listener.
func newListener(
	ifaces Interfaces, resyncPeriod time.Duration, spec ResourceSpec, processor processorFn) (*listener, error) {

	log.Debugf("Creating a new resource listener for: name='%s', gv:'%v'", spec.Singular, spec.GroupVersion())

	client, err := ifaces.DynamicInterface(spec.GroupVersion(), spec.Kind, spec.ListKind)
	if err != nil {
		return nil, err
	}

	iface := client.Resource(spec.APIResource(), "")

	return &listener{
		spec:         spec,
		resyncPeriod: resyncPeriod,
		iface:        iface,
		Client:       client,
		processor:    processor,
	}, nil
}

// Start the listener. This will commence listening and dispatching of events.
func (l *listener) start() {
	l.stateLock.Lock()
	defer l.stateLock.Unlock()

	if l.stopCh != nil {
		log.Errorf("already synchronizing resources: name='%s', gv='%v'", l.spec.Singular, l.spec.GroupVersion())
		return
	}

	log.Debugf("Starting listener for %s(%v)", l.spec.Singular, l.spec.GroupVersion())

	l.stopCh = make(chan struct{})

	l.informer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return l.iface.List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.Watch = true
				return l.iface.Watch(options)
			},
		},
		&unstructured.Unstructured{},
		l.resyncPeriod,
		cache.Indexers{})

	l.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) { l.handleEvent(resource.Added, obj) },
		UpdateFunc: func(old, new interface{}) {
			newRes := new.(*unstructured.Unstructured)
			oldRes := old.(*unstructured.Unstructured)
			if newRes.GetResourceVersion() == oldRes.GetResourceVersion() {
				// Periodic resync will send update events for all known resources.
				// Two different versions of the same resource will always have different RVs.
				return
			}
			l.handleEvent(resource.Updated, new)
		},
		DeleteFunc: func(obj interface{}) { l.handleEvent(resource.Deleted, obj) },
	})

	// Start CRD shared informer background process.
	go l.informer.Run(l.stopCh)
}

func (l *listener) waitForCacheSync() bool {
	// Wait for CRD cache sync.
	return cache.WaitForCacheSync(l.stopCh, l.informer.HasSynced)
}

// Stop the listener. This will stop publishing of events.
func (l *listener) stop() {
	l.stateLock.Lock()
	defer l.stateLock.Unlock()

	if l.stopCh == nil {
		log.Errorf("already stopped")
		return
	}

	close(l.stopCh)
	l.stopCh = nil
}

func (l *listener) handleEvent(c resource.EventKind, obj interface{}) {
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
		log.Infof("Recovered deleted object '%s' from tombstone", object.GetName())
	}

	key, err := cache.MetaNamespaceKeyFunc(object)
	if err != nil {
		log.Errorf("Error creating MetaNamespaceKey from object: %v", object)
		return
	}

	var u *unstructured.Unstructured

	if uns, ok := obj.(*unstructured.Unstructured); ok {
		u = uns
	}
	l.processor(l, c, key, object.GetResourceVersion(), u)
}
