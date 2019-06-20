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
	"istio.io/pkg/log"

	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/event"
)

var scope = log.RegisterScope("processing", "", 0)

type xformer struct {
	source      collection.Name
	destination collection.Name
	handler     event.Handler
}

var _ event.Transformer = &xformer{}

// Create a new Direct transformer.
func Create(mapping map[collection.Name]collection.Name) []event.Transformer {
	var result []event.Transformer

	for k, v := range mapping {
		xform := &xformer{
			source:      k,
			destination: v,
		}
		result = append(result, xform)
	}
	return result
}

// Inputs implements processing.Transformer
func (x *xformer) Inputs() collection.Names {
	return []collection.Name{x.source}
}

// Outputs implements processing.Transformer
func (x *xformer) Outputs() collection.Names {
	return []collection.Name{x.destination}
}

// Start implements processing.Transformer
func (x *xformer) Start(_ interface{}) {}

// Stop implements processing.Transformer
func (x *xformer) Stop() {}

// Select implements processing.Transformer
func (x *xformer) Select(c collection.Name, h event.Handler) {
	if c == x.destination {
		x.handler = event.CombineHandlers(x.handler, h)
	}
}

// Handle implements processing.Transformer
func (x *xformer) Handle(e event.Event) {
	if x.handler == nil {
		return
	}

	switch e.Kind {
	case event.Added, event.Updated, event.Deleted, event.FullSync:
		if e.Source != x.source {
			scope.Warnf("direct.xformer.Handle: Unexpected event: %v", e)
			return
		}
		e.Source = x.destination
		fallthrough

	case event.Reset:
		x.handler.Handle(e)

	default:
		scope.Warnf("direct.xformer.Handle: Unexpected event: %v", e)
	}
}
