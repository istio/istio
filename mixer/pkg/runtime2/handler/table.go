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
	"crypto/sha1"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/runtime2/config"
)

var emptyTable = &Table{
	entries: []entry{},
}

type Table struct {
	entries []entry
}

func (table *Table) Get(handlerName string) adapter.Handler {
	// TODO
	return nil
}

type entry struct {
	// Name of the handler
	Name string

	// Handler is the initialized handler object.
	Handler adapter.Handler

	// sha is used to verify and update the handlerEntry.
	sha [sha1.Size]byte
}

func Empty() *Table {
	return emptyTable
}

func Instantiate(current *Table, snapshot *config.Snapshot) *Table {
	// TODO:
	return nil
}
