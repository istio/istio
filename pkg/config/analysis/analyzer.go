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
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis/scope"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/util/sets"
)

// Analyzer is an interface for analyzing configuration.
type Analyzer interface {
	Metadata() Metadata
	Analyze(c Context)
}

// CombinedAnalyzer is a special Analyzer that combines multiple analyzers into one
type CombinedAnalyzer struct {
	name      string
	analyzers []Analyzer
}

// Combine multiple analyzers into a single one.
// For input metadata, use the union of the component analyzers
func Combine(name string, analyzers ...Analyzer) *CombinedAnalyzer {
	return &CombinedAnalyzer{
		name:      name,
		analyzers: analyzers,
	}
}

func (c *CombinedAnalyzer) RelevantSubset(kinds sets.Set[config.GroupVersionKind]) *CombinedAnalyzer {
	var selected []Analyzer
	for _, a := range c.analyzers {
		for _, inputKind := range a.Metadata().Inputs {
			if kinds.Contains(inputKind) {
				selected = append(selected, a)
				break
			}
		}
	}
	return Combine("subset", selected...)
}

// Metadata implements Analyzer
func (c *CombinedAnalyzer) Metadata() Metadata {
	return Metadata{
		Name:   c.name,
		Inputs: combineInputs(c.analyzers),
	}
}

// Analyze implements Analyzer
func (c *CombinedAnalyzer) Analyze(ctx Context) {
	for _, a := range c.analyzers {
		scope.Analysis.Debugf("Started analyzer %q...", a.Metadata().Name)
		if ctx.Canceled() {
			scope.Analysis.Debugf("Analyzer %q has been cancelled...", c.Metadata().Name)
			return
		}
		ctx.SetAnalyzer(a.Metadata().Name)
		a.Analyze(ctx)
		scope.Analysis.Debugf("Completed analyzer %q...", a.Metadata().Name)
	}
}

// RemoveSkipped removes analyzers that should be skipped, meaning they meet one of the following criteria:
// 1. The analyzer requires disabled input collections. The names of removed analyzers are returned.
// Transformer information is used to determine, based on the disabled input collections, which output collections
// should be disabled. Any analyzers that require those output collections will be removed.
// 2. The analyzer requires a collection not available in the current snapshot(s)
func (c *CombinedAnalyzer) RemoveSkipped(schemas collection.Schemas) []string {
	allSchemas := schemas.All()
	s := sets.NewWithLength[config.GroupVersionKind](len(allSchemas))
	for _, sc := range allSchemas {
		s.Insert(sc.GroupVersionKind())
	}

	var enabled []Analyzer
	var removedNames []string
mainloop:
	for _, a := range c.analyzers {
		for _, in := range a.Metadata().Inputs {
			if !s.Contains(in) {
				scope.Analysis.Infof("Skipping analyzer %q because collection %s is not in the snapshot(s).", a.Metadata().Name, in)
				removedNames = append(removedNames, a.Metadata().Name)
				continue mainloop
			}
		}

		enabled = append(enabled, a)
	}

	c.analyzers = enabled
	return removedNames
}

// AnalyzerNames returns the names of analyzers in this combined analyzer
func (c *CombinedAnalyzer) AnalyzerNames() []string {
	result := make([]string, 0, len(c.analyzers))
	for _, a := range c.analyzers {
		result = append(result, a.Metadata().Name)
	}
	return result
}

func combineInputs(analyzers []Analyzer) []config.GroupVersionKind {
	result := sets.NewWithLength[config.GroupVersionKind](len(analyzers))
	for _, a := range analyzers {
		result.InsertAll(a.Metadata().Inputs...)
	}
	return result.UnsortedList()
}
