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

package sync

import (
	"errors"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/galley/pkg/common"
	"istio.io/istio/galley/pkg/crd"
	"istio.io/istio/galley/pkg/resource"
	"istio.io/istio/pkg/log"
	// import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

// Server is the main entry point into the Galley code.
type Server struct {
	// stateLock for internal synchronizer state.
	stateLock sync.Mutex
	started   bool

	mapping      crd.Mapping
	kube         common.Kube
	resyncPeriod time.Duration

	queue workqueue.RateLimitingInterface

	// wait until the background process task is completed.
	waitForProcess sync.WaitGroup

	// The main CRD synchronizer
	crdSynchronizer *crd.Synchronizer

	resourceSynchronizers map[string]*resource.Synchronizer

	// event hook that gets called after every completion of event processing loop. Useful for testing.
	eventHook eventHookFn
}

type eventHookFn func(e interface{})

type event struct {
	added    bool
	name     string
	kind     string
	listKind string
}

// New returns a new instance of a Server.
func New(k common.Kube, mapping crd.Mapping, resyncPeriod time.Duration) *Server {
	return newServer(k, mapping, resyncPeriod, nil)
}

func newServer(
	k common.Kube, mapping crd.Mapping, resyncPeriod time.Duration, eventHook eventHookFn) *Server {

	return &Server{
		mapping:               mapping,
		kube:                  k,
		resyncPeriod:          resyncPeriod,
		eventHook:             eventHook,
		resourceSynchronizers: make(map[string]*resource.Synchronizer),
	}
}

// Start the server instance. It will start synchronizing CRDs.
func (s *Server) Start() error {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	if s.started {
		return errors.New("already started")
	}

	crdi, err := s.kube.CustomResourceDefinitionInterface()
	if err != nil {
		return err
	}

	s.queue = workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "server")
	listener := crd.SyncListener{
		OnDestinationAdded: func(name string, kind string, listKind string) {
			s.queue.AddRateLimited(&event{added: true, name: name, kind: kind, listKind: listKind})
		},
		OnDestinationRemoved: func(name string) {
			s.queue.AddRateLimited(&event{added: false, name: name})
		},
	}

	s.crdSynchronizer = crd.NewSynchronizer(crdi, s.mapping, s.resyncPeriod, listener)

	s.waitForProcess.Add(1)
	go s.process()

	if err := s.crdSynchronizer.Start(); err != nil {
		return err
	}

	s.started = true
	return nil
}

// Stop the server instance.
func (s *Server) Stop() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	if !s.started {
		return
	}

	s.queue.ShutDown()

	s.crdSynchronizer.Stop()
	for _, rsync := range s.resourceSynchronizers {
		rsync.Stop()
	}
	s.resourceSynchronizers = make(map[string]*resource.Synchronizer)

	s.waitForProcess.Wait()

	s.started = false
}

func (s *Server) process() {
	for {
		item, shutdown := s.queue.Get()
		if shutdown {
			break
		}

		if e, ok := item.(*event); !ok {
			log.Errorf("Got a non-event item from the queue: %v", item)
		} else if s.processEvent(e) {
			s.queue.Forget(item)
			log.Debugf("item processing completed successfully: %v", item)
		}

		s.queue.Done(item)
		if s.eventHook != nil {
			s.eventHook(item)
		}
		log.Debugf("marked item as done: %v", item)
	}

	s.waitForProcess.Done()
}

func (s *Server) processEvent(e *event) bool {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	if e.added {
		if _, found := s.resourceSynchronizers[e.name]; found {
			log.Errorf("found an already existing synchronizer for '%s', ignoring add event.", e.name)
			return true
		}

		gr := schema.ParseGroupResource(e.name)

		publicGv, internalGv, _ := s.mapping.GetGroupVersion(gr.Group)

		rs, err := resource.NewSynchronizer(s.kube, s.resyncPeriod, publicGv, internalGv, gr.Resource, e.kind, e.listKind)
		if err != nil {
			log.Errorf("error during resource synchronizer creation: name='%s', err='%v'", e.name, err)
			return false
		}

		rs.Start()
		s.resourceSynchronizers[e.name] = rs

	} else {
		if existing, found := s.resourceSynchronizers[e.name]; !found {
			log.Errorf("no existing synchronizer found for '%s', ignoring delete event.", e.name)
		} else {
			existing.Stop()

			delete(s.resourceSynchronizers, e.name)
		}
	}

	return true
}
