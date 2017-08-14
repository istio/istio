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

import (
	"time"

	rpc "github.com/googleapis/googleapis/google/rpc"
)

// TODO revisit the comment on this adapter struct.

// CheckResult provides return value from check request call on the handler.
type CheckResult struct {
	// The outcome status of the operation.
	Status rpc.Status
	// ValidDuration represents amount of time for which this result can be considered valid.
	ValidDuration time.Duration
	// ValidUseCount represents the number of uses for which this result can be considered valid.
	ValidUseCount int64
}
