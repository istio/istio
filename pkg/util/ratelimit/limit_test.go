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

package ratelimit

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	tests := []struct {
		rate             uint16
		duration         time.Duration
		iterations       int
		expectedDuration time.Duration
	}{
		{
			rate:             10,
			duration:         time.Second,
			iterations:       30,
			expectedDuration: 3 * time.Second,
		},
		{
			rate:             1000,
			duration:         time.Minute,
			iterations:       50,
			expectedDuration: 3 * time.Second,
		},
	}
	const tolerance = 50 * time.Millisecond
	for _, tt := range tests {
		name := fmt.Sprintf("%d_per_%v_for_%d_takes_%v",
			tt.rate,
			tt.duration,
			tt.iterations,
			tt.expectedDuration)

		t.Run(name, func(t *testing.T) {
			limiter := NewLeakyBucket(tt.rate, tt.duration)
			start := time.Now()
			wg := sync.WaitGroup{}
			for i := 0; i < tt.iterations; i++ {
				limiter.Wait()
				wg.Add(1)
				go func() {
					defer wg.Done()
				}()
			}
			wg.Wait() // ensure all jobs completed
			limiter.Close()
			elapsed := time.Since(start)
			if absDiff(elapsed, tt.expectedDuration) > tolerance {
				t.Errorf("expected to take %v; took %v", tt.expectedDuration, elapsed)
			} else {
				t.Logf("took %v", elapsed)
			}
		})

	}
}

func absDiff(a, b time.Duration) time.Duration {
	if a >= b {
		return a - b
	}
	return b - a
}
