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

import (
	"fmt"
	"time"

	"golang.org/x/time/rate"
)

const (
	// RateLimitEvaluatorName is the canonical name of the RateLimitEvaluator.
	RateLimitEvaluatorName = "RateLimit"
)

var (
	_ LoadEvaluator = &RateLimitEvaluator{}
)

// RateLimitEvaluator enforces a configured rate limit, using a rate.Limiter.
type RateLimitEvaluator struct {
	limiter *rate.Limiter
}

// NewRateLimitEvaluator builds a new RateLimitEvaluator.
func NewRateLimitEvaluator(limit rate.Limit, burstSize int) *RateLimitEvaluator {
	return &RateLimitEvaluator{
		limiter: rate.NewLimiter(limit, burstSize),
	}
}

// Name implements the LoadEvaluator interface.
func (r RateLimitEvaluator) Name() string {
	return RateLimitEvaluatorName
}

// EvaluateAgainst implements the LoadEvaluator interface.
func (r *RateLimitEvaluator) EvaluateAgainst(ri RequestInfo, threshold float64) LoadEvaluation {
	if r.limiter.AllowN(time.Now(), int(ri.PredictedCost)) {
		return LoadEvaluation{Status: BelowThreshold}
	}
	return LoadEvaluation{
		Status: ExceedsThreshold,
		Message: fmt.Sprintf(
			"Too many requests have been received by this server in the current time window (limit: %f). Please retry request later.", r.limiter.Limit()),
	}
}
