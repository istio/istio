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
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/operator/pkg/name"
	"istio.io/pkg/monitoring"
)

// CountCRMergeFail increments the count of CR merge failure
// for the given merge error type.
func CountCRMergeFail(reason MergeErrorType) {
	CRMergeFailures.
		With(MergeErrorLabel.Value(string(reason))).
		Increment()
}

// CountManifestRenderError increments the count of manifest
// render errors.
func CountManifestRenderError(cn name.ComponentName, reason RenderErrorType) {
	ManifestRenderErrors.
		With(ComponentNameLabel.Value(string(cn))).
		With(RenderErrorLabel.Value(string(reason))).
		Increment()
}

// CountCRFetchFail increments the count of CR fetch failure
// for a given name and the error status.
func CountCRFetchFail(name types.NamespacedName, reason metav1.StatusReason) {
	errorReason := string(reason)
	if reason == metav1.StatusReasonUnknown {
		errorReason = "unknown"
	}
	GetCRError.
		With(CRFetchErrorReasonLabel.Value(errorReason)).
		With(CRNamespacedNameLabel.Value(name.String())).
		Increment()
}

// CountManifestRender increments the count of rendered
// manifest from IstioOperator CR by component name.
func CountManifestRender(name name.ComponentName) {
	RenderManifestTotal.
		With(ComponentNameLabel.Value(string(name))).
		Increment()
}

// CountResourceCreations increments the number of K8S resources
// of a particular kind created by Operator for a CR and revision.
func CountResourceCreations(name, revision, resourceKind string) {
	incrementCount(name, revision, resourceKind, OperatorResourceCreations)
}

// CountResourceDeletions increments the number of K8S resources
// of a particular kind deleted by Operator for a CR and revision.
func CountResourceDeletions(name, revision string) {
	OperatorResourceDeletions.
		With(CRNamespacedNameLabel.Value(name)).
		With(CRRevisionLabel.Value(revision)).
		Increment()
}

// CountResourceUpdates increments the number of K8S resources
// of a particular kind updated by Operator for a CR and revision.
func CountResourceUpdates(name, revision, resourceKind string) {
	incrementCount(name, revision, resourceKind, OperatorResourceUpdates)
}

// CountResourcePrunes increments the number of K8S resources
// of a particular kind pruned by Operator
func CountResourcePrunes(name, revision, resourceKind string) {
	incrementCount(name, revision, resourceKind, OperatorResourcePrunes)
}

func incrementCount(name, revision, resourceKind string, metric monitoring.Metric) {
	metric.
		With(CRNamespacedNameLabel.Value(name)).
		With(CRRevisionLabel.Value(revision)).
		With(ResourceKindLabel.Value(resourceKind)).
		Increment()
}
