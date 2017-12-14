// Copyright 2017 Istio Authors
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

package handler

import (
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/runtime2/config"
)

func Instantiate(current *Table, snapshot *config.Snapshot, env adapter.Env) *Table {
	t := &Table{
		entries: make(map[string]entry),
	}

	var f *factory

	// Find all handlers, as referenced by instances, and associate handlers.
	instancesByHandler := config.GetInstancesGroupedByHandlers(snapshot)
	for handler, instances := range instancesByHandler {
		signature := CalculateHandlerSignature(handler, instances)

		currentEntry, found := current.entries[handler.Name]
		if found && currentEntry.StartupError == nil && currentEntry.Signature.Equals(signature) {
			// reuse the handler
			t.entries[handler.Name] = currentEntry
			continue
		}

		// Instantiate the new handler
		if f == nil {
			f = newFactory(snapshot, env)
		}

		instantiatedHandler, err := f.build(handler, instances, nil)

		t.entries[handler.Name] = entry{
			Name:         handler.Name,
			Handler:      instantiatedHandler,
			StartupError: err,
			Signature:    signature,
		}
	}

	return t
}
