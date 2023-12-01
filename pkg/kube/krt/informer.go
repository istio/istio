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

package krt

import (
	"fmt"
	"strings"
	"sync"

	"go.uber.org/atomic"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
)

type informer[I controllers.ComparableObject] struct {
	inf  kclient.Informer[I]
	log  *istiolog.Scope
	name string

	eventHandlers *handlers[I]
	augmentation  func(a any) any
	synced        chan struct{}
}

type eventQueue[I any] struct {
	q        []Event[I]
	handlers *handlers[I]
	mu       sync.RWMutex
	started  *atomic.Bool
}

func newEventQueue[I any](h *handlers[I]) *eventQueue[I] {
	return &eventQueue[I]{
		handlers: h,
		started:  atomic.NewBool(false),
	}
}

func (e *eventQueue[T]) add(t Event[T]) {
	if e.started.Load() {
		e.process([]Event[T]{t})
		// Hot path
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// may have started since we get the lock
	if e.started.Load() {
		e.process([]Event[T]{t})
		return
	}
	e.q = append(e.q, t)
}

func (e *eventQueue[I]) process(t []Event[I]) {
	handlers := e.handlers.Get()
	for _, handler := range handlers {
		handler(t)
	}
}

func (e *eventQueue[I]) drain() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.process(e.q)
	e.q = nil
	e.started.Store(true)
}

func (i *informer[I]) augment(a any) any {
	if i.augmentation != nil {
		return i.augmentation(a)
	}
	return a
}

var _ augmenter = &informer[controllers.Object]{}

var _ Collection[controllers.Object] = &informer[controllers.Object]{}

func (i *informer[I]) _internalHandler() {}

func (i *informer[I]) Synced() <-chan struct{} {
	return i.synced
}

func (i *informer[I]) Run(stop <-chan struct{}) {
	//kube.WaitForCacheSync(i.name, stop, i.inf.HasSynced)
	//i.eventQueue.drain()
	//<-stop
	//i.inf.ShutdownHandlers()
}

func (i *informer[I]) Name() string {
	return i.name
}

func (i *informer[I]) List(namespace string) []I {
	res := i.inf.List(namespace, klabels.Everything())
	return res
}

func (i *informer[I]) GetKey(k Key[I]) *I {
	ns, n := splitKeyFunc(string(k))
	if got := i.inf.Get(n, ns); !controllers.IsNil(got) {
		return &got
	}
	return nil
}

func (i *informer[I]) Register(f func(o Event[I])) {
	registerHandlerAsBatched[I](i, f)
}

func (i *informer[I]) RegisterBatch(f func(o []Event[I])) {
	if !i.eventHandlers.Insert(f) {
		i.inf.AddEventHandler(EventHandler[I](func(o Event[I]) {
			f([]Event[I]{o})
		}))
	}
}

func EventHandler[I controllers.ComparableObject](handler func(o Event[I])) cache.ResourceEventHandler {
	return controllers.EventHandler[I]{
		AddFunc: func(obj I) {
			handler(Event[I]{
				New:   &obj,
				Event: controllers.EventAdd,
			})
		},
		UpdateFunc: func(oldObj, newObj I) {
			handler(Event[I]{
				Old:   &oldObj,
				New:   &newObj,
				Event: controllers.EventUpdate,
			})
		},
		DeleteFunc: func(obj I) {
			handler(Event[I]{
				Old:   &obj,
				Event: controllers.EventDelete,
			})
		},
	}
}

func WrapClient[I controllers.ComparableObject](c kclient.Informer[I], opts ...CollectionOption) Collection[I] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("NewInformer[%v]", ptr.TypeName[I]())
	}
	inf := &informer[I]{
		inf:           c,
		log:           log.WithLabels("owner", o.name),
		name:          o.name,
		eventHandlers: &handlers[I]{},
		augmentation:  o.augmentation,
		synced:        make(chan struct{}),
	}

	stop := make(chan struct{})
	go func() {
		// First, wait for the informer to populate
		if !kube.WaitForCacheSync(o.name, stop, c.HasSynced) {
			return
		}
		handlers := inf.eventHandlers.Stop()
		// Now, take all our handlers we have built up
		for _, h := range handlers {
			c.AddEventHandler(EventHandler[I](func(o Event[I]) {
				h([]Event[I]{o})
			}))
		}
		// Now wait for handlers to sync
		kube.WaitForCacheSync(o.name+" handlers", stop, c.HasSynced)
		if !kube.WaitForCacheSync(o.name, stop, c.HasSynced) {
			c.ShutdownHandlers()
			return
		}
		close(inf.synced)
		inf.log.Infof("informers synced")
		//<-stop
		//c.ShutdownHandlers()
	}()
	// go inf.Run()
	return inf
}

func NewInformer[I controllers.ComparableObject](c kube.Client, opts ...CollectionOption) Collection[I] {
	return NewInformerFiltered[I](c, kubetypes.Filter{}, opts...)
}

func NewInformerFiltered[I controllers.ComparableObject](c kube.Client, filter kubetypes.Filter, opts ...CollectionOption) Collection[I] {
	return WrapClient[I](kclient.NewFiltered[I](c, filter), opts...)
}

func splitKeyFunc(key string) (namespace, name string) {
	parts := strings.Split(key, "/")
	switch len(parts) {
	case 1:
		// name only, no namespace
		return "", parts[0]
	case 2:
		// namespace and name
		return parts[0], parts[1]
	}

	return "", ""
}
