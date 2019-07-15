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

import "istio.io/pkg/log"

var scope = log.RegisterScope("analysis", "", 0)

// Analyzer is an interface for analyzing configuration.
type Analyzer interface {
	Name() string
	Analyze(c Context)
}

// Analyzers is a slice of Analyzers
type Analyzers []Analyzer

// Analyze implements Analyzer.Analyze
func (a Analyzers) Analyze(c Context) {
	for _, an := range a {
		scope.Debugf("Started analyzer %q...", an.Name())
		an.Analyze(c)
		scope.Debugf("Completed analyzer %q...", an.Name())
	}
}
