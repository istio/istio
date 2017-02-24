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

package aspect

// APIMethod constants are used to refer to the methods handled by api.Handler
type APIMethod int

// Supported API methods
const (
	CheckMethod APIMethod = iota
	ReportMethod
	QuotaMethod
)

// Name of all support API methods
const (
	CheckMethodName  = "Check"
	ReportMethodName = "Report"
	QuotaMethodName  = "Quota"
)

var apiMethodToString = map[APIMethod]string{
	CheckMethod:  CheckMethodName,
	ReportMethod: ReportMethodName,
	QuotaMethod:  QuotaMethodName,
}

// String returns the string representation of the method, or "" if an unknown method is given.
func (a APIMethod) String() string {
	return apiMethodToString[a]
}

type (
	// APIMethodArgs provides additional method-specific parameters
	APIMethodArgs interface{}

	// CheckMethodArgs is supplied by invocations of the Check method.
	CheckMethodArgs struct {
		APIMethodArgs
	}

	// ReportMethodArgs is supplied by invocations of the Report method.
	ReportMethodArgs struct {
		APIMethodArgs
	}

	// QuotaMethodArgs is supplied by invocations of the Quota method.
	QuotaMethodArgs struct {
		APIMethodArgs

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
