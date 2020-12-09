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

package telemetry

import (
	"flag"
	"time"
)

var (
	// RetryDelay is the retry delay used in tests.
	RetryDelay time.Duration
	// RetryTimeout is the retry timeout used in tests.
	RetryTimeout time.Duration
)

func init() {
	flag.DurationVar(&RetryDelay, "istio.test.telemetry.retryDelay", time.Second*3, "Default retry delay used in tests")
	flag.DurationVar(&RetryTimeout, "istio.test.telemetry.retryTimeout", time.Second*80, "Default retry timeout used in tests")
}
