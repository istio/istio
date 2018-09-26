// Copyright 2018 Istio Authors.
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

package test

import (
	"errors"
	"testing"
	"time"

	"github.com/cenkalti/backoff"
)

// ErrWaitTimeout is returned when the condition exited without success.
var ErrWaitTimeout = errors.New("timed out waiting for the condition")

// Condition is a function that returns true when a test condition is satisfied, or an error
// if the loop should be aborted.
type Condition func() (done bool, err error)

// Eventually polls cond until it completes (returns true) or times out (resulting in a test failure).
func Eventually(t *testing.T, name string, cond Condition) {
	t.Helper()
	EventualOpts{backoff.NewExponentialBackOff()}.Eventually(t, name, cond)
}

// EventualOpts defines a polling strategy for operations that must eventually succeed. A new EventualOpts must be provided
// for each invocation of Eventually (or call Reset on a previously completed set of options).
type EventualOpts struct {
	strategy *backoff.ExponentialBackOff
}

// NewEventualOpts constructs an EventualOpts instance with the provided polling interval and deadline. EventualOpts will
// perform randomized exponential backoff using the starting interval, and will stop polling (and therefore fail) after
// deadline time as elapsed from calling Eventually.
//
// Note: we always backoff with a randomization of 0.5 (50%), a multiplier of 1.5.
func NewEventualOpts(interval, deadline time.Duration) *EventualOpts {
	strategy := backoff.NewExponentialBackOff()
	strategy.InitialInterval = interval
	strategy.MaxElapsedTime = deadline
	return &EventualOpts{strategy}
}

// Eventually polls cond until it succeeds (returns true), return an error, or we exceed the deadline. Eventually performs backoff while
// polling cond.
//
// Eventually always checks 'condition' before waiting for the interval. 'condition'
// will always be invoked at least once.
//
// name is printed as part of the test failure message when we exceed the deadline to help identify the test case failing.
// cond does not need to be thread-safe: it is only called from the current goroutine. cond itself can also fail the test early using t.Fatal.
func (e EventualOpts) Eventually(t *testing.T, name string, cond Condition) error {
	t.Helper()

	// Check once before we start polling.
	done, err := cond()
	if err != nil {
		return err
	}
	if done {
		return nil
	}

	// We didn't get a happy fast-path, so set up timers and wait.
	// The backoff's ticker will close the channel after MaxElapsedTime, so we don't need to worry about a timeout.
	poll := backoff.NewTicker(e.strategy).C
	for {
		_, cont := <-poll
		done, err := cond()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if !cont {
			break
		}
	}
	return ErrWaitTimeout
}
