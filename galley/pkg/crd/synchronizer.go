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

package crd

import (
	errs "errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/galley/pkg/machinery"

	"istio.io/istio/galley/pkg/change"
	"istio.io/istio/pkg/log"

	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Synchronizer is a Kubernetes controller that keeps a synchronized copy of an source set of CRDs in
// destination APIGroup/Version(s). The mapping from source to destination is specified by a mapping object.
// A passed-in listener can be used to detect when target CRDs are added/deleted.
type Synchronizer struct {

	// mutex for controlling the start/stop behavior.
	stateLock sync.Mutex

	// mapping from the source APIGroup/Versions to destination APIGroup/Versions.
	mapping Mapping

	// The resync period to use when initializing an informer.
	resyncPeriod time.Duration

	// The standard Kubernetes interface for accessing CRDs.
	crdi v1beta1.CustomResourceDefinitionInterface

	// stopCh is used to quiesce the background activity during shutdown.
	stopCh chan struct{}

	// wait until the background process task is completed.
	waitForProcess sync.WaitGroup

	// queue is used to schedule work in a single background go-process.
	queue workqueue.RateLimitingInterface

	// SharedIndexInformer for watching/caching CRDs.
	informer cache.SharedIndexInformer

	// Optional listener that receives notifications when destination CRDs get created/deleted.
	listener SyncListener

	// event hook that gets called after every completion of event processing loop. Useful for testing.
	eventHook eventHookFn
}

// SyncListener defines an interface for a listener that will get notified as destination Crds are added/removed.
type SyncListener struct {
	OnDestinationAdded   func(name string, kind string, listKind string)
	OnDestinationRemoved func(name string)
}

// NewSynchronizer returns a new instance of a Synchronizer. The returned Synchronizer is not started: this
// needs to be done explicitly using start()/Stop() methods.
func NewSynchronizer(k machinery.Interface, mapping Mapping, resyncPeriod time.Duration, listener SyncListener) (*Synchronizer, error) {
	return newSynchronizer(k, mapping, resyncPeriod, listener, nil)
}

type eventHookFn func(e interface{})

// NewSynchronizer returns a new instance of a Synchronizer. The returned Synchronizer is not started: this
// needs to be done explicitly using start()/Stop() methods.
func newSynchronizer(k machinery.Interface, mapping Mapping, resyncPeriod time.Duration, listener SyncListener,
	eventHook eventHookFn) (*Synchronizer, error) {

	return &Synchronizer{
		mapping:      mapping,
		resyncPeriod: resyncPeriod,
		listener:     listener,
		crdi:         k.CustomResourceDefinitionInterface(),
		eventHook:    eventHook,
	}, nil
}

// Start commences synchronizing CRDs.
func (s *Synchronizer) Start() error {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	if s.stopCh != nil {
		return errs.New("crd.Synchronizer: already started")
	}
	log.Infof("Starting CRD Synchronizer using the following mapping: \n%v", s.mapping)
	s.stopCh = make(chan struct{})
	s.queue = workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "CRD queue")

	s.informer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return s.crdi.List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.Watch = true
				return s.crdi.Watch(options)
			},
		},
		&apiext.CustomResourceDefinition{},
		s.resyncPeriod,
		cache.Indexers{})

	s.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) { s.handleEvent(change.Add, obj) },
		UpdateFunc: func(old, new interface{}) {
			newCrd := new.(*apiext.CustomResourceDefinition)
			oldCrd := old.(*apiext.CustomResourceDefinition)
			if newCrd.ResourceVersion == oldCrd.ResourceVersion {
				// Periodic resync will send update events for all known crds.
				// Two different versions of the same crds will always have different RVs.
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
		log.Warnf("Shutting down while waiting for CRD cache sync")
	}
	log.Debugf("Completed CRD cache sync and starting processing.")

	s.waitForProcess.Add(1)
	go s.process()

	return nil
}

// Stop the Synchronizer.
func (s *Synchronizer) Stop() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	if s.stopCh == nil {
		log.Warnf("crd.Synchronizer: The synchronizer is already stopped.")
		// Already stopped
		return
	}
	log.Info("Stopping CRD Synchronizer")

	s.queue.ShutDown()

	close(s.stopCh)
	s.stopCh = nil

	s.waitForProcess.Wait()
}

// transform the incoming object into a change.Info and enqueue it in the work queue.
func (s *Synchronizer) handleEvent(t change.Type, obj interface{}) {
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		var tombstone cache.DeletedFinalStateUnknown
		tombstone, ok = obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Errorf("error decoding object, invalid type: %v", reflect.TypeOf(obj))
			return
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			log.Errorf("error decoding object tombstone, invalid type: %v", reflect.TypeOf(tombstone.Obj))
			return
		}
		log.Infof("Recovered deleted object '%s' from tombstone", object.GetName())
	}

	info := &change.Info{
		Type: t,
		Name: object.GetName(),
	}

	s.queue.AddRateLimited(info)
}

