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

package fixtures

import (
	"github.com/gogo/protobuf/proto"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

type testOrigin string

func (o testOrigin) FriendlyName() string {
	return string(o)
}

//Context is a context object fixture for testing
type Context struct {
	Collections map[collection.Name][]*resource.Entry
	Reports     map[collection.Name][]diag.Message
}

var _ analysis.Context = &Context{}

// Report implements analysis.Context
func (ctx *Context) Report(c collection.Name, t diag.Message) {
	if _, ok := ctx.Reports[c]; !ok {
		ctx.Reports[c] = make([]diag.Message, 0)
	}
	ctx.Reports[c] = append(ctx.Reports[c], t)
}

// Find implements analysis.Context
func (ctx *Context) Find(c collection.Name, name resource.Name) *resource.Entry {
	for _, r := range ctx.Collections[c] {
		if r.Metadata.Name == name {
			return r
		}
	}
	return nil
}

// Exists implements analysis.Context
func (ctx *Context) Exists(c collection.Name, name resource.Name) bool {
	return ctx.Find(c, name) != nil
}

// ForEach implements analysis.Context
func (ctx *Context) ForEach(c collection.Name, fn analysis.IteratorFn) {
	for _, r := range ctx.Collections[c] {
		if !fn(r) {
			break
		}
	}
}

// Canceled implements analysis.Context
func (ctx *Context) Canceled() bool {
	return false
}

// AddEntry adds a test data entry
func (ctx *Context) AddEntry(c collection.Name, n resource.Name, i proto.Message) {
	if _, ok := ctx.Collections[c]; !ok {
		ctx.Collections[c] = make([]*resource.Entry, 0)
	}

	r := &resource.Entry{
		Metadata: resource.Metadata{
			Name: n,
		},
		Item:   i,
		Origin: testOrigin(n.String()),
	}
	ctx.Collections[c] = append(ctx.Collections[c], r)
}

// NewContext creates a new context fixture
func NewContext() *Context {
	return &Context{
		Collections: make(map[collection.Name][]*resource.Entry),
		Reports:     make(map[collection.Name][]diag.Message),
	}
}
