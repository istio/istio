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
	var res []T
	for _, i := range c.List(d.filter.namespace) {
		i := i
		o := c.augment(i)
		if d.filter.Matches(o) {
			res = append(res, i)
		}
	}
	if log.DebugEnabled() {
		log.WithLabels(
			"parent", h.name(),
			"fetch", c.name(),
			"filter", d.filter,
			"size", len(res),
		).Debugf("Fetch")
	}
	return res
}
