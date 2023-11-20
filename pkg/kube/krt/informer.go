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
	inf kclient.Informer[I]
	log *istiolog.Scope
}

var _ Collection[controllers.Object] = &informer[controllers.Object]{}

func (i informer[I]) _internalHandler() {}

func (i informer[I]) Name() string {
	return fmt.Sprintf("informer[%T]", *new(I))
}

func (i informer[I]) List(namespace string) []I {
	res := i.inf.List(namespace, klabels.Everything())
	return res
}

func (i informer[I]) GetKey(k Key[I]) *I {
	ns, n := SplitKeyFunc(string(k))
	if got := i.inf.Get(n, ns); !controllers.IsNil(got) {
		return &got
	}
	return nil
}

func (i informer[I]) Register(f func(o Event[I])) {
	batchedRegister[I](i, f)
}

func (i informer[I]) RegisterBatch(f func(o []Event[I])) {
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

func WrapClient[I controllers.ComparableObject](c kclient.Informer[I]) Collection[I] {
	return informer[I]{c, log.WithLabels("owner", fmt.Sprintf("NewInformer[%v]", ptr.TypeName[I]()))}
}

func NewInformer[I controllers.ComparableObject](c kube.Client) Collection[I] {
	return NewInformerFiltered[I](c, kubetypes.Filter{})
}

func NewInformerFiltered[I controllers.ComparableObject](c kube.Client, filter kubetypes.Filter) Collection[I] {
	return WrapClient[I](kclient.NewFiltered[I](c, filter))
}
