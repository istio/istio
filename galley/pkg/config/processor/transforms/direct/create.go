// Copyright 2019 Istio Authors
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

package direct

import (
	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/event"
)

// Create a new Direct transformer.
func Create(mapping map[collection.Name]collection.Name) []event.Transformer {
	var result []event.Transformer

	for k, v := range mapping {
		xform := event.NewFnTransform(
			collection.Names{k},
			collection.Names{v},
			nil,
			nil,
			func(e event.Event, h event.Handler) {
				e = e.WithSource(v)
				h.Handle(e)
			})
		result = append(result, xform)
	}
	return result
}
