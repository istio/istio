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
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/time/rate"
)

// Options define the set of configuration parameters for controlling
// loadshedding behavior.
type Options struct {
	// Mode controls the server loadshedding behavior.
	Mode ThrottlerMode

	// Options for the gRPC Average Latency evaluator

	// AverageLatencyThreshold is the threshold for response times
	// over which the server will start rejecting requests (Unavailable).
	// Providing a value for AverageLatencyThreshold will enable the gRPC
	// Latency evaluator.
	AverageLatencyThreshold time.Duration

	// SamplesPerSecond controls how often gRPC response latencies are
	// recorded for calculating the average response latency.
	SamplesPerSecond rate.Limit

	// SampleHalfLife controls the decay rate of observations of response latencies.
	SampleHalfLife time.Duration

	// LatencyEnforcementThreshold is the threshold for enforcement of response
	// latency. Above the threshold, requests will be throttled based on average
	// response latency. Below the threshold, no load-shedding will take place.
	// This provides an option for ignoring load-shedding at low request volumes
	// while preserving the protection at volume.
	LatencyEnforcementThreshold rate.Limit

	// Options for the rate limit evaluator

	// MaxRequestsPerSecond controls the rate of requests over which the
	// server will start rejecting requests (Unavailable). Providing a value
	// for MaxRequestsPerSecond will enable the rate limit evaluator.
	//
	// In Mixer, a single Report() request may translate to multiple requests
	// counted against this limit, depending on batch size of the Report.
	MaxRequestsPerSecond rate.Limit

	// BurstSize controls the number of requests that are permitted beyond the
	// configured maximum for a period of time. This allows for handling bursty
	// traffic patterns. If this is set to 0, no traffic will be allowed.
	BurstSize int
}

// DefaultOptions returns a new set of options, initialized to the defaults
func DefaultOptions() Options {
	return Options{
		AverageLatencyThreshold:     0,
		SamplesPerSecond:            DefaultSampleFrequency,
		SampleHalfLife:              DefaultHalfLife,
		MaxRequestsPerSecond:        0,
		BurstSize:                   0,
		Mode:                        Disabled,
		LatencyEnforcementThreshold: DefaultEnforcementThreshold,
	}
}

// AttachCobraFlags attaches a set of Cobra flags to the given Cobra command.
//
// Cobra is the command-line processor that Istio uses. This command attaches
// the necessary set of flags to expose a CLI to let the user control all
// tracing options.
func (o *Options) AttachCobraFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().VarP(newModeValue("disabled", &o.Mode), "loadsheddingMode", "",
		"When enabled, the server will log violations but will not enforce load limits.")

	cmd.PersistentFlags().DurationVarP(&o.AverageLatencyThreshold, "averageLatencyThreshold", "", 0,
		"Maximum average response time supported by the server. When this limit is exceeded, the server will drop traffic.")

	cmd.PersistentFlags().VarP(newLimitValue(DefaultSampleFrequency, &o.SamplesPerSecond), "latencySamplesPerSecond", "",
		"Controls the frequency at which the server will sample response times to calculate the average response latency.")

	cmd.PersistentFlags().DurationVarP(&o.SampleHalfLife, "latencySampleHalflife", "", DefaultHalfLife,
		"Decay rate of samples in calculation of average response latency.")

	cmd.PersistentFlags().VarP(newLimitValue(0, &o.MaxRequestsPerSecond), "maxRequestsPerSecond", "",
		"Maximum requests per second supported by the server. Any requests above this limit will be dropped.")

	cmd.PersistentFlags().IntVarP(&o.BurstSize, "burstSize", "", 0,
		"Number of requests that are permitted beyond the configured maximum for a period of time. Only valid when used with 'maxRequestsPerSecond'.")

	cmd.PersistentFlags().VarP(newLimitValue(DefaultEnforcementThreshold, &o.LatencyEnforcementThreshold), "latencyEnforcementThreshold", "",
		"Controls the threshold, in requests per second, above which the average latency threshold will be enforced for load-shedding")
}

type modeValue ThrottlerMode

func newModeValue(val string, p *ThrottlerMode) *modeValue {
	*p = stringToModes[val]
	return (*modeValue)(p)
}

func (mv *modeValue) Set(val string) error {
	*mv = modeValue(stringToModes[val])
	return nil
}
func (mv *modeValue) Type() string {
	return "throttlermode"
}

func (mv *modeValue) String() string { return modesToString[ThrottlerMode(*mv)] }

type limitValue rate.Limit

func newLimitValue(val rate.Limit, p *rate.Limit) *limitValue {
	*p = val
	return (*limitValue)(p)
}

func (lv *limitValue) Set(s string) error {
	v, err := strconv.ParseFloat(s, 64)
	*lv = limitValue(v)
	return err
}

func (lv *limitValue) Type() string {
	return "ratelimit"
}

func (lv *limitValue) String() string { return strconv.FormatFloat(float64(*lv), 'g', -1, 64) }
