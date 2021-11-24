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
	"istio.io/istio/pkg/config/analysis/scope"
	"istio.io/istio/pkg/config/schema/collection"
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
		a.Analyze(ctx)
		scope.Analysis.Debugf("Completed analyzer %q...", a.Metadata().Name)
	}
}

// AnalyzerNames returns the names of analyzers in this combined analyzer
func (c *CombinedAnalyzer) AnalyzerNames() []string {
	var result []string
	for _, a := range c.analyzers {
		result = append(result, a.Metadata().Name)
	}
	return result
}

func combineInputs(analyzers []Analyzer) collection.Names {
	result := make([]collection.Name, 0)
	for _, a := range analyzers {
		result = append(result, a.Metadata().Inputs...)
	}

	return result
}
