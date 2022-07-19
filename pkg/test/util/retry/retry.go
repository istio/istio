//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package retry

import (
	"errors"
	"fmt"
	"time"

	"istio.io/istio/pkg/test"
	"istio.io/pkg/log"
)

var scope = log.RegisterScope("retry", "logs for retries", 0)

const (
	// DefaultTimeout the default timeout for the entire retry operation
	DefaultTimeout = time.Second * 30

	// DefaultDelay the default delay between successive retry attempts
	DefaultDelay = time.Millisecond * 10

	// DefaultConverge the default converge, requiring something to succeed one time
	DefaultConverge = 1
)

var defaultConfig = config{
	timeout:  DefaultTimeout,
	delay:    DefaultDelay,
	delayMax: DefaultDelay * 16,
	converge: DefaultConverge,
}

type config struct {
	error       string
	timeout     time.Duration
	delay       time.Duration
	delayMax    time.Duration
	converge    int
	maxAttempts int
}

// Option for a retry operation.
type Option func(cfg *config)

// Timeout sets the timeout for the entire retry operation.
func Timeout(timeout time.Duration) Option {
	return func(cfg *config) {
		cfg.timeout = timeout
	}
}

// Delay sets the delay between successive retry attempts.
func Delay(delay time.Duration) Option {
	return func(cfg *config) {
		cfg.delay = delay
		cfg.delayMax = delay
	}
}

func BackoffDelay(delay time.Duration) Option {
	return func(cfg *config) {
		cfg.delay = delay
		// Currently, hardcode to 16 backoffs. We can make it configurable if needed
		cfg.delayMax = delay * 16
	}
}

// Converge sets the number of successes in a row needed to count a success.
// This is useful to avoid the case where tests like `coin.Flip() == HEADS` will always
// return success due to random variance.
func Converge(successes int) Option {
	return func(cfg *config) {
		cfg.converge = successes
	}
}

// Message defines a more detailed error message to use when failing
func Message(errorMessage string) Option {
	return func(cfg *config) {
		cfg.error = errorMessage
	}
}

// MaxAttempts allows defining a maximum number of attempts. If unset, only timeout is considered.
func MaxAttempts(attempts int) Option {
	return func(cfg *config) {
		cfg.maxAttempts = attempts
	}
}

// RetriableFunc a function that can be retried.
type RetriableFunc func() (result interface{}, completed bool, err error)

// UntilSuccess retries the given function until success, timeout, or until the passed-in function returns nil.
func UntilSuccess(fn func() error, options ...Option) error {
	_, e := UntilComplete(func() (interface{}, bool, error) {
		err := fn()
		if err != nil {
			return nil, false, err
		}

		return nil, true, nil
	}, options...)

	return e
}

// UntilSuccessOrFail calls UntilSuccess, and fails t with Fatalf if it ends up returning an error
func UntilSuccessOrFail(t test.Failer, fn func() error, options ...Option) {
	t.Helper()
	err := UntilSuccess(fn, options...)
	if err != nil {
		t.Fatalf("retry.UntilSuccessOrFail: %v", err)
	}
}

var ErrConditionNotMet = errors.New("expected condition not met")

// Until retries the given function until it returns true or hits the timeout timeout
func Until(fn func() bool, options ...Option) error {
	return UntilSuccess(func() error {
		if !fn() {
			return getErrorMessage(options)
		}
		return nil
	}, options...)
}

// UntilOrFail calls Until, and fails t with Fatalf if it ends up returning an error
func UntilOrFail(t test.Failer, fn func() bool, options ...Option) {
	t.Helper()
	err := Until(fn, options...)
	if err != nil {
		t.Fatalf("retry.UntilOrFail: %v", err)
	}
}

func getErrorMessage(options []Option) error {
	cfg := defaultConfig
	for _, option := range options {
		option(&cfg)
	}
	if cfg.error == "" {
		return ErrConditionNotMet
	}
	return errors.New(cfg.error)
}

// UntilComplete retries the given function, until there is a timeout, or until the function indicates that it has completed.
// Once complete, the returned value and error are returned.
func UntilComplete(fn RetriableFunc, options ...Option) (interface{}, error) {
	cfg := defaultConfig
	for _, option := range options {
		option(&cfg)
	}

	successes := 0
	attempts := 0
	var lasterr error
	to := time.After(cfg.timeout)
	delay := cfg.delay
	for {
		if cfg.maxAttempts > 0 && attempts >= cfg.maxAttempts {
			return nil, fmt.Errorf("hit max attempts %d attempts (last error: %v)", attempts, lasterr)
		}
		select {
		case <-to:
			return nil, fmt.Errorf("timeout while waiting after %d attempts (last error: %v)", attempts, lasterr)
		default:
		}

		result, completed, err := fn()
		attempts++
		if completed {
			if err == nil {
				successes++
			} else {
				successes = 0
			}
			if successes >= cfg.converge {
				return result, err
			}

			// Skip delay if we have a success
			continue
		}
		successes = 0
		if err != nil {
			scope.Debugf("encountered an error on attempt %d: %v", attempts, err)
			lasterr = err
		}

		select {
		case <-to:
			convergeStr := ""
			if cfg.converge > 1 {
				convergeStr = fmt.Sprintf(", %d/%d successes", successes, cfg.converge)
			}
			return nil, fmt.Errorf("timeout while waiting after %d attempts%s (last error: %v)", attempts, convergeStr, lasterr)
		case <-time.After(delay):
			delay *= 2
			if delay > cfg.delayMax {
				delay = cfg.delayMax
			}
		}
	}
}
