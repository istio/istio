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
// thus the application could fall into dead loop.
package backoff

import (
	"context"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// BackOff is a backoff policy for retrying an operation.
type BackOff interface {
	// NextBackOff returns the duration to wait before retrying the next operation.
	NextBackOff() time.Duration
	// Reset to initial state.
	Reset()
	// RetryWithContext tries the operation until it does not return error,
	// or when the context expires, whichever happens first.
	RetryWithContext(ctx context.Context, operation func() error) error
}

type Option struct {
	InitialInterval time.Duration
	MaxInterval     time.Duration
}

// ExponentialBackOff is a wrapper of backoff.ExponentialBackOff to override its NextBackOff().
type ExponentialBackOff struct {
	exponentialBackOff *backoff.ExponentialBackOff
}

// Default values for ExponentialBackOff.
const (
	defaultInitialInterval = 500 * time.Millisecond
	defaultMaxInterval     = 60 * time.Second
)

func DefaultOption() Option {
	return Option{
		InitialInterval: defaultInitialInterval,
		MaxInterval:     defaultMaxInterval,
	}
}

// NewExponentialBackOff creates an istio wrapped ExponentialBackOff.
// By default, it never stops.
func NewExponentialBackOff(o Option) BackOff {
	b := ExponentialBackOff{}
	b.exponentialBackOff = backoff.NewExponentialBackOff()
	b.exponentialBackOff.InitialInterval = o.InitialInterval
	b.exponentialBackOff.MaxInterval = o.MaxInterval
	b.Reset()
	return b
}

func (b ExponentialBackOff) NextBackOff() time.Duration {
	duration := b.exponentialBackOff.NextBackOff()
	// always return maxInterval after it reaches MaxElapsedTime
	if duration == b.exponentialBackOff.Stop {
		return b.exponentialBackOff.MaxInterval
	}
	return duration
}

func (b ExponentialBackOff) Reset() {
	b.exponentialBackOff.Reset()
}

// RetryWithContext tries the operation until it does not return error,
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
		next := b.NextBackOff()
		select {
		case <-ctx.Done():
			return fmt.Errorf("%v with last error: %v", context.DeadlineExceeded, err)
		case <-time.After(next):
		}
	}
}
