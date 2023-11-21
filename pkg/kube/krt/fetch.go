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
)

func FetchOne[T any](ctx HandlerContext, c Collection[T], opts ...FetchOption) *T {
	res := Fetch[T](ctx, c, opts...)
	switch len(res) {
	case 0:
		return nil
	case 1:
		return &res[0]
	default:
		panic(fmt.Sprintf("FetchOne found for more than 1 item"))
	}
}

func Fetch[T any](ctx HandlerContext, c Collection[T], opts ...FetchOption) []T {
	// First, set up the dependency. On first run, this will be new.
	// One subsequent runs, we just validate
	h := ctx.(registerDependency)
	d := dependency{
		collection: eraseCollection(c),
	}
	for _, o := range opts {
		o(&d)
	}
	h.registerDependency(d)

	// Now we can do the real fetching
	var res []T
	for _, c := range c.List(d.filter.namespace) {
		c := c
		if d.filter.Matches(c) {
			res = append(res, c)
		}
	}
	if log.DebugEnabled() {
		log.WithLabels(
			"from", h.Name(),
			"for", c.Name(),
			"filter", d.filter,
			"size", len(res),
		).Debugf("Fetch")
	}
	return res
}
