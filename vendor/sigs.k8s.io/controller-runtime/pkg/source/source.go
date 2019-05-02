/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package source

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source/internal"

	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var log = logf.KBLog.WithName("source")

const (
	// defaultBufferSize is the default number of event notifications that can be buffered.
	defaultBufferSize = 1024
)

// Source is a source of events (eh.g. Create, Update, Delete operations on Kubernetes Objects, Webhook callbacks, etc)
// which should be processed by event.EventHandlers to enqueue reconcile.Requests.
//
// * Use Kind for events originating in the cluster (e.g. Pod Create, Pod Update, Deployment Update).
//
// * Use Channel for events originating outside the cluster (eh.g. GitHub Webhook callback, Polling external urls).
//
// Users may build their own Source implementations.  If their implementations implement any of the inject package
// interfaces, the dependencies will be injected by the Controller when Watch is called.
type Source interface {
	// Start is internal and should be called only by the Controller to register an EventHandler with the Informer
	// to enqueue reconcile.Requests.
	Start(handler.EventHandler, workqueue.RateLimitingInterface, ...predicate.Predicate) error
}

// Kind is used to provide a source of events originating inside the cluster from Watches (e.g. Pod Create)
type Kind struct {
	// Type is the type of object to watch.  e.g. &v1.Pod{}
	Type runtime.Object

	// cache used to watch APIs
	cache cache.Cache
}

var _ Source = &Kind{}

// Start is internal and should be called only by the Controller to register an EventHandler with the Informer
// to enqueue reconcile.Requests.
func (ks *Kind) Start(handler handler.EventHandler, queue workqueue.RateLimitingInterface,
	prct ...predicate.Predicate) error {

	// Type should have been specified by the user.
	if ks.Type == nil {
		return fmt.Errorf("must specify Kind.Type")
	}

	// cache should have been injected before Start was called
	if ks.cache == nil {
		return fmt.Errorf("must call CacheInto on Kind before calling Start")
	}

	// Lookup the Informer from the Cache and add an EventHandler which populates the Queue
	i, err := ks.cache.GetInformer(ks.Type)
	if err != nil {
		if kindMatchErr, ok := err.(*meta.NoKindMatchError); ok {
			log.Error(err, "if kind is a CRD, it should be installed before calling Start",
				"kind", kindMatchErr.GroupKind)
		}
		return err
	}
	i.AddEventHandler(internal.EventHandler{Queue: queue, EventHandler: handler, Predicates: prct})
	return nil
}

func (ks *Kind) String() string {
	if ks.Type != nil && ks.Type.GetObjectKind() != nil {
		return fmt.Sprintf("kind source: %v", ks.Type.GetObjectKind().GroupVersionKind().String())
	}
	return fmt.Sprintf("kind source: unknown GVK")
}

var _ inject.Cache = &Kind{}

// InjectCache is internal should be called only by the Controller.  InjectCache is used to inject
// the Cache dependency initialized by the ControllerManager.
func (ks *Kind) InjectCache(c cache.Cache) error {
	if ks.cache == nil {
		ks.cache = c
	}
	return nil
}

var _ Source = &Channel{}

// Channel is used to provide a source of events originating outside the cluster
// (e.g. GitHub Webhook callback).  Channel requires the user to wire the external
// source (eh.g. http handler) to write GenericEvents to the underlying channel.
type Channel struct {
	// once ensures the event distribution goroutine will be performed only once
	once sync.Once

	// Source is the source channel to fetch GenericEvents
	Source <-chan event.GenericEvent

	// stop is to end ongoing goroutine, and close the channels
	stop <-chan struct{}

	// dest is the destination channels of the added event handlers
	dest []chan event.GenericEvent

	// DestBufferSize is the specified buffer size of dest channels.
	// Default to 1024 if not specified.
	DestBufferSize int

	// destLock is to ensure the destination channels are safely added/removed
	destLock sync.Mutex
}

