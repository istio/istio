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

package resource

import (
	"reflect"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/galley/pkg/machinery"

	"istio.io/istio/galley/pkg/change"
	"istio.io/istio/pkg/log"
)

// accessor is a data access object for a particular type of custom resource, identified by its name,
// api group and version.
type accessor struct {
	// Lock for changing the running state of the accessor
	stateLock sync.Mutex

	// name, API group and version of the resource.
	name string
	gv   schema.GroupVersion

	resyncPeriod time.Duration

	// client for accessing the resources dynamically
	client dynamic.Interface

	// The dynamic resource interface for accessing custom resources dynamically.
	iface dynamic.ResourceInterface

	// metadata about the resource (i.e. name, kind, group, version etc.)
	apiResource *metav1.APIResource

	// stopCh is used to quiesce the background activity during shutdown
	stopCh chan struct{}

	// SharedIndexInformer for watching/caching resources
	informer cache.SharedIndexInformer

	// The processor function to invoke to send the incoming changes.
	processor changeProcessorFn
}

type changeProcessorFn func(c *change.Info)

// creates a new accessor instance.
func newAccessor(k machinery.Interface, resyncPeriod time.Duration, name string, gv schema.GroupVersion,
	kind string, listKind string, processor changeProcessorFn) (*accessor, error) {

	log.Debugf("Creating a new resource accessor for: name='%s', gv:'%v'", name, gv)

	client, err := k.DynamicInterface(gv, kind, listKind)
	if err != nil {
		return nil, err
	}

	apiResource := &metav1.APIResource{
		Name:       name,
		Group:      gv.Group,
		Version:    gv.Version,
		Namespaced: true,
		Kind:       kind,
	}
	iface := client.Resource(apiResource, "")

	return &accessor{
		resyncPeriod: resyncPeriod,
		name:         name,
		gv:           gv,
		iface:        iface,
		client:       client,
		processor:    processor,
		apiResource:  apiResource,
	}, nil
}

func (s *accessor) start() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	if s.stopCh != nil {
		log.Errorf("already synchronizing resources: name='%s', gv='%v'", s.name, s.gv)
		return
	}

	log.Debugf("Starting accessor for %s(%v)", s.name, s.gv)

	s.stopCh = make(chan struct{})

	s.informer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return s.iface.List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.Watch = true
				return s.iface.Watch(options)
			},
		},
		&unstructured.Unstructured{},
		s.resyncPeriod,
		cache.Indexers{})

	s.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) { s.handleEvent(change.Add, obj) },
		UpdateFunc: func(old, new interface{}) {
			newRes := new.(*unstructured.Unstructured)
			oldRes := old.(*unstructured.Unstructured)
			if newRes.GetResourceVersion() == oldRes.GetResourceVersion() {
				// Periodic resync will send update events for all known Deployments.
				// Two different versions of the same Deployment will always have different RVs.
				return
			}
			s.handleEvent(change.Update, new)
		},
		DeleteFunc: func(obj interface{}) { s.handleEvent(change.Delete, obj) },
	})

	// start CRD shared informer background process.
	go s.informer.Run(s.stopCh)

	// Wait for CRD cache sync.
	if !cache.WaitForCacheSync(s.stopCh, s.informer.HasSynced) {
		log.Warnf("Shutting down while waiting for accessor cache sync %s(%v)", s.name, s.gv)
	}
	log.Debugf("Completed cache sync and listening. %s(%v)", s.name, s.gv)
}

func (s *accessor) stop() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	if s.stopCh == nil {
		log.Errorf("already stopped")
		return
	}

	close(s.stopCh)
	s.stopCh = nil
}

func (s *accessor) handleEvent(t change.Type, obj interface{}) {
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
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
		log.Errorf("Error creating a MetaNamespaceKey from object: %v", object)
		return
	}

	info := &change.Info{
		Type:         t,
		Name:         key,
		GroupVersion: s.gv,
	}

	s.processor(info)
}
