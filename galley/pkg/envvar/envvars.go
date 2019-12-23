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

	// SourceServerStreamBurstSize is the burst size to be used for rate limiting in the source server
	// to control how many streams can be estabilished per SOURCE_SERVER_STREAM_FREQ
	SourceServerStreamBurstSize = env.RegisterIntVar("SOURCE_SERVER_STREAM_BURST_SIZE", 100, "")

	// SourceServerStreamFreq is the frequency that is used by the rate limiter in Source Server Stream
	SourceServerStreamFreq = env.RegisterDurationVar("SOURCE_SERVER_STREAM_FREQ", time.Second, "")

	// MCPSourceReqBurstSize is the burst size to be used for rate limiting in the MCP sources to control number of
	// requests processed per stream every MCP_SOURCE_REQ_FREQ
	MCPSourceReqBurstSize = env.RegisterIntVar("MCP_SOURCE_REQ_BURST_SIZE", 100, "")

	// MCPSourceReqFreq is the frequency that is used by the rate limiter in MCP Sources
	MCPSourceReqFreq = env.RegisterDurationVar("MCP_SOURCE_REQ_FREQ", time.Second, "")
)

// RegisteredEnvVarNames returns the names of registered environment variables.
func RegisteredEnvVarNames() []string {
	return []string{
		// AuthFailure RateLimiter
		AuthzFailureLogFreq.Name,
		AuthzFailureLogBurstSize.Name,
		// SourceServer RateLimiter
		SourceServerStreamBurstSize.Name,
		SourceServerStreamFreq.Name,
		// MCP Source RateLimiter
		MCPSourceReqBurstSize.Name,
		MCPSourceReqFreq.Name,
	}
}
