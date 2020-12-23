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

	"istio.io/istio/operator/pkg/name"
)

// CountCRMergeFail increments the count of CR merge failure
// for the given merge error type.
func CountCRMergeFail(reason MergeErrorType) {
	CRMergeFailureTotal.
		With(MergeErrorLabel.Value(string(reason))).
		Increment()
}

// CountManifestRenderError increments the count of manifest
// render errors.
func CountManifestRenderError(cn name.ComponentName, reason RenderErrorType) {
	ManifestRenderErrorTotal.
		With(ComponentNameLabel.Value(string(cn))).
		With(RenderErrorLabel.Value(string(reason))).
		Increment()
}

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

// CountManifestRender increments the count of rendered
// manifest from IstioOperator CR by component name.
func CountManifestRender(name name.ComponentName) {
	RenderManifestTotal.
		With(ComponentNameLabel.Value(string(name))).
		Increment()
}
