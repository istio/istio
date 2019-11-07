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

package cmd

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/pkg/collateral/metrics"
)

func TestGalleyCollateralFilterForMetrics(t *testing.T) {
	// Hi!
	// If you're breaking this test, then it means the test is detecting a metric that it doesn't recognize in Galley.
	// This can happen for either of two reasons:
	//
	// - You're adding a new, legitimate metric to Galley. If that is the case, please fix selectMetric function
	//   and this test.
	//
	// - You're adding a new metric to some non-Galley code. Due to library dependency hell, Galley ends up pulling the
	//   your metric. This is something we want to eliminate over time. In the meantime, here are a few ways to avoid
	//   this:
	//     - Do not introduce and/or eliminate the dependency, if possible. This is the best solution. In certain cases
	//    this can be accomplished by appropriate refactoring before introducing code.
	//     - Update the filtering code in selectMetric.
	//
	g := NewGomegaWithT(t)

	for _, m := range metrics.NewOpenCensusRegistry().ExportedMetrics() {
		if selectMetric(m) {
			g.Expect(m.Name).To(Or(HavePrefix("istio"), HavePrefix("galley")))
		}
	}
}
