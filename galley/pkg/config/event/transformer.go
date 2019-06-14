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

package event

import (
	"istio.io/istio/galley/pkg/config/collection"
)

// Transformer is a Processor that transforms input events from one or more collections to a set of output events to
// one or more collections.
//
// - A transformer must declare its inputs and outputs collections. via Inputs and Outputs methods. These must return
// idempotent results.
// - For every output collection that Transformer exposes, it must send a FullSync event, once the Transformer is
// started.
//
type Transformer interface {
	Processor

	// Select registers the given handler for a particular output collection.
	Select(c collection.Name, h Handler)

	// Inputs for this transformer
	Inputs() collection.Names

	// Outputs for this transformer
	Outputs() collection.Names
}
