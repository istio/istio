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
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/galley/pkg/config/schema/collection"
	"istio.io/istio/galley/pkg/config/scope"
)

// Analyzer is an interface for analyzing configuration.
type Analyzer interface {
	Metadata() Metadata
	Analyze(c Context)
}

// CombinedAnalyzer is a special Analyzer that combines multiple analyzers into one
type CombinedAnalyzer struct {
	metadata  Metadata
	analyzers []Analyzer
	disabled  map[collection.Name]struct{}
}

// Combine multiple analyzers into a single one.
// For input metadata, use the union of the component analyzers
func Combine(name string, analyzers ...Analyzer) *CombinedAnalyzer {
	return &CombinedAnalyzer{
		metadata: Metadata{
			Name:   name,
			Inputs: combineInputs(analyzers),
		},
		analyzers: analyzers,
	}
}

// Metadata implements Analyzer
func (c *CombinedAnalyzer) Metadata() Metadata {
	return c.metadata
}

// Analyze implements Analyzer
func (c *CombinedAnalyzer) Analyze(ctx Context) {
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

// WithDisabled returns a new CombinedAnalyzer that marks the specified input collections as disabled.
// Transformer information is used to determine, based on the disabled input collections, which output collections
// should be disabled. Any analyzers that require those output collections will be skipped.
func (c *CombinedAnalyzer) WithDisabled(disabledInputs collection.Names, xformProviders transformer.Providers) *CombinedAnalyzer {
	return &CombinedAnalyzer{
		metadata:  c.metadata,
		analyzers: c.analyzers,
		disabled:  getDisabledOutputs(disabledInputs, xformProviders),
	}
}

func getDisabledOutputs(disabledInputs collection.Names, xformProviders transformer.Providers) map[collection.Name]struct{} {
	// Get disabledCollections as a set
	disabledInputSet := make(map[collection.Name]struct{})
	for _, col := range disabledInputs {
		disabledInputSet[col] = struct{}{}
	}

	// Disable all outputs where every xform has at least one input disabled
	// 1. Count, for each output, how many xforms feed it
	outputXformCount := make(map[collection.Name]int)
	for _, p := range xformProviders {
		for _, out := range p.Outputs() {
			outputXformCount[out]++
		}
	}

	// 2. For each xform, if inputs are disabled decrement each output counter for that xform
	for _, p := range xformProviders {
		hasDisabledInput := false
		for _, in := range p.Inputs() {
			if _, ok := disabledInputSet[in]; ok {
				hasDisabledInput = true
				break
			}
		}
		if hasDisabledInput {
			for _, out := range p.Outputs() {
				outputXformCount[out]--
			}
		}
	}

	// 3. Any outputs == 0, add to the result set
	result := make(map[collection.Name]struct{})
	for out, count := range outputXformCount {
		if count == 0 {
			result[out] = struct{}{}
		}
	}

	return result
}
