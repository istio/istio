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
	"context"
	"testing"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc/stats"

	"istio.io/istio/mixer/pkg/loadshedding"
)

var (
	start, _ = time.Parse(time.RFC3339, time.RFC3339)

	oneSec = &stats.End{
		BeginTime: start,
		EndTime:   start.Add(1 * time.Second),
	}
)

// The exponential moving average stuff will be tested in other
// tests. This test is about validating behavior in broad strokes
// (do the defaults work? what happens when overrides are provided?).
func TestEvaluateAgainst_GRPCLatency(t *testing.T) {

	cases := []struct {
		name         string
		samplingRate rate.Limit
		halfLife     time.Duration
	}{
		{"Defaults", 0, 0},
		{"Once every 100ms", rate.Every(100 * time.Millisecond), 100 * time.Millisecond},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			e := loadshedding.NewGRPCLatencyEvaluatorWithThreshold(v.samplingRate, v.halfLife, rate.Limit(0))
			stop := make(chan bool)
			for i := 0; i < 10; i++ {
				go func(ch chan bool) {
					for {
						select {
						case <-ch:
							return
						default:
							// establish an average latency of 1s
							e.HandleRPC(context.Background(), oneSec)
						}

					}
				}(stop)
			}

			// allow collection of some latency measurements
			time.Sleep(500 * time.Millisecond)

			pc := loadshedding.RequestInfo{PredictedCost: 1.0}
			passingThreshold := 2.0 // simulates a threshold of 2 secs (which we should be well-below)
			failingThreshold := 0.5 // simulates a threshold of 0.5 secs (which we should be above)

			le := e.EvaluateAgainst(pc, passingThreshold)
			if loadshedding.ThresholdExceeded(le) {
				tt.Logf("Got: %#v", le)
				tt.Errorf("EvaluateAgainst(%#v, %f) => Status: %v; wanted %v", pc, passingThreshold, le.Status, loadshedding.BelowThreshold)
			}

			le = e.EvaluateAgainst(pc, failingThreshold)
			if !loadshedding.ThresholdExceeded(le) {
				tt.Logf("Got: %#v", le)
				tt.Errorf("EvaluateAgainst(%#v, %f) => Status: %v; wanted %v", pc, failingThreshold, le.Status, loadshedding.ExceedsThreshold)
			}
			close(stop)
		})
	}
}

func TestEvaluateAgainst_GRPCLatency_WithThreshold(t *testing.T) {

	cases := []struct {
		name         string
		threshold    rate.Limit
		shouldExceed bool
	}{
		{"BelowThreshold", rate.Inf, false},
		{"BeyondThreshold", rate.Limit(1), true},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			e := loadshedding.NewGRPCLatencyEvaluatorWithThreshold(0, 0, v.threshold)
			stop := make(chan bool)
			for i := 0; i < 10; i++ {
				go func(ch chan bool) {
					for {
						select {
						case <-ch:
							return
						default:
							// establish an average latency of 1s
							e.HandleRPC(context.Background(), oneSec)
						}

					}
				}(stop)
			}

			// allow collection of some latency measurements
			time.Sleep(500 * time.Millisecond)

			pc := loadshedding.RequestInfo{PredictedCost: 1.0}
			failingThreshold := 0.5 // simulates a threshold of 0.5 secs (which we should be above)

			for i := 0; i < 10; i++ {
				e.EvaluateAgainst(pc, failingThreshold) // simulate 10 requests checked for load-shedding
			}

			le := e.EvaluateAgainst(pc, failingThreshold)
			if got, want := loadshedding.ThresholdExceeded(le), v.shouldExceed; got != want {
				tt.Logf("Got: %#v", le)
				tt.Errorf("EvaluateAgainst(%#v, %f) => got exceeded == %t; wanted exceeded == %t", pc, failingThreshold, got, want)
			}
			close(stop)
		})
	}
}
