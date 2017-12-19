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
)

// Table contains a set of instantiated and configured of adapter handlers.
type Table struct {
	entries map[string]entry
}

type entry struct {
	// name of the handler
	name string

	// handler is the initialized handler object.
	handler adapter.Handler

	// sha is used to verify and update the handlerEntry.
	signature signature

	// error that was received during startup.
	startupError error
}

// GetHealthyHandler returns a healthy handler instance with the given name, if it exists.
func (table *Table) GetHealthyHandler(handlerName string) (adapter.Handler, bool) {
	e, found := table.entries[handlerName]
	if !found || e.startupError != nil {
		return nil, false
	}

	return e.handler, true
}

var emptyTable = &Table{
	entries: make(map[string]entry, 0),
}

// Empty returns an empty table instance.
func Empty() *Table {
	return emptyTable
}
