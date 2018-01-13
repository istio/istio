// Copyright 2016 Istio Authors
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

package runtime

type (
	// QuotaMethodArgs is supplied by invocations of the Quota method.
	QuotaMethodArgs struct {
		// Used for deduplicating quota allocation/free calls in the case of
		// failed RPCs and retries. This should be a UUID per call, where the same
		// UUID is used for retries of the same quota allocation call.
		DeduplicationID string

		// The quota to allocate from.
		Quota string

		// The amount of quota to allocate.
		Amount int64

		// If true, allows a response to return less quota than requested. When
		// false, the exact requested amount is returned or 0 if not enough quota
		// was available.
		BestEffort bool
	}
)
