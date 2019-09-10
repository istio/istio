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

package restart

import (
	"errors"
	"time"
)

const (
	infinity = 1<<63 - 1
)

// newRetryStrategy creates a new instance of retryStrategy.
func newRetryStrategy(maxRetries uint, initialInterval time.Duration) *retryStrategy {
	return &retryStrategy{
		maxRetries:      maxRetries,
		initialInterval: initialInterval,
		budget:          maxRetries,
	}
}

// retryStrategy is a strategy for managing the restart of Envoy.
type retryStrategy struct {
	// MaxRetries is the maximum number of retries
	maxRetries uint

	// InitialInterval is the delay between the first restart, from then on it is
	// multiplied by a factor of 2 for each subsequent retry
	initialInterval time.Duration

	// time is the timestamp of the next scheduled restart attempt
	time *time.Time

	timer *time.Timer

	// number of times to attempts left to retry applying the latest desired configuration
	budget uint
}

// Chan returns a new channel for receiving the next retry timer.
func (s *retryStrategy) Chan() <-chan time.Time {
	// Stop the existing timer if one exists.
	if s.timer != nil {
		s.timer.Stop()
	}

	// Get the delay until the next restart, or infinity if there is no restart time set.
	delay := time.Duration(infinity)
	if s.time != nil {
		delay = time.Until(*s.time)
	}

	// Create a new timer.
	s.timer = time.NewTimer(delay)
	return s.timer.C
}

// Budget returns the current retry budget.
func (s *retryStrategy) Budget() uint {
	return s.budget
}

// Reset restores the retry budget.
func (s *retryStrategy) Reset() {
	s.budget = s.maxRetries
}

// Cancel the timer, if one is pending.
func (s *retryStrategy) Cancel() {
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = nil
	s.time = nil
}

// Schedule the next retry, if not already scheduled.
func (s *retryStrategy) Schedule() (newScheduleCreated bool, budgetExhaustedErr error) {
	// If a retry is already scheduled, do nothing.
	if s.time != nil {
		return false, nil
	}

	// Return an error if the budget is exhausted.
	if s.budget == 0 {
		return false, errors.New("budget exhausted")
	}

	// Schedule a retry for the error.
	attempt := s.maxRetries - s.budget
	delay := s.initialInterval * (1 << attempt)
	newRestartTime := time.Now().Add(delay)
	s.time = &newRestartTime
	s.budget--
	return true, nil
}
