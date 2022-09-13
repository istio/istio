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

package framework

import (
	"flag"
	"time"
)

var (
	// TelemetryRetryDelay is the retry delay used in tests.
	TelemetryRetryDelay time.Duration
	// TelemetryRetryTimeout is the retry timeout used in tests.
	TelemetryRetryTimeout time.Duration
	// UseRealStackdriver controls whether to use real stackdriver backend for testing or not.
	UseRealStackdriver bool
)

func init() {
	flag.DurationVar(&TelemetryRetryDelay, "istio.test.telemetry.retryDelay", time.Second*2, "Default retry delay used in tests")
	flag.DurationVar(&TelemetryRetryTimeout, "istio.test.telemetry.retryTimeout", time.Second*80, "Default retry timeout used in tests")
	flag.BoolVar(&UseRealStackdriver, "istio.test.telemetry.useRealStackdriver", false,
		"controls whether to use real Stackdriver backend or not for Stackdriver integration test.")
}
