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

	"istio.io/pkg/log"
	"istio.io/pkg/monitoring"
)

const (
	// Disabled removes all throttling behavior for the server.
	Disabled ThrottlerMode = iota
	// LogOnly enables an advisory mode for throttling behavior on the server.
	LogOnly
	// Enforce turns the throttling behavior on for the server.
	Enforce
)

type (
	// ThrottlerMode controls the behavior a throttler.
	ThrottlerMode int

	// RequestInfo is used to hold information related to a request that
	// could be relevant to a LoadEvaluator.
	RequestInfo struct {
		// PredictedCost enables the server to pass information about the relative
		// size (or impact) of the request into the throttler. For instance, it can
		// be used to distinguish between Check() and Report() calls by setting the
		// value to the size of the batch.
		PredictedCost float64
	}

	// Throttler provides the loadshedding behavior by evaluating current request information
	// against a set of configured LoadEvaluators.
	Throttler struct {
		mode       ThrottlerMode
		evaluators map[string]LoadEvaluator
		thresholds map[string]float64
	}
)

var (
	scope = log.RegisterScope("loadshedding", "Information related to loadshedding", 0)

	modesToString = map[ThrottlerMode]string{
		Disabled: "disabled",
		LogOnly:  "logonly",
		Enforce:  "enforce",
	}

	stringToModes = map[string]ThrottlerMode{
		"disabled": Disabled,
		"logonly":  LogOnly,
		"enforce":  Enforce,
	}

	predictedCost = monitoring.NewSum(
		"mixer/loadshedding/predicted_cost_shed_total",
		"The total predicted cost of all requests that have been dropped.")

	throttled = monitoring.NewSum(
		"mixer/loadshedding/requests_throttled",
		"The number of requests that have been dropped by the loadshedder.")
)

func init() {
	monitoring.MustRegister(throttled, predictedCost)
}

// NewThrottler builds a Throttler based on the configured options.
func NewThrottler(opts Options) *Throttler {
	t := &Throttler{
		mode:       opts.Mode,
		evaluators: make(map[string]LoadEvaluator),
		thresholds: make(map[string]float64),
	}

	if t.mode == Disabled {
		// don't bother to create Evaluators if throttling is not
		// enabled.
		return t
	}

	if opts.AverageLatencyThreshold > 0 {
		e := NewGRPCLatencyEvaluatorWithThreshold(opts.SamplesPerSecond, opts.SampleHalfLife, opts.LatencyEnforcementThreshold)
		t.evaluators[e.Name()] = e
		t.thresholds[e.Name()] = opts.AverageLatencyThreshold.Seconds()
	}

	if opts.MaxRequestsPerSecond > 0 {
		e := NewRateLimitEvaluator(opts.MaxRequestsPerSecond, opts.BurstSize)
		t.evaluators[e.Name()] = e
		t.thresholds[e.Name()] = float64(opts.MaxRequestsPerSecond)
	}

	scope.Debugf("Built Throttler(%#v) from opts(%#v)", t, opts)
	return t
}

// Evaluator returns a configured LoadEvaluator based on the supplied name. If no
// LoadEvaluator with the given name is known to the Throttler, a nil value will
// be returned.
func (t *Throttler) Evaluator(name string) LoadEvaluator {
	return t.evaluators[name]
}

// Throttle returns a verdict on whether or not the server should drop the request, based on
// the current set of configured LoadEvaluators.
func (t *Throttler) Throttle(ri RequestInfo) bool {
	if t.mode == Disabled {
		return false
	}
	for _, e := range t.evaluators {
		thres, found := t.thresholds[e.Name()]
		if !found {
			continue
		}
		scope.Debugf("Evaluating load with %s against threshold %f", e.Name(), thres)
		eval := e.EvaluateAgainst(ri, thres)
		if ThresholdExceeded(eval) {
			msg := fmt.Sprintf("Throttled (%s): '%s'", e.Name(), eval.Message)
			if t.mode == LogOnly {
				scope.Infoa("LogOnly - ", msg)
				continue
			}
			throttled.Increment()
			predictedCost.Record(ri.PredictedCost)
			scope.Warn(msg)
			return true
		}
	}
	return false
}
