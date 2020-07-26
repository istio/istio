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
	"math"
	"sync"
	"time"
)

// exponentialMovingAverage is smoothing function used to continuously average
// a stream of samples, based on a given decay rate. Those samples may be
// irregularly spaced (in time). It  applies this smoothing as though the input
// is constant (at the same value as the previous sample) between samples.
//
// exponentialMovingAverage can be applied by sampling a noisy or variable signal
// but its accuracy will depend greatly on the sampling rate and the rate of change
// of the original signal.
//
// EMA(t) := EMA(s) * alpha(t-s) + X[s] * (1-alpha(t-s)) where alpha(t-s) is the
// smoothing factor.
//
// alpha can be expressed in terms of a half-life h using the following:
// alpha(t-s) := (1/2)^((t-s)/h) = 2^(-(t-s)/h) = exp((t-s) * -log(2)/h)
//
// Here, we rewrite EMA(t) as: EMA(t) = alpha(t-s) * (EMA(s) - X[s]) + X[s]
// so we can store (EMA(s) - X[s]) instead of EMA(s) and save 2 ops per
// currentValue().
//
// See also: http://en.wikipedia.org/wiki/Moving_average#Exponential_moving_average.
type exponentialMovingAverage struct {
	sync.Mutex
	decay                 float64
	lastSampleTime        time.Time
	lastSmoothedDeviation float64
	lastSampleValue       float64
}

func newExponentialMovingAverage(halfLife time.Duration, initialValue float64, initialTime time.Time) *exponentialMovingAverage {
	return &exponentialMovingAverage{
		decay:                 -math.Log(2) / halfLife.Seconds(),
		lastSampleTime:        initialTime,
		lastSmoothedDeviation: 0,
		lastSampleValue:       initialValue,
	}
}

func (ema *exponentialMovingAverage) currentValueLocked(now time.Time) float64 {
	alpha := 1.0
	if now.After(ema.lastSampleTime) {
		alpha = math.Exp(ema.decay * now.Sub(ema.lastSampleTime).Seconds())
	}
	return alpha*ema.lastSmoothedDeviation + ema.lastSampleValue
}

func (ema *exponentialMovingAverage) currentValue(now time.Time) float64 {
	ema.Lock()
	curVal := ema.currentValueLocked(now)
	ema.Unlock()
	return curVal
}

func (ema *exponentialMovingAverage) addSample(sample float64, now time.Time) {
	ema.Lock()
	smoothedValue := ema.currentValueLocked(now)
	ema.lastSmoothedDeviation = smoothedValue - sample
	ema.lastSampleValue = sample
	ema.lastSampleTime = now
	ema.Unlock()
}
