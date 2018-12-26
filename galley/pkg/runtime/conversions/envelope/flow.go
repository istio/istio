//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package envelope

import (
	"istio.io/istio/galley/pkg/runtime/flow"
	"istio.io/istio/galley/pkg/runtime/resource"
)

// AddDirectEnvelopePipeline adds a new pipeline for converting an incoming proto, directly into enveloped
// form, ready for snapshotting.
func AddDirectEnvelopePipeline(t resource.TypeURL, b *flow.PipelineBuilder) {

	// Collection to store eagerly enveloped resources
	c := flow.NewTable()

	// Add an accumulator that will convert events and apply it to a collection
	a := flow.NewAccumulator(c, doEnvelope)

	// Direct the events for the given type URL to the accumulator
	b.AddHandler(t, a)

	// Create a view on the collection that will directly interpret data as envelopes.
	v := flow.NewTableView(t, c, nil)

	// register the view for snapshotting.
	b.AddView(v)
}

// doEnvelope the incoming entry
func doEnvelope(entry resource.Entry) (interface{}, error) {
	return resource.Envelope(entry)
}
