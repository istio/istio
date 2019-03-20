// Copyright 2017 Istio Authors
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

package model

import (
	"strconv"

	"istio.io/istio/pkg/features/pilot"
)

// Default trace sampling, if not provided in env var.
const traceSamplingDefault = 100.0

var (
	traceSampling = getTraceSampling()
)

// Return trace sampling if set correctly, or default if not.
func getTraceSampling() float64 {
	if pilot.TraceSampling == "" {
		return traceSamplingDefault
	}
	f, err := strconv.ParseFloat(pilot.TraceSampling, 64)
	if err != nil {
		log.Warnf("PILOT_TRACE_SAMPLING not set to a number: %v", pilot.TraceSampling)
		return traceSamplingDefault
	}
	if f < 0.0 || f > 100.0 {
		log.Warnf("PILOT_TRACE_SAMPLING out of range: %v", f)
		return traceSamplingDefault
	}
	return f
}

// TraceConfig values are percentages 0.0 - 100.0
type TraceConfig struct {
	ClientSampling  float64
	RandomSampling  float64
	OverallSampling float64
}

// GetTraceConfig returns configured TraceConfig
func GetTraceConfig() TraceConfig {
	return TraceConfig{
		ClientSampling:  100.0,
		RandomSampling:  traceSampling,
		OverallSampling: 100.0,
	}
}
