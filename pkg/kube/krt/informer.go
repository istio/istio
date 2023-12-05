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
	inf            kclient.Informer[I]
	log            *istiolog.Scope
	collectionName string

	eventHandlers *handlers[I]
	augmentation  func(a any) any
	synced        chan struct{}
}

// nolint: unused // (not true, its to implement an interface)
func (i *informer[I]) augment(a any) any {
	if i.augmentation != nil {
		return i.augmentation(a)
	}
	return a
}

var _ internalCollection[controllers.Object] = &informer[controllers.Object]{}

func (i *informer[I]) _internalHandler() {}

func (i *informer[I]) Synced() Syncer {
	return channelSyncer{
		name:   i.collectionName,
		synced: i.synced,
	}
}

// nolint: unused // (not true, its to implement an interface)
func (i *informer[I]) dump() {
	// TODO: implement some useful dump here
}

func (i *informer[I]) name() string {
	return i.collectionName
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

func (i *informer[I]) Register(f func(o Event[I])) Syncer {
	return registerHandlerAsBatched[I](i, f)
}

func (i *informer[I]) RegisterBatch(f func(o []Event[I]), runExistingState bool) Syncer {
	// Note: runExistingState is NOT respected here.
	// Informer doesn't expose a way to do that. However, due to the runtime model of informers, this isn't a dealbreaker;
	// the handlers are all called async, so we don't end up with the same deadlocks we would have in the other collection types.
	// While this is quite kludgy, this is an internal interface so its not too bad.
	if !i.eventHandlers.Insert(f) {
		i.inf.AddEventHandler(EventHandler[I](func(o Event[I]) {
			f([]Event[I]{o})
		}))
	}
	return pollSyncer{
		name: fmt.Sprintf("%v handler", i.name()),
		f:    i.inf.HasSynced,
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
	h := &informer[I]{
		inf:            c,
		log:            log.WithLabels("owner", o.name),
		collectionName: o.name,
		eventHandlers:  &handlers[I]{},
		augmentation:   o.augmentation,
		synced:         make(chan struct{}),
	}

	go func() {
		// First, wait for the informer to populate
		if !kube.WaitForCacheSync(o.name, o.stop, c.HasSynced) {
			return
		}
		// Now, take all our handlers we have built up and register them...
		handlers := h.eventHandlers.MarkInitialized()
		for _, h := range handlers {
			c.AddEventHandler(EventHandler[I](func(o Event[I]) {
				h([]Event[I]{o})
			}))
		}
		// Now wait for handlers to sync
		if !kube.WaitForCacheSync(o.name+" handlers", o.stop, c.HasSynced) {
			c.ShutdownHandlers()
			return
		}
		close(h.synced)
		h.log.Infof("%v synced", h.name())
	}()
	return h
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
