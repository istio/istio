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
	"fmt"
	"time"

	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/status"
)

// TODO revisit the comment on this adapter struct.

// CheckResult provides return value from check request call on the handler.
type CheckResult struct {
	// The outcome status of the operation.
	Status rpc.Status
	// ValidDuration represents amount of time for which this result can be considered valid.
	ValidDuration time.Duration
	// ValidUseCount represents the number of uses for which this result can be considered valid.
	ValidUseCount int32
}

// GetStatus gets status embedded in the result.
func (r *CheckResult) GetStatus() rpc.Status { return r.Status }

// SetStatus embeds status in result.
func (r *CheckResult) SetStatus(s rpc.Status) { r.Status = s }

// Combine combines other result with self. It does not handle
// Status.
func (r *CheckResult) Combine(otherPtr interface{}) interface{} {
	if otherPtr == nil {
		return r
	}
	other := otherPtr.(*CheckResult)
	if r.ValidDuration > other.ValidDuration {
		r.ValidDuration = other.ValidDuration
	}
	if r.ValidUseCount > other.ValidUseCount {
		r.ValidUseCount = other.ValidUseCount
	}
	return r
}

func (r *CheckResult) String() string {
	return fmt.Sprintf("CheckResult: status:%d, duration:%d, usecount:%d", status.String(r.Status), r.ValidDuration, r.ValidDuration)
}
