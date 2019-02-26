// Copyright 2019 Istio Authors
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

package internal

import (
	"context"
	"time"

	"golang.org/x/time/rate"
)

// RateLimit is partially representing standard lib's rate limiter
type RateLimit interface {
	Wait(ctx context.Context) (err error)
}

var _ RateLimit = &rate.Limiter{}

// ConnectionRateLimit is an interface for creating per-connection rate limiters
type ConnectionRateLimit interface {
	Create() RateLimit
}

// RateLimiter is wrapper around golang's rate.Limit
// for more balanced rate limiting among multiple connections
type RateLimiter struct {
	connectionFreq      time.Duration
	connectionBurstSize int
}

type noopRateLimiter struct {
}

var _ RateLimit = &noopRateLimiter{}

// Wait implements RateLimit
func (n *noopRateLimiter) Wait(context.Context) error {
	return nil
}

type noopConnRateLimiter struct {
}

var _ ConnectionRateLimit = &noopConnRateLimiter{}

func (n *noopConnRateLimiter) Create() RateLimit {
	return &noopRateLimiter{}
}

// NewRateLimiter returns a new RateLimiter
func NewRateLimiter(freq time.Duration, burstSize int) *RateLimiter {
	return &RateLimiter{
		connectionFreq:      freq,
		connectionBurstSize: burstSize,
	}
}

// Create returns a new standard lib's rate limiter
// for each connectin with a given frequency and burstSize
func (c *RateLimiter) Create() RateLimit {
	return rate.NewLimiter(
		rate.Every(c.connectionFreq),
		c.connectionBurstSize)
}

// NewNoopRateLimiter returns a new rate limiter that does nothing. Useful for defaulting of options.
func NewNoopRateLimiter() RateLimit {
	return &noopRateLimiter{}
}

func NewNoopConnRateLimiter() ConnectionRateLimit {
	return &noopConnRateLimiter{}
}
