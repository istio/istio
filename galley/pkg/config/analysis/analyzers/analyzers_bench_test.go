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

package analyzers

import (
	"fmt"
	"testing"

	"istio.io/pkg/log"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	coll "istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/snapshots"
)

type context struct {
	set      *coll.Set
	messages diag.Messages
}

var _ analysis.Context = &context{}

// Report implements analysis.Context
func (ctx *context) Report(_ collection.Name, m diag.Message) {
	ctx.messages.Add(m)
}

// Find implements analysis.Context
func (ctx *context) Find(col collection.Name, name resource.FullName) *resource.Instance {
	c := ctx.set.Collection(col)
	if c == nil {
		return nil
	}
	return c.Get(name)
}

// Exists implements analysis.Context
func (ctx *context) Exists(col collection.Name, name resource.FullName) bool {
	return ctx.Find(col, name) != nil
}

// ForEach implements analysis.Context
func (ctx *context) ForEach(col collection.Name, fn analysis.IteratorFn) {
	c := ctx.set.Collection(col)
	if c == nil {
		return
	}
	c.ForEach(fn)
}

// Canceled implements analysis.Context
func (ctx *context) Canceled() bool {
	return false
}

type origin struct {
	friendlyName string
}

var _ resource.Origin = &origin{}
var _ resource.Reference = &reference{}

func (o origin) Namespace() resource.Namespace { return "" }
func (o origin) FriendlyName() string          { return o.friendlyName }
func (o origin) Reference() resource.Reference { return reference{name: ""} }

type reference struct {
	name string
}

func (r reference) String() string { return r.name }

// This is a very basic benchmark on unit test data, so it doesn't tell us anything about how an analyzer performs at scale
func BenchmarkAnalyzers(b *testing.B) {
	for _, tc := range testGrid {
		tc := tc // Capture range variable so subtests work correctly
		b.Run(tc.name+"-bench", func(b *testing.B) {
			sa, err := setupAnalyzerForCase(tc, nil)
			if err != nil {
				b.Fatalf("Error setting up analysis for benchmark on testcase %s: %v", tc.name, err)
			}

			b.ResetTimer()

			// Run the analysis
			_, err = runAnalyzer(sa)
			if err != nil {
				b.Fatalf("Error running analysis for benchmark on testcase %s: %v", tc.name, err)
			}
		})
	}
}

func BenchmarkAnalyzersArtificialBlankData100(b *testing.B) {
	benchmarkAnalyzersArtificialBlankData(100, b)
}

func BenchmarkAnalyzersArtificialBlankData200(b *testing.B) {
	benchmarkAnalyzersArtificialBlankData(200, b)
}

func BenchmarkAnalyzersArtificialBlankData400(b *testing.B) {
	benchmarkAnalyzersArtificialBlankData(400, b)
}

func BenchmarkAnalyzersArtificialBlankData800(b *testing.B) {
	benchmarkAnalyzersArtificialBlankData(800, b)
}

// Benchmark analyzers against an artificial set of blank data.
// This does not cover all scaling factors, and it's not representative of a realistic snapshot, but it does cover some things.
func benchmarkAnalyzersArtificialBlankData(count int, b *testing.B) {
	// Suppress log noise from validation warnings
	validationScope := log.Scopes()["validation"]
	oldLevel := validationScope.GetOutputLevel()
	validationScope.SetOutputLevel(log.ErrorLevel)
	defer validationScope.SetOutputLevel(oldLevel)

	// Get the set of collections that could actually get used
	m := schema.MustGet()
	isUsedCollection := make(map[string]bool)
	for _, col := range m.AllCollectionsInSnapshots(snapshots.SnapshotNames()) {
		isUsedCollection[col] = true
	}

	// Generate blank test data
	set := coll.NewSet(collections.All)
	collections.All.ForEach(func(s collection.Schema) bool {
		// Skip over collections that the Galley pipeline would always ignore
		if !isUsedCollection[s.Name().String()] {
			return false
		}

		for i := 0; i < count; i++ {
			name := resource.NewFullName("default", resource.LocalName(fmt.Sprintf("%s-%d", s.Name(), i)))
			r := &resource.Instance{
				Metadata: resource.Metadata{
					Schema:   s.Resource(),
					FullName: name,
				},
				Message: s.Resource().MustNewProtoInstance(),
				Origin:  &origin{friendlyName: name.String()},
			}
			set.Collection(s.Name()).Set(r)
		}

		return false
	})
	ctx := &context{set: set}

	b.ResetTimer()
	for _, a := range All() {
		b.Run(a.Metadata().Name+"-bench", func(b *testing.B) {
			a.Analyze(ctx)
		})
	}
}
