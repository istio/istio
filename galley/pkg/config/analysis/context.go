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

package analysis

import (
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// IteratorFn is used to iterate over a set of collection entries. It must return true to keep iterating.
type IteratorFn func(r *resource.Entry) bool

// Context is an analysis context that is passed to individual analyzers.
type Context interface {
	// Report a diagnostic message against a resource Entry.
	Report(c collection.Name, r *resource.Entry, t diag.Template, params ...interface{})

	// Find a resource in the collection. If not found, nil is returned
	Find(c collection.Name, name resource.Name) *resource.Entry

	// ForEach iterates over all the entries of a given collection.
	ForEach(c collection.Name, fn func(r *resource.Entry) bool)
}
