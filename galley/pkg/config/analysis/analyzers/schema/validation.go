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
package schema

import (
	"fmt"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schemas"
)

// ValidationAnalyzer runs schema validation as an analyzer and reports any violations as messages
type ValidationAnalyzer struct {
	s schema.Instance
}

var _ analysis.Analyzer = &ValidationAnalyzer{}

// AllValidationAnalyzers returns a slice with a validation analyzer for each Istio schema
// This automation comes with an assumption: that the collection names used by the schema match the metadata used by Galley components
func AllValidationAnalyzers() []analysis.Analyzer {
	result := make([]analysis.Analyzer, 0)
	for _, s := range schemas.Istio {
		// Skip synthetic service entries
		// TODO(https://github.com/istio/istio/issues/17949)
		if s.VariableName == schemas.SyntheticServiceEntry.VariableName {
			continue
		}
		result = append(result, &ValidationAnalyzer{s: s})
	}
	return result
}

// Metadata implements Analyzer
func (a *ValidationAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:   fmt.Sprintf("schema.ValidationAnalyzer.%s", a.s.VariableName),
		Inputs: collection.Names{collection.NewName(a.s.Collection)},
	}
}

// Analyze implements Analyzer
func (a *ValidationAnalyzer) Analyze(ctx analysis.Context) {
	c := collection.NewName(a.s.Collection)

	ctx.ForEach(c, func(r *resource.Entry) bool {
		name, ns := r.Metadata.Name.InterpretAsNamespaceAndName()

		err := a.s.Validate(name, ns, r.Item)
		if err != nil {
			ctx.Report(c, msg.NewSchemaValidationError(r, err))
		}

		return true
	})

}
