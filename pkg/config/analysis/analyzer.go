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
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/galley/pkg/config/scope"
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

// RemoveSkipped removes analyzers that should be skipped, meaning they meet one of the following criteria:
// 1. The analyzer requires disabled input collections. The names of removed analyzers are returned.
// Transformer information is used to determine, based on the disabled input collections, which output collections
// should be disabled. Any analyzers that require those output collections will be removed.
// 2. The analyzer requires a collection not available in the current snapshot(s)
func (c *CombinedAnalyzer) RemoveSkipped(colsInSnapshots, disabledInputs collection.Names, xformProviders transformer.Providers) []string {
	disabledOutputs := getDisabledOutputs(disabledInputs, xformProviders)
	var enabled []Analyzer
	var removedNames []string

	snapshotCols := make(map[collection.Name]bool)
	for _, col := range colsInSnapshots {
		snapshotCols[col] = true
	}

mainloop:
	for _, a := range c.analyzers {
		for _, in := range a.Metadata().Inputs {
			// Skip over any analyzers that require disabled input
			if _, ok := disabledOutputs[in]; ok {
				scope.Analysis.Infof("Skipping analyzer %q because collection %s is disabled.", a.Metadata().Name, in)
				removedNames = append(removedNames, a.Metadata().Name)
				continue mainloop
			}

			// Skip over any analyzers needing collections not in the snapshot(s)
			if _, ok := snapshotCols[in]; !ok {
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
		p.Outputs().ForEach(func(out collection.Schema) (done bool) {
			outputXformCount[out.Name()]++
			return
		})
	}

	// 2. For each xform, if inputs are disabled decrement each output counter for that xform
	for _, p := range xformProviders {
		hasDisabledInput := false
		p.Inputs().ForEach(func(in collection.Schema) (done bool) {
			if _, ok := disabledInputSet[in.Name()]; ok {
				hasDisabledInput = true
				return true
			}
			return
		})
		if hasDisabledInput {
			p.Outputs().ForEach(func(out collection.Schema) (done bool) {
				outputXformCount[out.Name()]--
				return
			})
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
