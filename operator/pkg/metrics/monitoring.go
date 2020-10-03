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
	"istio.io/pkg/monitoring"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	// MergeErrorLabel describes the type of merge error
	MergeErrorLabel = monitoring.MustCreateLabel("error_type")
)

var (
	// CRFetchErrorReasonLabel describes the reason/HTTP code
	// for failing to fetch CR
	CRFetchErrorReasonLabel = monitoring.MustCreateLabel("reason")

	// CRFetchNamespacedNameLabel describes the name with namespace
	CRFetchNamespacedNameLabel = monitoring.MustCreateLabel("name")
)

// MergeErrorType describes the class of errors that could
// occur while merging profile, user supplied YAML, values
// overriden by --set and so on
type MergeErrorType string

const (
	// CannotFindProfileError occurs when profile cannot be found
	CannotFindProfileError MergeErrorType = "fetch_profile"

	// CannotMergeProfileWithHubTagError occurs when merging profile
	// and Hub-Tag overlay fails
	CannotMergeProfileWithHubTagError MergeErrorType = "overlay_hub_tag_on_profile"

	// CannotMarshalUserIOPError occurs when user supplied CR cannot
	// be marshaled to YAML string
	CannotMarshalUserIOPError MergeErrorType = "user_iop_marshal"

	// CannotMergeFileOverlayWithProfileError occurs when file overlay
	// supplied by user cannot be overlaid on profile+hub/tag overlay
	CannotMergeFileOverlayWithProfileError MergeErrorType = "overlay_file_on_profile"

	// CannotGetSpecSubtreeError occurs when spec section in merged CR
	// cannot be accessed for some reason (either missing or multiple)
	CannotGetSpecSubtreeError MergeErrorType = "spec_subtree"
)

var (
	// FetchCRError counts the number of times fetching
	// CR fails from API server
	FetchCRError = monitoring.NewSum(
		"operator_get_cr_errors",
		"Number of times fetching CR from apiserver failed",
		monitoring.WithLabels(CRFetchErrorReasonLabel, CRFetchNamespacedNameLabel),
	)

	// CRMergeFailures counts number of CR merge failures
	CRMergeFailures = monitoring.NewSum(
		"operator_cr_merge_failures",
		"Number of IstioOperator CR merge failures",
		monitoring.WithLabels(MergeErrorLabel),
	)

	// CRDeletions counts the number of times
	// IstioOperator CR was deleted
	CRDeletions = monitoring.NewSum(
		"operator_cr_deletions",
		"Number of IstioOperator CR deleted",
	)

	// CRValidationFailure counts the number of CR
	// validation failures
	CRValidationFailure = monitoring.NewSum(
		"operator_cr_validation_errors",
		"Number of IstioOperator CR validation failures",
	)

	// CacheFlushes counts number of cache flushes
	CacheFlushes = monitoring.NewSum(
		"operator_cache_flushes",
		"number of times operator cache was flushed",
	)
)

func init() {
	monitoring.MustRegister(
		FetchCRError,
		CRMergeFailures,
		CRValidationFailure,
		CRDeletions,
		CacheFlushes,
	)
}

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
