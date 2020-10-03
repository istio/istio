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

import "istio.io/pkg/monitoring"

var (
	// MergeErrorLabel describes the type of merge error
	MergeErrorLabel = monitoring.MustCreateLabel("error_type")
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

	// CacheFlushes counts number of cache flushes
	CacheFlushes = monitoring.NewSum(
		"operator_cache_flushes",
		"number of times operator cache was flushed",
	)
)

func init() {
	monitoring.MustRegister(
		CRMergeFailures,
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
