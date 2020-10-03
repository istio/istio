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
	"istio.io/istio/operator/pkg/name"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// CountCRMergeFail increments the count of CR merge failure
// for the given merge error type
func CountCRMergeFail(reason MergeErrorType) {
	CRMergeFailures.
		With(MergeErrorLabel.Value(string(reason))).
		Increment()
}

// CountCRFetchFail increments the count of CR fetch failure
// for a given name and the error status
func CountCRFetchFail(name types.NamespacedName, reason metav1.StatusReason) {
	errorReason := string(reason)
	if reason == metav1.StatusReasonUnknown {
		errorReason = "unknown"
	}
	FetchCRError.
		With(CRFetchErrorReasonLabel.Value(errorReason)).
		With(CRFetchNamespacedNameLabel.Value(name.String())).
		Increment()
}

// CountManifestRender increments the count of rendered
// manifest from IstioOperator CR by component name
func CountManifestRender(name name.ComponentName) {
	RenderManifestCount.
		With(ComponentNameLabel.Value(string(name))).
		Increment()
}
