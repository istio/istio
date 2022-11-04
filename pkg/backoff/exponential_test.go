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
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"istio.io/istio/pkg/test/util/assert"
)

func TestBackOff(t *testing.T) {
	var (
		testInitialInterval = 500 * time.Millisecond
		testMaxInterval     = 5 * time.Second
	)

	o := DefaultOption()
	o.InitialInterval = testInitialInterval
	o.MaxInterval = testMaxInterval
	exp := NewExponentialBackOff(o)
	exp.(ExponentialBackOff).exponentialBackOff.Multiplier = 2

	expectedResults := []time.Duration{500, 1000, 2000, 4000, 5000, 5000, 5000, 5000, 5000, 5000}
	for i, d := range expectedResults {
		expectedResults[i] = d * time.Millisecond
	}

	DefaultRandomizationFactor := 0.5
	for _, expected := range expectedResults {
		// Assert that the next backoff falls in the expected range.
		minInterval := expected - time.Duration(DefaultRandomizationFactor*float64(expected))
		maxInterval := expected + time.Duration(DefaultRandomizationFactor*float64(expected))
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

func TestRetry(t *testing.T) {
	o := DefaultOption()
	o.InitialInterval = 1 * time.Microsecond
	ebf := NewExponentialBackOff(o)

	// Run a task that fails the first time and retries.
	wg := sync.WaitGroup{}
	wg.Add(2)
	failed := false
	err := ebf.RetryWithContext(context.TODO(), func() error {
		defer wg.Done()
		if failed {
			return nil
		}
		failed = true
		return errors.New("fake error")
	})
	assert.NoError(t, err)

	// wait for the task to run twice.
	wg.Wait()

	o.InitialInterval = 1 * time.Second
	// Test timeout context
	ebf = NewExponentialBackOff(o)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Microsecond)
	defer cancel()

	count := 0
	err = ebf.RetryWithContext(ctx, func() error {
		count++
		return errors.New("fake error")
	})
	assert.Error(t, err)
	assert.Equal(t, count, 1)
}
