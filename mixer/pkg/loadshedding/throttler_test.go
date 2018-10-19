// Copyright 2018 Istio Authors
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
	"reflect"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"istio.io/istio/mixer/pkg/loadshedding"
)

var (
	maxRPS = rate.Every(10 * time.Millisecond)
	burst  = 1

	rateLimitOpts = loadshedding.Options{
		Mode:                 loadshedding.LogOnly,
		MaxRequestsPerSecond: maxRPS,
		BurstSize:            burst,
	}

	rateLimitEval = loadshedding.NewRateLimitEvaluator(maxRPS, burst)

	samplesPerSec = 100000

	grpcLatencyOpts = loadshedding.Options{
		Mode: loadshedding.Enforce,
		AverageLatencyThreshold: 1 * time.Nanosecond,
		SampleHalfLife:          1 * time.Millisecond,
		SamplesPerSecond:        rate.Every(1 * time.Nanosecond),
	}

	grpcLatencyEval = loadshedding.NewGRPCLatencyEvaluator(rate.Every(1*time.Nanosecond), 1*time.Millisecond)
)

type evalComparisonFn func(got loadshedding.LoadEvaluator) bool
type evalMap map[string]evalComparisonFn

func TestNewThrottler(t *testing.T) {

	rateLimitEvalFn := func(got loadshedding.LoadEvaluator) bool {
		return reflect.DeepEqual(got, rateLimitEval)
	}

	latencyEvalFn := func(got loadshedding.LoadEvaluator) bool {
		_, ok := got.(*loadshedding.GRPCLatencyEvaluator)
		return ok
	}

	cases := []struct {
		name       string
		opts       loadshedding.Options
		evaluators evalMap
	}{
		{"default", *loadshedding.DefaultOptions(), evalMap{}},
		{"rate limit", rateLimitOpts, evalMap{loadshedding.RateLimitEvaluatorName: rateLimitEvalFn}},
		{"latency", grpcLatencyOpts, evalMap{loadshedding.GRPCLatencyEvaluatorName: latencyEvalFn}},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			thr := loadshedding.NewThrottler(v.opts)
			for name, wantFn := range v.evaluators {
				got := thr.Evaluator(name)
				if got == nil {
					tt.Errorf("Evaluator(%s) => nil; wanted LoadEvaluator.", name)
				}
				if wantFn != nil && !wantFn(got) {
					tt.Errorf("Evaluator(%s) => %#v, which did not pass supplied validation function", name, got)
				}
			}
		})
	}
}
