// Copyright Istio Authors
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
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	resource2 "istio.io/istio/pkg/config/schema/resource"
)

type analyzer struct {
	name   string
	inputs collection.Inputs
	ran    bool
}

// Metadata implements Analyzer
func (a *analyzer) Metadata() Metadata {
	return Metadata{
		Name:   a.name,
		Inputs: a.inputs,
	}
}

// Analyze implements Analyzer
func (a *analyzer) Analyze(Context) {
	a.ran = true
}

type context struct{}

func (ctx *context) Report(config.GroupVersionKind, diag.Message)                       {}
func (ctx *context) Find(config.GroupVersionKind, resource.FullName) *resource.Instance { return nil }
func (ctx *context) Exists(config.GroupVersionKind, resource.FullName) bool             { return false }
func (ctx *context) ForEach(config.GroupVersionKind, IteratorFn)                        {}
func (ctx *context) Canceled() bool                                                     { return false }

func TestCombinedAnalyzer(t *testing.T) {
	g := NewWithT(t)

	col1 := newSchema("col1")
	col2 := newSchema("col2")
	col3 := newSchema("col3")
	col4 := newSchema("col4")

	a1 := &analyzer{name: "a1", inputs: collection.Inputs{col1.Resource().GroupVersionKind()}}
	a2 := &analyzer{name: "a2", inputs: collection.Inputs{col2.Resource().GroupVersionKind()}}
	a3 := &analyzer{name: "a3", inputs: collection.Inputs{col3.Resource().GroupVersionKind()}}
	a4 := &analyzer{name: "a4", inputs: collection.Inputs{col4.Resource().GroupVersionKind()}}

	a := Combine("combined", a1, a2, a3, a4)
	g.Expect(a.Metadata().Inputs).To(ConsistOf(col1.Resource().GroupVersionKind(), col2.Resource().GroupVersionKind(), col3.Resource().GroupVersionKind(), col4.Resource().GroupVersionKind()))

	removed := a.RemoveSkipped(collection.NewSchemasBuilder().MustAdd(col1).MustAdd(col2).Build())

	g.Expect(removed).To(ConsistOf(a3.Metadata().Name, a4.Metadata().Name))
	g.Expect(a.Metadata().Inputs).To(ConsistOf(col1.Resource().GroupVersionKind(), col2.Resource().GroupVersionKind()))

	a.Analyze(&context{})

	g.Expect(a1.ran).To(BeTrue())
	g.Expect(a2.ran).To(BeTrue())
	g.Expect(a3.ran).To(BeFalse())
	g.Expect(a4.ran).To(BeFalse())
}

func newSchema(name string) collection.Schema {
	return collection.Builder{
		Resource: resource2.Builder{
			Kind:         name,
			Plural:       name + "s",
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.MustBuild(),
	}.MustBuild()
}
