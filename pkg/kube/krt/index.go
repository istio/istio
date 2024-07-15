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

	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/slices"
)

type Index[K comparable, O any] interface {
	Lookup(k K) []O
	objectHasKey(obj O, k K) bool
}

// NewNamespaceIndex is a small helper to index a collection by namespace
func NewNamespaceIndex[O Namespacer](c Collection[O]) Index[string, O] {
	return NewIndex(c, func(o O) []string {
		return []string{o.GetNamespace()}
	})
}

// NewIndex creates a simple index, keyed by key K, over an informer for O. This is similar to
// Informer.AddIndex, but is easier to use and can be added after an informer has already started.
func NewIndex[K comparable, O any](
	c Collection[O],
	extract func(o O) []K,
) Index[K, O] {
	idx := c.(internalCollection[O]).index(func(o O) []string {
		return slices.Map(extract(o), func(e K) string {
			return toString(e)
		})
	})

	return index[K, O]{idx, extract}
}

type index[K comparable, O any] struct {
	kclient.RawIndexer
	extract func(o O) []K
}

// nolint: unused // (not true)
func (i index[K, O]) objectHasKey(obj O, k K) bool {
	for _, got := range i.extract(obj) {
		if got == k {
			return true
		}
	}
	return false
}

// Lookup finds all objects matching a given key
func (i index[K, O]) Lookup(k K) []O {
	if i.RawIndexer == nil {
		return nil
	}
	res := i.RawIndexer.Lookup(toString(k))
	return slices.Map(res, func(e any) O {
		return e.(O)
	})
}

func toString(rk any) string {
	tk, ok := rk.(string)
	if !ok {
		return rk.(fmt.Stringer).String()
	}
	return tk
}
