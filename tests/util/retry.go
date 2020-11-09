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

package util

import (
	"context"
	"time"

	"istio.io/pkg/log"
)

const (
	backoffFactor = 1.3 // backoff increases by this factor on each retry
)

// Backoff returns a random value in [0, maxDelay] that increases exponentially with
// retries, starting from baseDelay.  It is the Go equivalent to C++'s
// //util/time/backoff.cc.
func Backoff(baseDelay, maxDelay time.Duration, retries int) time.Duration {
	backoff, max := float64(baseDelay), float64(maxDelay)
	for backoff < max && retries > 0 {
		backoff *= backoffFactor
		retries--
	}
	if backoff > max {
		backoff = max
	}

	if backoff < 0 {
		return 0
	}
	return time.Duration(backoff)
}

// Retrier contains the retry configuration parameters.
type Retrier struct {
	// BaseDelay is the minimum delay between retry attempts.
	BaseDelay time.Duration
	// MaxDelay is the maximum delay allowed between retry attempts.
	MaxDelay time.Duration
	// MaxDuration is the maximum cumulative duration allowed for all retries
	MaxDuration time.Duration
	// Retries defines number of retry attempts
	Retries int
}

// Break the retry loop if the error returned is of this type.
type Break struct {
	Err error
}

func (e Break) Error() string {
	return e.Err.Error()
}

// Retry calls the given function a number of times, unless it returns a nil or a Break
func (r Retrier) Retry(ctx context.Context, fn func(ctx context.Context, retryIndex int) error) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r.MaxDuration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.MaxDuration)
		defer cancel()
	}

	var err error
	var i int
	if r.Retries <= 0 {
		log.Warnf("retries must to be >= 1. Got %d, setting to 1", r.Retries)
		r.Retries = 1
	}
	for i = 1; i <= r.Retries; i++ {
		err = fn(ctx, i)
		if err == nil {
			return i, nil
		}
		if be, ok := err.(Break); ok {
			return i, be.Err
		}

		select {
		case <-ctx.Done():
			return i - 1, ctx.Err()
		case <-time.After(Backoff(r.BaseDelay, r.MaxDelay, i)):
		}
	}
	return i - 1, err
}
