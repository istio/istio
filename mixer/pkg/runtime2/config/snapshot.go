// Copyright 2018 Istio Authors
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

package config

import (
	"github.com/gogo/protobuf/proto"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/template"
)

type (

	// Snapshot view of configuration.
	Snapshot struct {
		ID int64

		// Static information
		Templates map[string]*template.Info
		Adapters  map[string]*adapter.Info

		// Config store based information
		Attributes expr.AttributeDescriptorFinder

		Handlers  map[string]*Handler
		Instances map[string]*Instance
		Rules     []*Rule

		// Perf Counters relevant to configuration.
		Counters Counters
	}

	// Handler configuration. Fully resolved.
	Handler struct {

		// Name of the Handler. Fully qualified.
		Name string

		// Associated adapter. Always resolved.
		Adapter *adapter.Info

		// parameters used to construct the Handler.
		Params proto.Message
	}

	// Instance configuration. Fully resolved.
	Instance struct {
		// Name of the instance. Fully qualified.
		Name string

		// Associated template. Always resolved.
		Template *template.Info

		// parameters used to construct the instance.
		Params proto.Message
	}

	// Rule configuration. Fully resolved.
	Rule struct {
		// Name of the rule
		Name string

		// Namespace of the rule
		Namespace string

		// Match condition
		Match string

		Actions []*Action

		ResourceType ResourceType
	}

	// Action configuration. Fully resolved.
	Action struct {
		// Handler that this action is resolved to.
		Handler *Handler

		// Instances that should be generated as part of invoking action.
		Instances []*Instance
	}
)

// Empty returns a new, empty configuration snapshot.
func Empty() *Snapshot {
	return &Snapshot{
		ID:       -1,
		Rules:    []*Rule{},
		Counters: newCounters(-1),
	}
}
