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

package kclient

import (
	"fmt"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/slices"
)

type Index[K any, O controllers.ComparableObject] interface {
	Lookup(k K) []O
}

type index[K any, O controllers.ComparableObject] struct {
	RawIndexer
}

// Lookup finds all objects matching a given key
func (i index[K, O]) Lookup(k K) []O {
	if i.RawIndexer == nil {
		return nil
	}
	rk := any(k)
	tk, ok := rk.(string)
	if !ok {
		tk = rk.(fmt.Stringer).String()
	}
	res := i.RawIndexer.Lookup(tk)
	return slices.Map(res, func(e any) O {
		return e.(O)
	})
}

// CreateStringIndex creates a simple index, keyed by a string, over an informer for O. This is similar to
// Informer.AddIndex, but is easier to use and can be added after an informer has already started.
// This is split from CreateIndex because string does not implement fmt.Stringer.
//
// If an informer is filtered, the underlying index will still store all data. Items that do not match the filter
// are removed at Lookup() time.
// If the filter changes, there is no "notification" to the user of an Index, as there are no events for indexes.
func CreateStringIndex[O controllers.ComparableObject](
	client Informer[O],
	name string,
	extract func(o O) []string,
) Index[string, O] {
	return index[string, O]{client.Index(name, extract)}
}

// CreateIndex creates a simple index, keyed by key K, over an informer for O. This is similar to
// Informer.AddIndex, but is easier to use and can be added after an informer has already started.
// Keys can be any object, but they must encode down to a *unique* value with String().
//
// If an informer is filtered, the underlying index will still store all data. Items that do not match the filter
// are removed at Lookup() time.
// If the filter changes, there is no "notification" to the user of an Index, as there are no events for indexes.
func CreateIndex[K fmt.Stringer, O controllers.ComparableObject](
	client Informer[O],
	name string,
	extract func(o O) []K,
) Index[K, O] {
	x := client.Index(name, func(o O) []string {
		return slices.Map(extract(o), K.String)
	})

	return index[K, O]{x}
}
