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
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/galley/pkg/config/resource"
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
func (a *analyzer) Analyze(ctx Context) {
	a.ran = true
}

type context struct{}

func (ctx *context) Report(c collection.Name, t diag.Message)                   {}
func (ctx *context) Find(c collection.Name, name resource.Name) *resource.Entry { return nil }
func (ctx *context) Exists(c collection.Name, name resource.Name) bool          { return false }
func (ctx *context) ForEach(c collection.Name, fn IteratorFn)                   {}
func (ctx *context) Canceled() bool                                             { return false }

func TestCombinedAnalyzer(t *testing.T) {
	g := NewGomegaWithT(t)

	col1 := collection.NewName("col1")
	col2 := collection.NewName("col2")
	col3 := collection.NewName("col3")
	col4 := collection.NewName("col4")

	a1 := &analyzer{name: "a1", inputs: collection.Names{col1}}
	a2 := &analyzer{name: "a2", inputs: collection.Names{col2}}
	a3 := &analyzer{name: "a3", inputs: collection.Names{col3}}
	a4 := &analyzer{name: "a4", inputs: collection.Names{col4}}

	xform := transformer.NewSimpleTransformerProvider(col3, col3, func(_ event.Event, _ event.Handler) {})

	a := Combine("combined", a1, a2, a3, a4)
	g.Expect(a.Metadata().Inputs).To(ConsistOf(col1, col2, col3, col4))

	removed := a.RemoveSkipped(
		collection.Names{col1, col2, col3},
		collection.Names{col3},
		transformer.Providers{xform})

	g.Expect(removed).To(ConsistOf(a3.Metadata().Name, a4.Metadata().Name))
	g.Expect(a.Metadata().Inputs).To(ConsistOf(col1, col2))

	a.Analyze(&context{})

	g.Expect(a1.ran).To(BeTrue())
	g.Expect(a2.ran).To(BeTrue())
	g.Expect(a3.ran).To(BeFalse())
	g.Expect(a4.ran).To(BeFalse())
}

func TestGetDisabledOutputs(t *testing.T) {
	g := NewGomegaWithT(t)

	in1 := collection.NewName("in1")
	in2 := collection.NewName("in2")
	in3 := collection.NewName("in3")
	in4 := collection.NewName("in4")
	in5 := collection.NewName("in5")
	out1 := collection.NewName("out1")
	out2 := collection.NewName("out2")
	out3 := collection.NewName("out3")
	out4 := collection.NewName("out4")

	blankFn := func(_ processing.ProcessorOptions) event.Transformer {
		return event.NewFnTransform(collection.Names{}, collection.Names{}, func() {}, func() {}, func(e event.Event, handler event.Handler) {})
	}

	xformProviders := transformer.Providers{
		transformer.NewProvider(collection.Names{in1}, collection.Names{out1, out2}, blankFn),
		transformer.NewProvider(collection.Names{in2}, collection.Names{out3}, blankFn),
		transformer.NewProvider(collection.Names{in3}, collection.Names{out3}, blankFn),
		transformer.NewProvider(collection.Names{in4, in5}, collection.Names{out4}, blankFn),
	}

	expectCollections(g, getDisabledOutputs(collection.Names{in1}, xformProviders), collection.Names{out1, out2})
	expectCollections(g, getDisabledOutputs(collection.Names{in2}, xformProviders), collection.Names{})
	expectCollections(g, getDisabledOutputs(collection.Names{in2, in3}, xformProviders), collection.Names{out3})
	expectCollections(g, getDisabledOutputs(collection.Names{in4}, xformProviders), collection.Names{out4})
}

func expectCollections(g *GomegaWithT, actualSet map[collection.Name]struct{}, expectedCols collection.Names) {
	g.Expect(actualSet).To(HaveLen(len(expectedCols)))
	for _, col := range expectedCols {
		g.Expect(actualSet).To(HaveKey(col))
	}
}
