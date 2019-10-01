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
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/schema/collection"
	"istio.io/istio/galley/pkg/config/testing/data"
)

type analyzer struct {
	inputs collection.Names
	ran    bool
}

// Metadata implements Analyzer
func (a *analyzer) Metadata() Metadata {
	return Metadata{
		Name:   "",
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

	a1 := &analyzer{inputs: collection.Names{data.Collection1}}
	a2 := &analyzer{inputs: collection.Names{data.Collection2}}

	a := Combine("combined", a1, a2)
	g.Expect(a.Metadata().Inputs).To(ConsistOf(data.Collection1, data.Collection2))

	a.Analyze(&context{})
	g.Expect(a1.ran).To(BeTrue())
	g.Expect(a2.ran).To(BeTrue())
}
