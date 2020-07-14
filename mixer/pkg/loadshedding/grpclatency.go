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
	"context"
	"fmt"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc/stats"
)

const (
	// DefaultSampleFrequency controls the base sampling rate of latency averaging calculation.
	DefaultSampleFrequency = rate.Inf
	// DefaultHalfLife controls the decay rate of an individual sample.
	DefaultHalfLife = 1 * time.Second // Impact of each sample is expected to last ~2s.
	// DefaultEnforcementThreshold controls the RPS limit under which no load shedding will occur.
	DefaultEnforcementThreshold = rate.Limit(100.0)
	// GRPCLatencyEvaluatorName is the name of the gRPC Response Latency LoadEvaluator.
	GRPCLatencyEvaluatorName = "grpcResponseLatency"
)

var (
	_ stats.Handler = &GRPCLatencyEvaluator{}
	_ LoadEvaluator = &GRPCLatencyEvaluator{}
)

// GRPCLatencyEvaluator calculates the moving average of response latency (as reported via the gRPC stats.Handler interface).
// It then evaluates incoming requests by comparing the average response latency against a threshold.
type GRPCLatencyEvaluator struct {
	sampler                     *rate.Limiter
	enforcementThresholdLimiter *rate.Limiter
	loadAverage                 *exponentialMovingAverage
}

// NewGRPCLatencyEvaluator creates a new LoadEvaluator that uses an average of gRPC Response Latency.
func NewGRPCLatencyEvaluator(sampleFrequency rate.Limit, averageHalfLife time.Duration) *GRPCLatencyEvaluator {
	return NewGRPCLatencyEvaluatorWithThreshold(sampleFrequency, averageHalfLife, DefaultEnforcementThreshold)
}

// NewGRPCLatencyEvaluatorWithThreshold creates a new LoadEvaluator that uses an average of gRPC Response Latency above
// the specified RPS limit.
func NewGRPCLatencyEvaluatorWithThreshold(sampleFrequency rate.Limit, averageHalfLife time.Duration, enforcementThreshold rate.Limit) *GRPCLatencyEvaluator {

	sf := sampleFrequency
	if sf == 0 {
		sf = DefaultSampleFrequency
	}

	hl := averageHalfLife
	if hl == 0 {
		hl = DefaultHalfLife
	}

	// allow burstiness in enforcement threshold evaluation -- up to 100% of threshold (all simultaneous requests)
	thresholdLimiter := rate.NewLimiter(enforcementThreshold, int(enforcementThreshold))

	return &GRPCLatencyEvaluator{
		sampler:                     rate.NewLimiter(sf, 1), // no need to support burstiness beyond 1 event per Allow()
		enforcementThresholdLimiter: thresholdLimiter,
		loadAverage:                 newExponentialMovingAverage(hl, 0, time.Now()),
	}
}

// Name implements the LoadEvaluator interface.
func (g GRPCLatencyEvaluator) Name() string {
	return GRPCLatencyEvaluatorName
}

// EvaluateAgainst implements the LoadEvaluator interface.
func (g *GRPCLatencyEvaluator) EvaluateAgainst(ri RequestInfo, threshold float64) LoadEvaluation {

	if g.enforcementThresholdLimiter.Allow() {
		// if we haven't hit the enforcement limit, then just allow
		return LoadEvaluation{Status: BelowThreshold}
	}

	load := g.currentLoad()
	if load < threshold {
		return LoadEvaluation{Status: BelowThreshold}
	}
	return LoadEvaluation{
		Status:  ExceedsThreshold,
		Message: fmt.Sprintf("Current observed average latency (%f) exceeds specified threshold (%f). Please retry request.", load, threshold),
	}
}

// HandleRPC processes the RPC stats.
func (g *GRPCLatencyEvaluator) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	if !g.sampler.Allow() {
		return
	}
	switch st := rs.(type) {
	case *stats.End:
		dur := st.EndTime.Sub(st.BeginTime)
		g.loadAverage.addSample(dur.Seconds(), st.EndTime)
	}
}

// TagRPC can attach some information to the given context.
func (g *GRPCLatencyEvaluator) TagRPC(ctx context.Context, rti *stats.RPCTagInfo) context.Context {
	return ctx
}

// TagConn can attach some information to the given context.
func (g *GRPCLatencyEvaluator) TagConn(ctx context.Context, cti *stats.ConnTagInfo) context.Context {
	return ctx
}

// HandleConn processes the Conn stats.
func (g *GRPCLatencyEvaluator) HandleConn(context.Context, stats.ConnStats) {}

func (g *GRPCLatencyEvaluator) currentLoad() float64 {
	return g.loadAverage.currentValue(time.Now())
}
