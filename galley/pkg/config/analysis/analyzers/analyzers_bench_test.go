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

package analyzers

import (
	"fmt"
	"testing"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	coll "istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/schema"
	"istio.io/istio/galley/pkg/config/schema/collection"
	"istio.io/istio/galley/pkg/config/schema/collections"
	"istio.io/istio/galley/pkg/config/schema/snapshots"
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

func (o origin) Namespace() resource.Namespace { return "" }
func (o origin) FriendlyName() string          { return o.friendlyName }

// Note that what gets measured here is both the input pipeline (reading in YAML files, turning it into a snapshot) and the analysis.
// This also doesn't tell us anything about how an analyzer performs at scale, since we're just looking at unit test data.
func BenchmarkAnalyzers(b *testing.B) {
	for _, tc := range testGrid {
		tc := tc // Capture range variable so subtests work correctly
		b.Run(tc.name+"-bench", func(b *testing.B) {
			_, err := setupAndRunCase(tc, nil)
			if err != nil {
				b.Fatalf("Error running benchmark on testcase %s: %v", tc.name, err)
			}
		})
	}
}

// Benchmark analyzers against an artificial set of blank data.
// This does not cover all scaling factors, and it's not representative of a realistic snapshot, but it does cover some things.
func BenchmarkAnalyzersArtificialBlankData(b *testing.B) {
	// TODO: Fix log spam

	// Get the set of collections that could actually get used
	m := schema.MustGet()
	isUsedCollection := make(map[string]bool)
	for _, col := range m.AllCollectionsInSnapshots(snapshots.SnapshotNames()) {
		isUsedCollection[col] = true
	}

	set := coll.NewSet(collections.All)
	collections.All.ForEach(func(s collection.Schema) bool {
		// Skip over collections that the Galley pipeline would always ignore
		if !isUsedCollection[s.Name().String()] {
			return false
		}

		for i := 0; i < 100; i++ { // TODO: Configurable scaling factor
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
	AllCombined().Analyze(ctx)
}
