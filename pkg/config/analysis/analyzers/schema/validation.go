// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package schema

import (
	"fmt"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/validation"
)

// ValidationAnalyzer runs schema validation as an analyzer and reports any violations as messages
type ValidationAnalyzer struct {
	s collection.Schema
}

var _ analysis.Analyzer = &ValidationAnalyzer{}

func CollectionValidationAnalyzer(s collection.Schema) analysis.Analyzer {
	return &ValidationAnalyzer{s: s}
}

// AllValidationAnalyzers returns a slice with a validation analyzer for each Istio schema
// This automation comes with an assumption: that the collection names used by the schema match the metadata used by Galley components
func AllValidationAnalyzers() []analysis.Analyzer {
	result := make([]analysis.Analyzer, 0)
	collections.Istio.ForEach(func(s collection.Schema) (done bool) {
		result = append(result, &ValidationAnalyzer{s: s})
		return
	})
	return result
}

// Metadata implements Analyzer
func (a *ValidationAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        fmt.Sprintf("schema.ValidationAnalyzer.%s", a.s.Resource().Kind()),
		Description: fmt.Sprintf("Runs schema validation as an analyzer on '%s' resources", a.s.Resource().Kind()),
		Inputs:      collection.Names{a.s.Name()},
	}
}

// Analyze implements Analyzer
func (a *ValidationAnalyzer) Analyze(ctx analysis.Context) {
	c := a.s.Name()

	ctx.ForEach(c, func(r *resource.Instance) bool {
		ns := r.Metadata.FullName.Namespace
		name := r.Metadata.FullName.Name

		warnings, err := a.s.Resource().ValidateConfig(config.Config{
			Meta: config.Meta{
				Name:      string(name),
				Namespace: string(ns),
			},
			Spec: r.Message,
		})
		if err != nil {
			if multiErr, ok := err.(*multierror.Error); ok {
				for _, err := range multiErr.WrappedErrors() {
					ctx.Report(c, morePreciseMessage(r, err, true))
				}
			} else {
				ctx.Report(c, morePreciseMessage(r, err, true))
			}
		}
		if warnings != nil {
			if multiErr, ok := warnings.(*multierror.Error); ok {
				for _, err := range multiErr.WrappedErrors() {
					ctx.Report(c, morePreciseMessage(r, err, false))
				}
			} else {
				ctx.Report(c, morePreciseMessage(r, warnings, false))
			}
		}

		return true
	})
}

func morePreciseMessage(r *resource.Instance, err error, isError bool) diag.Message {
	if aae, ok := err.(*validation.AnalysisAwareError); ok {
		switch aae.Type {
		case "VirtualServiceUnreachableRule":
			return msg.NewVirtualServiceUnreachableRule(r, aae.Parameters[0].(string), aae.Parameters[1].(string))
		case "VirtualServiceIneffectiveMatch":
			return msg.NewVirtualServiceIneffectiveMatch(r, aae.Parameters[0].(string), aae.Parameters[1].(string), aae.Parameters[2].(string))
		}
	}
	if !isError {
		return msg.NewSchemaWarning(r, err)
	}
	return msg.NewSchemaValidationError(r, err)
}
