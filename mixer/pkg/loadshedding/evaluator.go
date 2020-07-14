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

package loadshedding

// LoadEvaluator evaluates the current request against a threshold. LoadEvaluators
// may calculate load on an on-going basis or through some external mechanism.
type LoadEvaluator interface {
	// Name returns a canonical name for the LoadEvaluator.
	Name() string

	// EvaluateAgainst compares the current request and known load against the supplied
	// threshold to determine if the threshold is/will be exceeded.
	EvaluateAgainst(ri RequestInfo, threshold float64) LoadEvaluation
}

// LoadEvaluation holds the result of the evaluation of a current request against a threshold.
type LoadEvaluation struct {
	// Status indicates whether or not the threshold was exceeded.
	Status LoadStatus

	// Message enables LoadEvaluators to provide custom error messages when the threshold is
	// exceeded.
	Message string
}

// ThresholdExceeded determines if a load evaluation status is `ExceedsThreshold`.
func ThresholdExceeded(eval LoadEvaluation) bool {
	return eval.Status == ExceedsThreshold
}

// LoadStatus records the determination of a LoadEvaluation.
type LoadStatus int

const (
	// BelowThreshold indicates that a given request will not exceed a threshold.
	BelowThreshold LoadStatus = iota
	// ExceedsThreshold indicates that a given request will exceed a threshold.
	ExceedsThreshold
)
