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

import "istio.io/istio/galley/pkg/config/scope"

// Analyzer is an interface for analyzing configuration.
type Analyzer interface {
	Name() string
	Analyze(c Context)
}

type combinedAnalyzers struct {
	name      string
	analyzers []Analyzer
}

// Combine multiple analyzers into a single one.
func Combine(name string, analyzers ...Analyzer) Analyzer {
	return &combinedAnalyzers{
		name:      name,
		analyzers: analyzers,
	}
}

// Name implements Analyzer
func (c *combinedAnalyzers) Name() string {
	return c.name
}

// Analyze implements Analyzer
func (c *combinedAnalyzers) Analyze(ctx Context) {
	for _, a := range c.analyzers {
		scope.Analysis.Debugf("Started analyzer %q...", a.Name())
		if ctx.Canceled() {
			scope.Analysis.Debugf("Analyzer %q has been cancelled...", c.Name())
			return
		}
		a.Analyze(ctx)
		scope.Analysis.Debugf("Completed analyzer %q...", a.Name())
	}
}
