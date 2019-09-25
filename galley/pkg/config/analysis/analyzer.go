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
	"istio.io/istio/galley/pkg/config/schema/collection"
	"istio.io/istio/galley/pkg/config/scope"
)

// Analyzer is an interface for analyzing configuration.
type Analyzer interface {
	Metadata() Metadata
	Analyze(c Context)
}

type combinedAnalyzers struct {
	metadata  Metadata
	analyzers []Analyzer
	disabled  map[collection.Name]struct{}
}

// CombinedAnalyzer is a special Analyzer that combines multiple analyzers into one
type CombinedAnalyzer interface {
	Analyzer

	// Disable marks the specified collections as disabled.
	// Any analyzers that require these collections as inputs will be skipped.
	Disable(collection.Names)
}

// Combine multiple analyzers into a single one.
// For input metadata, use the union of the component analyzers
func Combine(name string, analyzers ...Analyzer) CombinedAnalyzer {
	return &combinedAnalyzers{
		metadata: Metadata{
			Name:   name,
			Inputs: combineInputs(analyzers),
		},
		analyzers: analyzers,
	}
}

// Metadata implements Analyzer
func (c *combinedAnalyzers) Metadata() Metadata {
	return c.metadata
}

// Analyze implements Analyzer
func (c *combinedAnalyzers) Analyze(ctx Context) {
mainloop:
	for _, a := range c.analyzers {
		// Skip over any analyzers that require disabled input
		for _, in := range a.Metadata().Inputs {
			if _, ok := c.disabled[in]; ok {
				scope.Analysis.Debugf("Skipping analyzer %q because collection %s is disabled.", a.Metadata().Name, in)
				continue mainloop
			}
		}

		scope.Analysis.Debugf("Started analyzer %q...", a.Metadata().Name)
		if ctx.Canceled() {
			scope.Analysis.Debugf("Analyzer %q has been cancelled...", c.Metadata().Name)
			return
		}
		a.Analyze(ctx)
		scope.Analysis.Debugf("Completed analyzer %q...", a.Metadata().Name)
	}
}

func combineInputs(analyzers []Analyzer) collection.Names {
	result := make([]collection.Name, 0)
	for _, a := range analyzers {
		result = append(result, a.Metadata().Inputs...)
	}

	return result
}

// Disable implements CombinedAnalyzer
func (c *combinedAnalyzers) Disable(cols collection.Names) {
	disabled := make(map[collection.Name]struct{})
	for _, col := range cols {
		disabled[col] = struct{}{}
	}
	c.disabled = disabled
}
