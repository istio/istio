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

package rate

import (
	"context"
	"time"

	"golang.org/x/time/rate"
)

// Limit is partially representing standard lib's rate limiter
type Limit interface {
	Wait(ctx context.Context) (err error)
}

// LimitFactory is an interface for creating per-connection rate limiters
type LimitFactory interface {
	Create() Limit
}

// Limiter is wrapper around golang's rate.Limit
// for more balanced rate limiting among multiple connections
type Limiter struct {
	connectionFreq      time.Duration
	connectionBurstSize int
}

// NewRateLimiter returns a new Limiter
func NewRateLimiter(freq time.Duration, burstSize int) *Limiter {
	return &Limiter{
		connectionFreq:      freq,
		connectionBurstSize: burstSize,
	}
}

// Create returns a new standard lib's rate limiter
// for each connectin with a given frequency and burstSize
func (c *Limiter) Create() Limit {
	return rate.NewLimiter(
		rate.Every(c.connectionFreq),
		c.connectionBurstSize)
}
