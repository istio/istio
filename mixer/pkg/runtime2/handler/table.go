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

type Table struct {
	entries map[string]entry
}

type entry struct {
	// Name of the handler
	Name string

	// Handler is the initialized handler object.
	Handler adapter.Handler

	// sha is used to verify and update the handlerEntry.
	Signature HandlerSignature

	// error that was received during startup.
	StartupError error
}

func (table *Table) Get(handlerName string) adapter.Handler {
	// TODO
	return nil
}

var emptyTable = &Table{
	entries: make(map[string]entry, 0),
}

func Empty() *Table {
	return emptyTable
}
