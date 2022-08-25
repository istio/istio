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
	"time"

	"github.com/cenkalti/backoff/v4"
)

// BackOff is a backoff policy for retrying an operation.
type BackOff interface {
	// NextBackOff returns the duration to wait before retrying the operation,
	// or backoff. Return MaxDuration when MaxElapsedTime elapsed.
	NextBackOff() time.Duration
}

// ExponentialBackOff is a wrapper of backoff.ExponentialBackOff to override its NextBackOff().
type ExponentialBackOff struct {
	*backoff.ExponentialBackOff
}

// NewExponentialBackOff creates an istio wrapped ExponentialBackOff.
func NewExponentialBackOff(initFuncs ...func(off *ExponentialBackOff)) BackOff {
	b := &ExponentialBackOff{}
	b.ExponentialBackOff = backoff.NewExponentialBackOff()
	for _, fn := range initFuncs {
		fn(b)
	}
	b.Reset()
	return b
}

const MaxDuration = 1<<63 - 1

func (b *ExponentialBackOff) NextBackOff() time.Duration {
	duration := b.ExponentialBackOff.NextBackOff()
	if duration == b.Stop {
		return MaxDuration
	}
	return duration
}
