// Copyright 2018 Istio Authors
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

package kube

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/source/kube/client"
	"istio.io/istio/galley/pkg/source/kube/log"
	"istio.io/istio/galley/pkg/source/kube/schema"
	"istio.io/istio/galley/pkg/source/kube/stats"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	runtimeSchema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
)

// processorFn is a callback function that will receive change events back from listener.
type processorFn func(
	l *listener, eventKind resource.EventKind, key resource.FullName, version string, u *unstructured.Unstructured)

// listener is a simplified client interface for listening/getting Kubernetes resources in an unstructured way.
type listener struct {
	// Lock for changing the running state of the listener
	stateLock sync.Mutex

	spec schema.ResourceSpec

	resyncPeriod time.Duration

	// The dynamic resource interface for accessing custom resources dynamically.
	resourceClient dynamic.ResourceInterface

	// stopCh is used to quiesce the background activity during shutdown
	stopCh chan struct{}

	// SharedIndexInformer for watching/caching resources
	informer cache.SharedIndexInformer

	// The processor function to invoke to send the incoming changes.
	processor processorFn
}

// newListener returns a new instance of an listener.
func newListener(
	kubeInterface client.Interfaces, resyncPeriod time.Duration, spec schema.ResourceSpec, processor processorFn) (*listener, error) {

	if log.Scope.DebugEnabled() {
		log.Scope.Debugf("Creating a new resource listener for: name='%s', gv:'%v'", spec.Singular, spec.GroupVersion())
	}

	c, err := kubeInterface.DynamicInterface()
	if err != nil {
		log.Scope.Debugf("Error creating dynamic interface: %s: %v", spec.CanonicalResourceName(), err)
		return nil, err
	}

	resourceClient := c.Resource(spec.GroupVersion().WithResource(spec.Plural))

	return &listener{
		spec:           spec,
		resyncPeriod:   resyncPeriod,
		resourceClient: resourceClient,
		processor:      processor,
	}, nil
}

// Start the listener. This will commence listening and dispatching of events.
func (l *listener) start() {
	l.stateLock.Lock()
	defer l.stateLock.Unlock()

	if l.stopCh != nil {
		log.Scope.Errorf("already synchronizing resources: name='%s', gv='%v'", l.spec.Singular, l.spec.GroupVersion())
		return
	}

	log.Scope.Debugf("Starting listener for %s(%v)", l.spec.Singular, l.spec.GroupVersion())

	l.stopCh = make(chan struct{})

	l.informer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return l.resourceClient.List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.Watch = true
				return l.resourceClient.Watch(options)
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
		log.Scope.Errorf("already stopped")
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
			msg := fmt.Sprintf("error decoding object, invalid type: %v", reflect.TypeOf(obj))
			log.Scope.Error(msg)
			stats.RecordHandleEventError(msg)
			return
		}
		if object, ok = tombstone.Obj.(metav1.Object); !ok {
			msg := fmt.Sprintf("error decoding object tombstone, invalid type: %v", reflect.TypeOf(tombstone.Obj))
			log.Scope.Error(msg)
			stats.RecordHandleEventError(msg)
			return
		}
		log.Scope.Infof("Recovered deleted object '%s' from tombstone", object.GetName())
	}

	key := resource.FullNameFromNamespaceAndName(object.GetNamespace(), object.GetName())

	var u *unstructured.Unstructured

	if uns, ok := obj.(*unstructured.Unstructured); ok {
		u = uns

		// https://github.com/kubernetes/kubernetes/pull/63972
		// k8s machinery does not always preserve TypeMeta in list operations. Restore it
		// using aprior knowledge of the GVK for this listener.
		u.SetGroupVersionKind(runtimeSchema.GroupVersionKind{
			Group:   l.spec.Group,
			Version: l.spec.Version,
			Kind:    l.spec.Kind,
		})
	}

	if log.Scope.DebugEnabled() {
		log.Scope.Debugf("Sending event: [%v] from: %s", c, l.spec.CanonicalResourceName())
	}
	l.processor(l, c, key, object.GetResourceVersion(), u)
	stats.RecordHandleEventSuccess()
}
