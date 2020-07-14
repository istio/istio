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

	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/pkg/config/event"
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
	g := NewGomegaWithT(t)

	col1 := newSchema("col1")
	col2 := newSchema("col2")
	col3 := newSchema("col3")
	col4 := newSchema("col4")

	a1 := &analyzer{name: "a1", inputs: collection.Names{col1.Name()}}
	a2 := &analyzer{name: "a2", inputs: collection.Names{col2.Name()}}
	a3 := &analyzer{name: "a3", inputs: collection.Names{col3.Name()}}
	a4 := &analyzer{name: "a4", inputs: collection.Names{col4.Name()}}

	xform := transformer.NewSimpleTransformerProvider(col3, col3, func(_ event.Event, _ event.Handler) {})

	a := Combine("combined", a1, a2, a3, a4)
	g.Expect(a.Metadata().Inputs).To(ConsistOf(col1.Name(), col2.Name(), col3.Name(), col4.Name()))

	removed := a.RemoveSkipped(
		collection.Names{col1.Name(), col2.Name(), col3.Name()},
		collection.Names{col3.Name()},
		transformer.Providers{xform})

	g.Expect(removed).To(ConsistOf(a3.Metadata().Name, a4.Metadata().Name))
	g.Expect(a.Metadata().Inputs).To(ConsistOf(col1.Name(), col2.Name()))

	a.Analyze(&context{})

	g.Expect(a1.ran).To(BeTrue())
	g.Expect(a2.ran).To(BeTrue())
	g.Expect(a3.ran).To(BeFalse())
	g.Expect(a4.ran).To(BeFalse())
}

func TestGetDisabledOutputs(t *testing.T) {
	g := NewGomegaWithT(t)

	in1 := newSchema("in1")
	in2 := newSchema("in2")
	in3 := newSchema("in3")
	in4 := newSchema("in4")
	in5 := newSchema("in5")
	out1 := newSchema("out1")
	out2 := newSchema("out2")
	out3 := newSchema("out3")
	out4 := newSchema("out4")

	blankFn := func(_ processing.ProcessorOptions) event.Transformer {
		return event.NewFnTransform(collection.SchemasFor(), collection.SchemasFor(), func() {}, func() {}, func(e event.Event, handler event.Handler) {})
	}

	xformProviders := transformer.Providers{
		transformer.NewProvider(collection.SchemasFor(in1), collection.SchemasFor(out1, out2), blankFn),
		transformer.NewProvider(collection.SchemasFor(in2), collection.SchemasFor(out3), blankFn),
		transformer.NewProvider(collection.SchemasFor(in3), collection.SchemasFor(out3), blankFn),
		transformer.NewProvider(collection.SchemasFor(in4, in5), collection.SchemasFor(out4), blankFn),
	}

	expectCollections(g, getDisabledOutputs(collection.Names{in1.Name()}, xformProviders), collection.Names{out1.Name(), out2.Name()})
	expectCollections(g, getDisabledOutputs(collection.Names{in2.Name()}, xformProviders), collection.Names{})
	expectCollections(g, getDisabledOutputs(collection.Names{in2.Name(), in3.Name()}, xformProviders), collection.Names{out3.Name()})
	expectCollections(g, getDisabledOutputs(collection.Names{in4.Name()}, xformProviders), collection.Names{out4.Name()})
}

func expectCollections(g *GomegaWithT, actualSet map[collection.Name]struct{}, expectedCols collection.Names) {
	g.Expect(actualSet).To(HaveLen(len(expectedCols)))
	for _, col := range expectedCols {
		g.Expect(actualSet).To(HaveKey(col))
	}
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
