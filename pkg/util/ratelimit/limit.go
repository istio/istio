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
	"time"
)

type Limiter interface {
	// Wait will block until the configured rate limit allows continuing.
	Wait()
	Close()
}

type leakyBucket chan struct{}

// NewLeakyBucket creates a Limiter that will allow rate requests in an interval of duration seconds.
func NewLeakyBucket(rate uint16, duration time.Duration) Limiter {
	// bucket will never hold more than `rate` items
	// Wait will "pour" from the bucket. If it's empty, we wait until it isn't.
	// On a duration/rate interval, the bucket is filled with 1 item.
	bucket := leakyBucket(make(chan struct{}, rate))

	// initial fill
	for i := 0; i < cap(bucket); i++ {
		bucket <- struct{}{}
	}

	// periodic bucket fill
	go func() {
		tickRate := time.Duration(int(duration) / int(rate))
		ticker := time.NewTicker(tickRate)
		defer ticker.Stop()
		for range ticker.C {
			_, ok := <-bucket
			// closing the bucket chan signals this goroutine to exit
			if !ok {
				return
			}
		}
	}()

	return &bucket
}

func (l *leakyBucket) Wait() {
	*l <- struct{}{}
}

func (l *leakyBucket) Close() {
	close(*l)
}
