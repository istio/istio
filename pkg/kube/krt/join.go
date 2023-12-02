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
	"istio.io/istio/pkg/ptr"
)

type join[T any] struct {
	name        string
	collections []Collection[T]
	merge       func(ts []T) T
	synced      <-chan struct{}
}

func (j *join[I]) Dump() {
	log.Errorf("> BEGIN DUMP (join %v)", j.name)
	for _, c := range j.collections {
		Dump(c)
	}
	log.Errorf("< END DUMP (join %v)", j.name)
}

func (j *join[T]) GetKey(k Key[T]) *T {
	var found []T
	for _, c := range j.collections {
		if r := c.GetKey(k); r != nil {
			found = append(found, *r)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return ptr.Of(j.merge(found))
}

func (j *join[T]) List(namespace string) []T {
	res := map[Key[T]][]T{}
	for _, c := range j.collections {
		for _, i := range c.List(namespace) {
			key := GetKey(i)
			res[key] = append(res[key], i)
		}
	}
	var l []T
	for _, ts := range res {
		l = append(l, j.merge(ts))
	}
	return l
}

func (j *join[T]) Name() string { return j.name }

func (j *join[T]) Register(f func(o Event[T])) HandlerRegistration {
	for _, c := range j.collections {
		c.Register(f)
	}
	// TODO: actually check...
	synced := make(chan struct{})
	close(synced)
	return handlerRegistration{synced: synced}
}

var _ registerBatchInterface[any] = &join[any]{}

func (j *join[T]) RegisterBatch(f func(o []Event[T])) HandlerRegistration {
	for _, c := range j.collections {
		c.RegisterBatch(f)
	}
	// TODO: actually check...
	synced := make(chan struct{})
	close(synced)
	return handlerRegistration{synced: synced}
}

func (j *join[T]) registerBatch(f func(o []Event[T]), runExistingState bool)  {
	for _, c := range j.collections {
		if rb, ok := c.(registerBatchInterface[T]); ok {
			rb.registerBatch(f, runExistingState)
		} else {
			c.RegisterBatch(f)
		}
	}
}

func (j *join[T]) Run(stop <-chan struct{}) {
}

func (j *join[T]) Synced() <-chan struct{} {
	return j.synced
}

func JoinCollection[T any](cs []Collection[T], opts ...CollectionOption) Collection[T] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("Join[%v]", ptr.TypeName[T]())
	}
	synced := make(chan struct{})
	stop := make(chan struct{}) // TODO: pass in from user
	go func() {
		for _, c := range cs {
			if !WaitForCacheSync(fmt.Sprintf("%v from %v", c.Name(), o.name), stop, c) {
				return
			}
		}
		close(synced)
		log.Infof("%v synced", o.name)
	}()
	return &join[T]{
		name:        o.name,
		synced:      synced,
		collections: cs,
		merge: func(ts []T) T {
			return ts[0]
		},
	}
}

func JoinCollectionOn[T any](merge func(ts []T) T, cs ...Collection[T]) Collection[T] {
	return &join[T]{collections: cs, merge: merge}
}
