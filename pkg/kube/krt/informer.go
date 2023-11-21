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
	inf  kclient.Informer[I]
	log  *istiolog.Scope
	name string

	augmentation func(a any) any
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
	i.inf.AddEventHandler(EventHandler(func(o Event[I]) {
		if i.log.DebugEnabled() {
			i.log.WithLabels("key", GetKey(o.Latest()), "type", o.Event).Debugf("handling event")
		}
		f([]Event[I]{o})
	}))
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
	return &informer[I]{
		inf:          c,
		log:          log.WithLabels("owner", o.name),
		name:         o.name,
		augmentation: o.augmentation,
	}
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
