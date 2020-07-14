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

package loadshedding_test

import (
	"testing"
	"time"

	"golang.org/x/time/rate"

	"istio.io/istio/mixer/pkg/loadshedding"
)

var (
	pc1  = loadshedding.RequestInfo{PredictedCost: 1}
	pc11 = loadshedding.RequestInfo{PredictedCost: 11}

	rateLimitThreshold = 0.1 // this value is not used by the evaluator
)

// this test is not meant to exercise fully the underlying
// `rate.Limiter`. Rather, it is meant to exercise the basic
// creation mechanism and simple boundary conditions.
func TestEvaluateAgainst_RateLimit(t *testing.T) {
	// with no limit (rate.Inf), everything should be allowed.
	e := loadshedding.NewRateLimitEvaluator(rate.Inf, 0)
	le := e.EvaluateAgainst(pc1, rateLimitThreshold)
	if loadshedding.ThresholdExceeded(le) {
		t.Errorf("rate.Inf: EvaluateAgainst() => %#v; wanted a status of: %v", le, loadshedding.BelowThreshold)
	}

	// with limit, but burst size == 0, nothing should be allowed.
	e = loadshedding.NewRateLimitEvaluator(rate.Every(1*time.Millisecond), 0)
	le = e.EvaluateAgainst(pc1, rateLimitThreshold)
	if !loadshedding.ThresholdExceeded(le) {
		t.Errorf("no burst: EvaluateAgainst() => %#v; wanted a status of: %v", le, loadshedding.ExceedsThreshold)
	}

	// allow 10 events per second, try with 11, should fail
	e = loadshedding.NewRateLimitEvaluator(rate.Every(100*time.Millisecond), 1)
	le = e.EvaluateAgainst(pc11, rateLimitThreshold)
	if !loadshedding.ThresholdExceeded(le) {
		t.Errorf("every 100ms: EvaluateAgainst() => %#v; wanted a status of: %v", le, loadshedding.ExceedsThreshold)
	}
}
