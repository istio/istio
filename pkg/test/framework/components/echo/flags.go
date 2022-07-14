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

package echo

import (
	"flag"
	"time"

	"istio.io/istio/pkg/test/util/retry"
)

var (
	callTimeout      = 20 * time.Second
	callDelay        = 10 * time.Millisecond
	callConverge     = 3
	readinessTimeout = 10 * time.Minute
	callsPerWorkload = 5
)

// init registers the command-line flags that we can exposed for "go test".
func init() {
	flag.DurationVar(&callTimeout, "istio.test.echo.callTimeout", callTimeout,
		"Specifies the default total timeout used when retrying calls to the Echo service")
	flag.DurationVar(&callDelay, "istio.test.echo.callDelay", callDelay,
		"Specifies the default delay between successive retry attempts when calling the Echo service")
	flag.IntVar(&callConverge, "istio.test.echo.callConverge", callConverge,
		"Specifies the number of successive retry attempts that must be successful when calling the Echo service")
	flag.DurationVar(&readinessTimeout, "istio.test.echo.readinessTimeout", readinessTimeout,
		"Specifies the default timeout for echo readiness check")
	flag.IntVar(&callsPerWorkload, "istio.test.echo.callsPerWorkload", callsPerWorkload,
		"Specifies the number of calls that will be made for each target workload. "+
			"Only applies if the call count is zero (default) and a target was specified for the call")
}

// DefaultCallRetryOptions returns the default call retry options as specified in command-line flags.
func DefaultCallRetryOptions() []retry.Option {
	return []retry.Option{retry.Timeout(callTimeout), retry.BackoffDelay(callDelay), retry.Converge(callConverge)}
}

// DefaultReadinessTimeout returns the default echo readiness check timeout.
func DefaultReadinessTimeout() time.Duration {
	return readinessTimeout
}

// DefaultCallsPerWorkload returns the number of calls that should be made per target workload by default.
func DefaultCallsPerWorkload() int {
	return callsPerWorkload
}
