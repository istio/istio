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
	"testing"
	"time"
)

func TestExponentialMovingAverage(t *testing.T) {

	// test initial value
	ema := newExponentialMovingAverage(1*time.Second, 0.0, time.Unix(0, 0))
	if got, want := ema.currentValue(time.Unix(0, 0)), 0.0; got != want {
		t.Errorf("currentValue() => %v, want %v", got, want)
	}

	// test decay over seconds (initial 0 value + new sample of 1 decaying )
	ema.addSample(1, time.Unix(1, 0))
	if got, want := ema.currentValue(time.Unix(2, 0)), 0.5; got != want {
		t.Errorf("currentValue() => %v, want %v", got, want)
	}
	if got, want := ema.currentValue(time.Unix(3, 0)), 0.75; got != want {
		t.Errorf("currentValue() => %v, want %v", got, want)
	}
	if got, want := ema.currentValue(time.Unix(4, 0)), 0.875; got != want {
		t.Errorf("currentValue() => %v, want %v", got, want)
	}

	// test decay over seconds (initial 0, 1 + new sample of 2 decaying)
	ema.addSample(2, time.Unix(2, 0))
	if got, want := ema.currentValue(time.Unix(3, 0)), 1.25; got != want {
		t.Errorf("currentValue() => %v, want %v", got, want)
	}
	if got, want := ema.currentValue(time.Unix(4, 0)), 1.625; got != want {
		t.Errorf("currentValue() => %v, want %v", got, want)
	}
	if got, want := ema.currentValue(time.Unix(5, 0)), 1.8125; got != want {
		t.Errorf("currentValue() => %v, want %v", got, want)
	}

	// multiple samples at same instant
	ema.addSample(3.5, time.Unix(2, 0))
	if got, want := ema.currentValue(time.Unix(3, 0)), 2.0; got != want {
		t.Errorf("currentValue() => %v, want %v", got, want)
	}

	// adding samples from the past (going backwards)
	ema.addSample(2.5, time.Unix(1, 0))
	if got, want := ema.currentValue(time.Unix(3, 0)), 2.0; got != want {
		t.Errorf("currentValue() => %v, want %v", got, want)
	}
}
