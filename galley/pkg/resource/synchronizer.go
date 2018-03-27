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
	"fmt"
	"reflect"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/galley/pkg/machinery"

	"istio.io/istio/galley/pkg/change"
	"istio.io/istio/pkg/log"
)

// Synchronizer is a Kubernetes controller that keeps a synchronized copy of an accessor set of custom
// resources in destination APIGroup/Version(s). The mapping from sources to destinations are specified by the
// respective schema.GroupVersion resources given during initialization.
type Synchronizer struct {
	// Lock for changing the running state of the accessor
	stateLock sync.Mutex
	// indicates whether the Synchronizer is running or not
	running bool

	// accessor for listening to, and operating on source and destination resources
	source      *accessor
	destination *accessor

	// work queue for processing change events
	queue workqueue.RateLimitingInterface

	// wait until the background process task is completed
	waitForProcess sync.WaitGroup

	// event hook that gets called after every completion of event processing loop. Useful for testing
	eventHook eventHookFn
}

type eventHookFn func(e interface{})

// NewSynchronizer returns a new instance of a Synchronizer
func NewSynchronizer(k machinery.Interface, resyncPeriod time.Duration, name string, source schema.GroupVersion,
	destination schema.GroupVersion, kind string, listKind string) (s *Synchronizer, err error) {
	return newSynchronizer(k, resyncPeriod, name, source, destination, kind, listKind, nil)
}

// NewSynchronizer returns a new instance of a Synchronizer that synchronizes the custom resource contents
// from the accessor APIGroup/Version to the destination APIGroup/Version. The kind and listKind is used
// when hydrating objects as unstructured.Unstructured.
func newSynchronizer(
	k machinery.Interface, resyncPeriod time.Duration, name string, source schema.GroupVersion,
	destination schema.GroupVersion, kind string, listKind string, eventHook eventHookFn) (
	s *Synchronizer, err error) {

	s = &Synchronizer{
		eventHook: eventHook,
	}

	if s.source, err = newAccessor(k, resyncPeriod, name,
		source, kind, listKind, s.handleInputEvent); err != nil {

		s = nil
		return
	}

	if s.destination, err = newAccessor(k, resyncPeriod, name,
		destination, kind, listKind, s.handleOutputEvent); err != nil {

		s = nil
		return
	}

	return
}

// Start the resource synchronizer
func (s *Synchronizer) Start() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	if s.running {
		log.Errorf("Synchronizer has already started: %s", s.source.name)
		return
	}

	log.Infof("Starting resource synchronization: %s(%s) => %s(%s)",
		s.source.name, s.source.gv, s.destination.name, s.destination.gv)

	s.queue = workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), s.source.name)
	s.source.start()
	s.destination.start()

	s.waitForProcess.Add(1)
	go s.process()

	s.running = true
}

// Stop the resource synchronizer.
func (s *Synchronizer) Stop() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	if !s.running {
		log.Error("Already stopped")
		return
	}

	s.source.stop()
	s.destination.stop()

	s.queue.ShutDown()
	s.waitForProcess.Wait()

	s.running = false
}

// background operation loop
func (s *Synchronizer) process() {
	for {
		item, shutdown := s.queue.Get()
		if shutdown {
			break
		}

		if info, ok := item.(*change.Info); !ok {
			log.Errorf("Got a non-change item from the queue: %v", item)
		} else if s.processChange(info) {
			s.queue.Forget(item)
			log.Debugf("item processing complete successfully: %v", item)
		}

		if s.eventHook != nil {
			s.eventHook(item)
		}
		s.queue.Done(item)
	}

	s.waitForProcess.Done()
}

func (s *Synchronizer) processChange(info *change.Info) bool {
	var sourceRes *unstructured.Unstructured
	var destinationRes *unstructured.Unstructured
	var err error

	if sourceRes, err = getResource(s.source, info.Name); err != nil {
		return false
	}
	if destinationRes, err = getResource(s.destination, info.Name); err != nil {
		return false
	}

	if sourceRes != nil {
		candidate := rewrite(sourceRes, s.destination.gv.String())

		iface := s.destination.client.Resource(s.destination.apiResource, candidate.GetNamespace())

		if destinationRes != nil {
			if !equals(candidate, destinationRes) {
				candidate.SetResourceVersion(destinationRes.GetResourceVersion())
				log.Infof("Updating resource: %s (%s/%v)", info.Name, s.destination.name, s.destination.gv)

				if _, err = iface.Update(candidate); err != nil {
					log.Errorf("Error updating resource %s (%s/%v): %v",
						info.Name, s.destination.name, s.destination.gv, err)
					return false
				}
			}
		} else {
			log.Infof("Creating resource: %s (%s/%v)", info.Name, s.destination.name, s.destination.gv)

			if _, err = iface.Create(candidate); err != nil {
				log.Errorf("Error creating resource %s (%s/%v): %v",
					info.Name, s.destination.name, s.destination.gv, err)
				return false
			}
		}
	} else {
		if destinationRes != nil {
			log.Infof("Deleting resource: %s (%s/%v)", info.Name, s.destination.name, s.destination.gv)
			if err = s.destination.iface.Delete(info.Name, &v1.DeleteOptions{}); err != nil {
				log.Errorf("Error deleting resource %s (%s/%v): %v",
					info.Name, s.destination.name, s.destination.gv, err)
				return false
			}
		}
	}

	return true
}

func getResource(a *accessor, name string) (*unstructured.Unstructured, error) {
	res, exists, err := a.informer.GetIndexer().Get(cache.ExplicitKey(name))
	if err != nil {
		log.Errorf("error getting resource from indexer: %v", err)
		return nil, err
	}

	if !exists {
		return nil, nil
	}

	u, ok := res.(*unstructured.Unstructured)
	if !ok {
		err = fmt.Errorf("unexpected type when reading %s from %s(%v): %v",
			name, a.name, a.gv, reflect.TypeOf(res))
		log.Errorf("error converting resource to unstructured: %v", err)
		return nil, err
	}

	return u, nil
}

func (s *Synchronizer) handleInputEvent(c *change.Info) {
	s.queue.AddRateLimited(c)
}

func (s *Synchronizer) handleOutputEvent(c *change.Info) {
	s.queue.AddRateLimited(c)
}
