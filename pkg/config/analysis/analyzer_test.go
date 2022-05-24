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

	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	resource2 "istio.io/istio/pkg/config/schema/resource"
)

type analyzer struct {
	name   string
	inputs collection.Names
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

func (ctx *context) Report(collection.Name, diag.Message)                       {}
func (ctx *context) Find(collection.Name, resource.FullName) *resource.Instance { return nil }
func (ctx *context) Exists(collection.Name, resource.FullName) bool             { return false }
func (ctx *context) ForEach(collection.Name, IteratorFn)                        {}
func (ctx *context) Canceled() bool                                             { return false }

func TestCombinedAnalyzer(t *testing.T) {
	g := NewWithT(t)

	col1 := newSchema("col1")
	col2 := newSchema("col2")
	col3 := newSchema("col3")
	col4 := newSchema("col4")

	a1 := &analyzer{name: "a1", inputs: collection.Names{col1.Name()}}
	a2 := &analyzer{name: "a2", inputs: collection.Names{col2.Name()}}
	a3 := &analyzer{name: "a3", inputs: collection.Names{col3.Name()}}
	a4 := &analyzer{name: "a4", inputs: collection.Names{col4.Name()}}

	a := Combine("combined", a1, a2, a3, a4)
	g.Expect(a.Metadata().Inputs).To(ConsistOf(col1.Name(), col2.Name(), col3.Name(), col4.Name()))

	avalableSchemas := collection.NewSchemasBuilder()
	avalableSchemas.Add(&testSchemaImpl{col1.Name()})
	avalableSchemas.Add(&testSchemaImpl{col2.Name()})

	removed := a.RemoveSkipped(avalableSchemas.Build())

	g.Expect(removed).To(ConsistOf(a3.Metadata().Name, a4.Metadata().Name))
	g.Expect(a.Metadata().Inputs).To(ConsistOf(col1.Name(), col2.Name()))

	a.Analyze(&context{})

	g.Expect(a1.ran).To(BeTrue())
	g.Expect(a2.ran).To(BeTrue())
	g.Expect(a3.ran).To(BeFalse())
	g.Expect(a4.ran).To(BeFalse())
}

func newSchema(name string) collection.Schema {
	return collection.Builder{
		Name: name,
		Resource: resource2.Builder{
			Kind:         name,
			Plural:       name + "s",
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.MustBuild(),
	}.MustBuild()
}

type testSchemaImpl struct {
	name collection.Name
}

// String interface method implementation.
func (s *testSchemaImpl) String() string {
	return string(s.Name())
}

func (s *testSchemaImpl) Name() collection.Name {
	return s.name
}

func (s *testSchemaImpl) VariableName() string {
	panic("implement me")
}

func (s *testSchemaImpl) Resource() resource2.Schema {
	panic("implement me")
}

func (s *testSchemaImpl) IsDisabled() bool {
	panic("implement me")
}

func (s *testSchemaImpl) Disable() collection.Schema {
	panic("implement me")
}

func (s *testSchemaImpl) Equal(o collection.Schema) bool {
	panic("implement me")
}
