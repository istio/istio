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

// Package backoff is a wrapper of `github.com/cenkalti/backoff/v4`.
// It is to prevent misuse of `github.com/cenkalti/backoff/v4`,
// thus application could fall into dead loop.
// The difference is in this package `NextBackOff` returns a MaxDuration rather than backoff.Stop(which is -1)
// when MaxElapsedTime elapsed.
package backoff

import (
	"context"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// BackOff is a backoff policy for retrying an operation.
type BackOff interface {
	// NextBackOff returns the duration to wait before retrying the operation,
	// or backoff. Return MaxInterval when MaxElapsedTime elapsed.
	NextBackOff() time.Duration
	// Reset to initial state.
	Reset()
	// RetryWithContext tries the operation until it does not return error, BackOff stops,
	// or when the context expires, whichever happens first.
	RetryWithContext(ctx context.Context, operation func() error) error
}

// ExponentialBackOff is a wrapper of backoff.ExponentialBackOff to override its NextBackOff().
type ExponentialBackOff struct {
	*backoff.ExponentialBackOff
}

// NewExponentialBackOff creates an istio wrapped ExponentialBackOff.
// By default, it never stops.
func NewExponentialBackOff(initFuncs ...func(off *ExponentialBackOff)) BackOff {
	b := ExponentialBackOff{}
	b.ExponentialBackOff = backoff.NewExponentialBackOff()
	b.ExponentialBackOff.MaxElapsedTime = 0
	for _, fn := range initFuncs {
		fn(&b)
	}
	b.Reset()
	return b
}

func (b ExponentialBackOff) NextBackOff() time.Duration {
	duration := b.ExponentialBackOff.NextBackOff()
	if duration == b.Stop {
		return b.ExponentialBackOff.MaxInterval
	}
	return duration
}

func (b ExponentialBackOff) Reset() {
	b.ExponentialBackOff.Reset()
}

// RetryWithContext tries the operation until it does not return error, BackOff stops,
// or when the context expires, whichever happens first.
// o is guaranteed to be run at least once.
// RetryWithContext sleeps the goroutine for the duration returned by BackOff after a
// failed operation returns.
func (b ExponentialBackOff) RetryWithContext(ctx context.Context, operation func() error) error {
	b.Reset()
	for {
		err := operation()
		if err == nil {
			return nil
		}
		next := b.ExponentialBackOff.NextBackOff()
		if next == b.Stop {
			return fmt.Errorf("backoff timeouted with last error: %v", err)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%v with last error: %v", context.DeadlineExceeded, err)
		case <-time.After(next):
		}
	}
}