// The main processing loop that takes out work from the queue and processes it in a panic-protected context.
func (s *Synchronizer) process() {
	for {
		item, shutdown := s.queue.Get()
		if shutdown {
			log.Debugf("exiting the crd synchronizer process loop due to shutdown signal")
			break
		}
		log.Debugf("incoming item for processing: %v", item)

		if info, ok := item.(*change.Info); !ok {
			log.Errorf("Got a non-change item from the queue: %v", item)
		} else if s.processChange(info) {
			s.queue.Forget(item)
			log.Debugf("item processing complete successfully: %v", item)
		}

		s.queue.Done(item)
		if s.eventHook != nil {
			s.eventHook(item)
		}
		log.Debugf("marked item as done: %v", item)
	}

	s.waitForProcess.Done()
}

// processChange does the actual processing of a change notification. Returns true, if the rate-limiter should
// forget the work and not reschedule again.
func (s *Synchronizer) processChange(info *change.Info) (forget bool) {

	gr := schema.ParseGroupResource(info.Name)
	sourceGv, destinationGv, found := s.mapping.GetGroupVersion(gr.Group)
	if !found {
		log.Debugf("skipping unrelated CRD change notification: '%v'", info.Name)
		forget = true
		return
	}

	sourceName := rewriteName(destinationGv.Group, info.Name)
	destinationName := rewriteName(sourceGv.Group, info.Name)

	var err error
	var sourceCrd *apiext.CustomResourceDefinition
	var destinationCrd *apiext.CustomResourceDefinition

	if sourceCrd, err = s.getCrd(destinationName); err != nil {
		return
	}

	if destinationCrd, err = s.getCrd(sourceName); err != nil {
		return
	}

	forget = true
	if sourceCrd != nil {
		candidate := rewrite(sourceCrd, destinationGv.Group, destinationGv.Version)

		// Update the destination CRD, if it is different than the source one.
		if destinationCrd != nil {
			candidate.ResourceVersion = destinationCrd.ResourceVersion

			if !equals(candidate, destinationCrd) {
				log.Infof("Updating destination CRD: %s => %s (versions: %s => %s)",
					sourceCrd.Name, candidate.Name, sourceCrd.ResourceVersion, candidate.ResourceVersion)
				if _, err = s.crdi.Update(candidate); err != nil {
					log.Errorf("Error during CRD update: name='%s', err:'%v'", sourceName, err)
					forget = false
				}
			}
		} else {
			// Insert a new CRD in destination, as it doesn't exist.
			log.Infof("Creating destination CRD: %s => %s (source version: %s)",
				sourceCrd.Name, candidate.Name, sourceCrd.ResourceVersion)
			if _, err = s.crdi.Create(candidate); err != nil {
				// TODO: Consider doing an update here if it is found to exists.
				log.Errorf("Error during CRD create: name='%s', err:'%v'", sourceName, err)
				forget = false
			}
		}
	} else {
		// The source CRD doesn't exist, we need to take corrective action
		if destinationCrd != nil {
			log.Infof("Deleting destination CRD: %s", destinationCrd.Name)
			if err = s.crdi.Delete(destinationCrd.Name, &metav1.DeleteOptions{}); err != nil {
				if !errors.IsNotFound(err) {
					log.Errorf("Error during CRD delete: name='%s', err:'%v'", sourceName, err)
					forget = false
				}
			}
		}
	}

	// If we get an Add/Delete event notify the listeners.
	if forget {
		if destinationName == info.Name {
			switch info.Type {
			case change.Add:
				// Source CRD should usually exists for the add case, although we may end up with a message ordering
				// issue where the source CRD could be added/deleted in rapid succession before the destination
				// could be added. In such a case, it is better to not send a notification as the destination
				// was deleted above (and we will get a notification about it soon).
				if s.listener.OnDestinationAdded != nil && sourceCrd != nil {
					s.listener.OnDestinationAdded(destinationName, sourceCrd.Spec.Names.Kind, sourceCrd.Spec.Names.ListKind)
				}
			case change.Delete:
				if s.listener.OnDestinationRemoved != nil {
					s.listener.OnDestinationRemoved(destinationName)
				}
			}
		}
	}

	return
}

func (s *Synchronizer) getCrd(name string) (*apiext.CustomResourceDefinition, error) {

	resource, exists, err := s.informer.GetIndexer().Get(cache.ExplicitKey(name))
	if err != nil {
		log.Errorf("error during resource get: err='%v', name='%s'", err, name)
		return nil, err
	}

	if !exists {
		return nil, nil
	}

	if crd, ok := resource.(*apiext.CustomResourceDefinition); ok {
		return crd, nil
	}

	err = fmt.Errorf("unexpected resource type encountered: name:'%s', resource:'%v'",
		name, reflect.TypeOf(resource))

	return nil, err
}
