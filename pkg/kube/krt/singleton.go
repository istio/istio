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
	"istio.io/istio/pkg/ptr"
)

// dummyValue is a placeholder value for use with dummyCollection.
type dummyValue struct{}

func (d dummyValue) ResourceName() string {
	return ""
}

// dummyCollection provides a trivial collection implementation that always provides a single dummyValue.
// This is an internal construct exclusively for implementing the "Singleton" pattern.
// This is so we can represent a singleton (a func() *O) as a collection (a func(I) *O).
// dummyCollection just returns a single "I" that is ignored.
type dummyCollection struct{}

func (d dummyCollection) GetKey(k Key[dummyValue]) *dummyValue {
	return ptr.Of(dummyValue{})
}

func (d dummyCollection) List(namespace string) []dummyValue {
	return []dummyValue{{}}
}

func (d dummyCollection) Register(f func(o Event[dummyValue])) Syncer {
	return registerHandlerAsBatched[dummyValue](d, f)
}

func (d dummyCollection) RegisterBatch(f func(o []Event[dummyValue])) Syncer {
	f([]Event[dummyValue]{{
		New:   ptr.Of(dummyValue{}),
		Event: controllers.EventAdd,
	}})
	return alwaysSynced{}
}

func (d dummyCollection) Name() string {
	return "empty"
}

func (d dummyCollection) Synced() Syncer {
	return alwaysSynced{}
}

var _ Collection[dummyValue] = dummyCollection{}

type collectionAdapter[T any] struct {
	c Collection[T]
}

func (c collectionAdapter[T]) Get() *T {
	// Guaranteed to be 0 or 1 len
	res := c.c.List("")
	if len(res) == 0 {
		return nil
	}
	return &res[0]
}

func (c collectionAdapter[T]) Register(f func(o Event[T])) Syncer {
	return c.c.Register(f)
}

func (c collectionAdapter[T]) Name() string {
	return c.c.Name()
}

func (c collectionAdapter[T]) AsCollection() Collection[T] {
	return c.c
}

var _ Singleton[any] = &collectionAdapter[any]{}

func NewSingleton[O any](hf TransformationEmpty[O], opts ...CollectionOption) Singleton[O] {
	col := NewCollection[dummyValue, O](dummyCollection{}, func(ctx HandlerContext, _ dummyValue) *O {
		return hf(ctx)
	}, opts...)
	return collectionAdapter[O]{col}
}
