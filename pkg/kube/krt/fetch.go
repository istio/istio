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

import "istio.io/istio/pkg/slices"

func FetchOne[T any](ctx HandlerContext, c Collection[T], opts ...FetchOption) *T {
	res := Fetch[T](ctx, c, opts...)
	switch len(res) {
	case 0:
		return nil
	case 1:
		return &res[0]
	default:
		panic("FetchOne found for more than 1 item")
	}
}

func Fetch[T any](ctx HandlerContext, cc Collection[T], opts ...FetchOption) []T {
	h := ctx.(registerDependency)
	c := cc.(internalCollection[T])
	d := dependency{
		collection: eraseCollection(c),
	}
	for _, o := range opts {
		o(&d)
	}
	// Important: register before we List(), so we cannot miss any events
	h.registerDependency(d)

	// Now we can do the real fetching
	// Compute our list of all possible objects that can match. Then we will filter them later.
	// This pre-filtering upfront avoids extra work
	var list []T
	if d.filter.keys != nil {
		// If they fetch a set of keys, directly Get these. Usually this is a single resource.
		list = make([]T, 0, len(d.filter.keys))
		for k := range d.filter.keys {
			if i := c.GetKey(Key[T](k)); i != nil {
				list = append(list, *i)
			}
		}
	} else if d.filter.listFromIndex != nil {
		// Otherwise from an index; fetch from there. Often this is a list of a namespace
		list = d.filter.listFromIndex().([]T)
	} else {
		// Otherwise get everything
		list = c.List()
	}
	list = slices.FilterInPlace(list, func(i T) bool {
		o := c.augment(i)
		return d.filter.Matches(o, true)
	})
	if log.DebugEnabled() {
		log.WithLabels(
			"parent", h.name(),
			"fetch", c.name(),
			"filter", d.filter,
			"size", len(list),
		).Debugf("Fetch")
	}
	return list
}
