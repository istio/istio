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

package multicluster

import "istio.io/istio/pkg/config/analysis"

func AllMultiCluster() []analysis.Analyzer {
	analyzers := []analysis.Analyzer{
		&ServiceAnalyzer{},
	}
	return analyzers
}

// AllMultiClusterCombined returns all multi-cluster analyzers combined as one
func AllMultiClusterCombined() analysis.CombinedAnalyzer {
	return analysis.Combine("all-multi-cluster", AllMultiCluster()...)
}
