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
	"sync"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

type StaticCollection[T any] struct {
	*staticList[T]
}

type staticList[T any] struct {
	mu             sync.RWMutex
	vals           map[Key[T]]T
	eventHandlers  *handlerSet[T]
	id             collectionUID
	stop           <-chan struct{}
	collectionName string
}

func NewStaticCollection[T any](vals []T, opts ...CollectionOption) StaticCollection[T] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("Static[%v]", ptr.TypeName[T]())
	}

	res := make(map[Key[T]]T, len(vals))
	for _, v := range vals {
		res[GetKey(v)] = v
	}

	sl := &staticList[T]{
		eventHandlers:  &handlerSet[T]{},
		vals:           res,
		id:             nextUID(),
		stop:           o.stop,
		collectionName: o.name,
	}

	return StaticCollection[T]{
		staticList: sl,
	}
}

func (s *staticList[T]) GetKey(k Key[T]) *T {
	if o, f := s.vals[k]; f {
		return &o
	}
	return nil
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) name() string {
	return s.collectionName
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) uid() collectionUID {
	return s.id
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) dump() {
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) augment(a any) any {
	return a
}

// nolint: unused // (not true)
type staticListIndex[T any] struct {
	extract func(o T) []string
	parent  *staticList[T]
}

// nolint: unused // (not true)
func (s staticListIndex[T]) Lookup(key string) []any {
	var res []any
	for _, v := range s.parent.vals {
		have := s.extract(v)
		if slices.Contains(have, key) {
			res = append(res, v)
		}
	}
	return res
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) index(extract func(o T) []string) kclient.RawIndexer {
	return staticListIndex[T]{
		extract: extract,
		parent:  s,
	}
}

func (s *staticList[T]) List() []T {
	return maps.Values(s.vals)
}

func (s *staticList[T]) Register(f func(o Event[T])) Syncer {
	return registerHandlerAsBatched(s, f)
}

func (s *staticList[T]) Synced() Syncer {
	return alwaysSynced{}
}

func (s *staticList[T]) RegisterBatch(f func(o []Event[T], initialSync bool), runExistingState bool) Syncer {
	s.mu.Lock()
	defer s.mu.Unlock()
	var objs []Event[T]
	if runExistingState {
		for _, v := range s.vals {
			v := v
			objs = append(objs, Event[T]{
				New:   &v,
				Event: controllers.EventAdd,
			})
		}
	}
	return s.eventHandlers.Insert(f, s.Synced(), objs, s.stop)
}

var _ internalCollection[any] = &staticList[any]{}
