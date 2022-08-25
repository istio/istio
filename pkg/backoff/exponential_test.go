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

package backoff

import (
	"testing"
	"time"
)

func TestBackOff(t *testing.T) {
	var (
		testInitialInterval     = 500 * time.Millisecond
		testRandomizationFactor = 0.1
		testMultiplier          = 2.0
		testMaxInterval         = 5 * time.Second
		testMaxElapsedTime      = 15 * time.Minute
	)

	exp := NewExponentialBackOff(func(off *ExponentialBackOff) {
		off.InitialInterval = testInitialInterval
		off.RandomizationFactor = testRandomizationFactor
		off.Multiplier = testMultiplier
		off.MaxInterval = testMaxInterval
		off.MaxElapsedTime = testMaxElapsedTime
	})

	expectedResults := []time.Duration{500, 1000, 2000, 4000, 5000, 5000, 5000, 5000, 5000, 5000}
	for i, d := range expectedResults {
		expectedResults[i] = d * time.Millisecond
	}

	for _, expected := range expectedResults {
		// Assert that the next backoff falls in the expected range.
		minInterval := expected - time.Duration(testRandomizationFactor*float64(expected))
		maxInterval := expected + time.Duration(testRandomizationFactor*float64(expected))
		actualInterval := exp.NextBackOff()
		if !(minInterval <= actualInterval && actualInterval <= maxInterval) {
			t.Error("error")
		}
	}
}

type TestClock struct {
	i     time.Duration
	start time.Time
}

func (c *TestClock) Now() time.Time {
	t := c.start.Add(c.i)
	c.i += time.Second
	return t
}

func TestMaxElapsedTime(t *testing.T) {
	exp := NewExponentialBackOff(func(off *ExponentialBackOff) {
		off.Clock = &TestClock{start: time.Time{}}
	})
	b := exp.(*ExponentialBackOff)
	// override clock to simulate the max elapsed time has passed.
	b.Clock = &TestClock{start: time.Time{}.Add(10000 * time.Second)}
	assertEquals(t, MaxDuration, exp.NextBackOff())
}

func assertEquals(t *testing.T, expected, value time.Duration) {
	if expected != value {
		t.Errorf("got: %d, expected: %d", value, expected)
	}
}
