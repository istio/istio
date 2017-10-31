// Copyright 2017 Istio Authors
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

package adapter

type (
	// AccessLogsAspect handles access log data within Mixer.
	AccessLogsAspect interface {
		Aspect

		// LogAccess directs a backend adapter to process a batch of
		// access log entries derived from potentially several Report()
		// calls. LogEntries generated for this Aspect will only have
		// the fields LogName, Labels, and TextPayload populated.
		// TextPayload will contain the generated access log string,
		// based on the aspect configuration.
		LogAccess([]LogEntry) error
	}

	// AccessLogsBuilder builds instances of the AccessLogger aspect.
	AccessLogsBuilder interface {
		Builder

		// NewAccessLogsAspect returns a new instance of the AccessLogger aspect.
		NewAccessLogsAspect(env Env, c Config) (AccessLogsAspect, error)
	}
)
