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

// Package envvar is the package for Galley-wide environment variables.
package envvar

import (
	"time"

	"istio.io/pkg/env"
)

var (
	// AuthzFailureLogBurstSize is used to limit logging rate of authz failures. This controls how
	// many authz failures are logs as a burst every AUTHZ_FAILURE_LOG_FREQ.
	AuthzFailureLogBurstSize = env.RegisterIntVar("AUTHZ_FAILURE_LOG_BURST_SIZE", 1, "")

	// AuthzFailureLogFreq is used to limit logging rate of authz failures. This controls how
	// frequently bursts of authz failures are logged.
	AuthzFailureLogFreq = env.RegisterDurationVar("AUTHZ_FAILURE_LOG_FREQ", time.Minute, "")
)

// RegisteredEnvVarNames returns the names of registered environment variables.
func RegisteredEnvVarNames() []string {
	return []string{
		AuthzFailureLogFreq.Name,
		AuthzFailureLogBurstSize.Name,
	}
}
