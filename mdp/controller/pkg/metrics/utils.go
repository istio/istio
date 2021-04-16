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

package metrics

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CountCRFetchFail increments the count of CR fetch failure
// for a given name and the error status.
func CountCRFetchFail(reason metav1.StatusReason) {
	errorReason := string(reason)
	if reason == metav1.StatusReasonUnknown {
		errorReason = "unknown"
	}
	GetCRErrorTotal.
		With(CRFetchErrorReasonLabel.Value(errorReason)).
		Increment()
}
