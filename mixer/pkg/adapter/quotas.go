// Copyright Istio Authors
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

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/istio/mixer/pkg/status"
)

type (

	// QuotaArgs supplies the arguments for quota operations.
	QuotaArgs struct {
		// DeduplicationID is used for deduplicating quota allocation/free calls in the case of
		// failed RPCs and retries. This should be a UUID per call, where the same
		// UUID is used for retries of the same quota allocation or release call.
		DeduplicationID string

		// The amount of quota being allocated or released.
		QuotaAmount int64

		// If true, allows a response to return less quota than requested. When
		// false, the exact requested amount is returned or 0 if not enough quota
		// was available.
		BestEffort bool
	}

	// QuotaResult provides return values from quota allocation calls on the handler
	QuotaResult struct {
		// The outcome status of the operation.
		Status rpc.Status

		// The amount of time until which the returned quota expires, this is 0 for non-expiring quotas.
		ValidDuration time.Duration

		// The total amount of quota returned, may be less than requested.
		Amount int64
	}
)

// IsDefault returns true if the QuotaResult is in its zero state
func (r *QuotaResult) IsDefault() bool {
	return status.IsOK(r.Status) && r.ValidDuration == 0 && r.Amount == 0
}
