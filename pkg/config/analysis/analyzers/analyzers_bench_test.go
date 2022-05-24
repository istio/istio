/*
 Copyright Istio Authors

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package analyzers

import (
	"fmt"
	"testing"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis/local"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/pkg/log"
)

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

	// Generate blank test data
	store := memory.MakeSkipValidation(collections.All)
	collections.All.ForEach(func(s collection.Schema) bool {
		for i := 0; i < count; i++ {
			name := resource.NewFullName("default", resource.LocalName(fmt.Sprintf("%s-%d", s.Name(), i)))
			_, _ = store.Create(config.Config{
				Meta: config.Meta{
					GroupVersionKind: s.Resource().GroupVersionKind(),
					Name:             name.Name.String(),
					Namespace:        name.Namespace.String(),
				},
				Spec: s.Resource().MustNewInstance(),
			})
		}

		return false
	})
	ctx := local.NewContext(store, make(chan struct{}), func(name collection.Name) {})

	b.ResetTimer()
	for _, a := range All() {
		b.Run(a.Metadata().Name+"-bench", func(b *testing.B) {
			a.Analyze(ctx)
		})
	}
}
