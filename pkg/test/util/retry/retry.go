//  Copyright 2018 Istio Authors
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
	"fmt"
	"testing"
	"time"
)

const (
	// DefaultTimeout the default timeout for the entire retry operation
	DefaultTimeout = time.Second * 30

	// DefaultDelay the default delay between successive retry attempts
	DefaultDelay = time.Millisecond * 10
)

var (
	defaultConfig = config{
		timeout: DefaultTimeout,
		delay:   DefaultDelay,
	}
)

type config struct {
	timeout time.Duration
	delay   time.Duration
}

// Option for a retry opteration.
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
	}
}

// RetriableFunc a function that can be retried.
type RetriableFunc func() (result interface{}, completed bool, err error)

// UntilSuccess retries the given function until success, timeout, or until the passed-in function returns nil.
func UntilSuccess(fn func() error, options ...Option) error {
	_, e := Do(func() (interface{}, bool, error) {
		err := fn()
		if err != nil {
			return nil, false, err
		}

		return nil, true, nil
	}, options...)

	return e
}

// UntilSuccessOrFail calls UntilSuccess, and fails t with Fatalf if it ends up returning an error
func UntilSuccessOrFail(t *testing.T, fn func() error, options ...Option) {
	t.Helper()
	err := UntilSuccess(fn, options...)
	if err != nil {
		t.Fatalf("retry.UntilSuccessOrFail: %v", err)
	}
}

// Do retries the given function, until there is a timeout, or until the function indicates that it has completed.
func Do(fn RetriableFunc, options ...Option) (interface{}, error) {
	cfg := defaultConfig
	for _, option := range options {
		option(&cfg)
	}

	var lasterr error
	to := time.After(cfg.timeout)
	for {
		select {
		case <-to:
			return nil, fmt.Errorf("timeout while waiting (last error: %v)", lasterr)
		default:
		}

		result, completed, err := fn()
		if completed {
			return result, err
		}
		if err != nil {
			lasterr = err
		}

		<-time.After(cfg.delay)
	}
}