func (cs *Channel) String() string {
	return fmt.Sprintf("channel source: %p", cs)
}

var _ inject.Stoppable = &Channel{}

// InjectStopChannel is internal should be called only by the Controller.
// It is used to inject the stop channel initialized by the ControllerManager.
func (cs *Channel) InjectStopChannel(stop <-chan struct{}) error {
	if cs.stop == nil {
		cs.stop = stop
	}

	return nil
}

// Start implements Source and should only be called by the Controller.
func (cs *Channel) Start(
	handler handler.EventHandler,
	queue workqueue.RateLimitingInterface,
	prct ...predicate.Predicate) error {
	// Source should have been specified by the user.
	if cs.Source == nil {
		return fmt.Errorf("must specify Channel.Source")
	}

	// stop should have been injected before Start was called
	if cs.stop == nil {
		return fmt.Errorf("must call InjectStop on Channel before calling Start")
	}

	// use default value if DestBufferSize not specified
	if cs.DestBufferSize == 0 {
		cs.DestBufferSize = defaultBufferSize
	}

	cs.once.Do(func() {
		// Distribute GenericEvents to all EventHandler / Queue pairs Watching this source
		go cs.syncLoop()
	})

	dst := make(chan event.GenericEvent, cs.DestBufferSize)
	go func() {
		for evt := range dst {
			shouldHandle := true
			for _, p := range prct {
				if !p.Generic(evt) {
					shouldHandle = false
					break
				}
			}

			if shouldHandle {
				handler.Generic(evt, queue)
			}
		}
	}()

	cs.destLock.Lock()
	defer cs.destLock.Unlock()

	cs.dest = append(cs.dest, dst)

	return nil
}

func (cs *Channel) doStop() {
	cs.destLock.Lock()
	defer cs.destLock.Unlock()

	for _, dst := range cs.dest {
		close(dst)
	}
}

func (cs *Channel) distribute(evt event.GenericEvent) {
	cs.destLock.Lock()
	defer cs.destLock.Unlock()

	for _, dst := range cs.dest {
		// We cannot make it under goroutine here, or we'll meet the
		// race condition of writing message to closed channels.
		// To avoid blocking, the dest channels are expected to be of
		// proper buffer size. If we still see it blocked, then
		// the controller is thought to be in an abnormal state.
		dst <- evt
	}
}

func (cs *Channel) syncLoop() {
	for {
		select {
		case <-cs.stop:
			// Close destination channels
			cs.doStop()
			return
		case evt := <-cs.Source:
			cs.distribute(evt)
		}
	}
}

// Informer is used to provide a source of events originating inside the cluster from Watches (e.g. Pod Create)
type Informer struct {
	// Informer is the generated client-go Informer
	Informer toolscache.SharedIndexInformer
}

var _ Source = &Informer{}

// Start is internal and should be called only by the Controller to register an EventHandler with the Informer
// to enqueue reconcile.Requests.
func (is *Informer) Start(handler handler.EventHandler, queue workqueue.RateLimitingInterface,
	prct ...predicate.Predicate) error {

	// Informer should have been specified by the user.
	if is.Informer == nil {
		return fmt.Errorf("must specify Informer.Informer")
	}

	is.Informer.AddEventHandler(internal.EventHandler{Queue: queue, EventHandler: handler, Predicates: prct})
	return nil
}

func (is *Informer) String() string {
	return fmt.Sprintf("informer source: %p", is.Informer)
}

// Func is a function that implements Source
type Func func(handler.EventHandler, workqueue.RateLimitingInterface, ...predicate.Predicate) error

// Start implements Source
func (f Func) Start(evt handler.EventHandler, queue workqueue.RateLimitingInterface,
	pr ...predicate.Predicate) error {
	return f(evt, queue, pr...)
}

func (f Func) String() string {
	return fmt.Sprintf("func source: %p", f)
}
