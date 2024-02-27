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

import "istio.io/istio/pkg/maps"

type staticList[T any] struct {
	vals map[Key[T]]T
}

func NewStaticCollection[T any](vals []T) Collection[T] {
	res := map[Key[T]]T{}
	for _, v := range vals {
		res[GetKey(v)] = v
	}
	return &staticList[T]{res}
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
func (s *staticList[T]) dump() {
}

// nolint: unused // (not true, its to implement an interface)
func (s *staticList[T]) augment(a any) any {
	return a
}

func (s *staticList[T]) List(namespace string) []T {
	if namespace == "" {
		return maps.Values(s.vals)
	}
	var res []T
	// Future improvement: shard outputs by namespace so we can query more efficiently
	for _, v := range s.vals {
		if getNamespace(v) == namespace {
			res = append(res, v)
		}
	}
	return res
}

func (s *staticList[T]) Register(f func(o Event[T])) Syncer {
	panic("StaticCollection does not support event handlers")
}

func (s *staticList[T]) Synced() Syncer {
	return alwaysSynced{}
}

func (s *staticList[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) Syncer {
	panic("StaticCollection does not support event handlers")
}

var _ internalCollection[any] = &staticList[any]{}
