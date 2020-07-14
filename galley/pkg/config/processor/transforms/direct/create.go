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

package direct

import (
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema"
)

// GetProviders creates a transformer provider for each direct transform in the metadata
func GetProviders(m *schema.Metadata) transformer.Providers {
	var result []transformer.Provider

	cols := m.AllCollections()

	for k, v := range m.DirectTransformSettings().Mapping() {
		from := cols.MustFind(k.String())
		to := cols.MustFind(v.String())

		handleFn := func(e event.Event, h event.Handler) {
			e = e.WithSource(to)
			h.Handle(e)
		}
		result = append(result, transformer.NewSimpleTransformerProvider(from, to, handleFn))
	}
	return result
}
