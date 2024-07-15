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
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
)

type staticList[T any] struct {
	vals map[Key[T]]T
	id   collectionUID
}

func NewStaticCollection[T any](vals []T) Collection[T] {
	res := map[Key[T]]T{}
	for _, v := range vals {
		res[GetKey(v)] = v
	}
	return &staticList[T]{
		vals: res,
		id:   nextUID(),
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
	return "staticList"
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
	if runExistingState {
		f(slices.Map(s.List(), func(e T) Event[T] {
			return Event[T]{
				New:   &e,
				Event: controllers.EventAdd,
			}
		}), true)
	}
	return alwaysSynced{}
}

var _ internalCollection[any] = &staticList[any]{}
