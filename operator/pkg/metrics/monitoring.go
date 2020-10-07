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
)

// OperatorVersionLabel describes version of running binary
var OperatorVersionLabel = monitoring.MustCreateLabel("v")

// MergeErrorLabel describes the type of merge error
var MergeErrorLabel = monitoring.MustCreateLabel("error_type")

// PathLabel describes translated path
var PathLabel = monitoring.MustCreateLabel("path")

var (
	// CRFetchErrorReasonLabel describes the reason/HTTP code
	// for failing to fetch CR
	CRFetchErrorReasonLabel = monitoring.MustCreateLabel("reason")

	// CRNamespacedNameLabel describes the name with namespace
	CRNamespacedNameLabel = monitoring.MustCreateLabel("name")
)

var (
	// ComponentNameLabel represents istio component name - like
	// core, pilot, istio-cni etc.
	ComponentNameLabel = monitoring.MustCreateLabel("component")

	// ResourceKindLabel indicates the kind of resource owned
	// or created or updated or deleted or pruned by operator
	ResourceKindLabel = monitoring.MustCreateLabel("kind")

	// CRRevisionLabel indicates the revision in the CR
	CRRevisionLabel = monitoring.MustCreateLabel("revision")
)

// MergeErrorType describes the class of errors that could
// occur while merging profile, user supplied YAML, values
// overridden by --set and so on
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
	// OperatorVersion is the version of the operator binary running currently.
	// This is required for fleet level metrics although it is available from
	// ControlZ (more precisely versionz endpoint)
	OperatorVersion = monitoring.NewGauge(
		"operator_version",
		"Version of operator binary",
		monitoring.WithLabels(OperatorVersionLabel),
	)

	// FetchCRError counts the number of times fetching
	// CR fails from API server
	FetchCRError = monitoring.NewSum(
		"operator_get_cr_errors",
		"Number of times fetching CR from apiserver failed",
		monitoring.WithLabels(CRFetchErrorReasonLabel, CRNamespacedNameLabel),
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

	// RenderManifestCount counts the number of manifest
	// renders at each component level
	RenderManifestCount = monitoring.NewSum(
		"operator_render_manifest_count",
		"Number of component manifests rendered",
		monitoring.WithLabels(ComponentNameLabel),
	)

	// OperatorResourceCount indicates the number of resources
	// currently owned by the CR with given name and revision
	OperatorResourceCount = monitoring.NewGauge(
		"operator_resource_count",
		"Number of resources currently owned by the operator",
		monitoring.WithLabels(ResourceKindLabel, CRRevisionLabel, CRNamespacedNameLabel),
	)

	// OperatorResourceCreations indicates the number of resources
	// created by the operator for a CR and revision
	OperatorResourceCreations = monitoring.NewSum(
		"operator_resource_creations",
		"Number of resources created by the operator",
		monitoring.WithLabels(ResourceKindLabel, CRRevisionLabel, CRNamespacedNameLabel),
	)

	// OperatorResourceUpdates indicates the number of resources updated by
	// the operator in response to CR updates for a revision
	OperatorResourceUpdates = monitoring.NewSum(
		"operator_resource_updates",
		"Number of resources updated by the operator",
		monitoring.WithLabels(ResourceKindLabel, CRRevisionLabel, CRNamespacedNameLabel),
	)

	// OperatorResourceDeletions indicates the number of resources deleted
	// by the operator in response to CR update or delete operation (like
	// ingress-gateway which was enabled could be disabled and this requires
	// deleting ingress-gateway deployment)
	OperatorResourceDeletions = monitoring.NewSum(
		"operator_resource_deletions",
		"Number of resources deleted by the operator",
		monitoring.WithLabels(ResourceKindLabel, CRRevisionLabel, CRNamespacedNameLabel),
	)

	// OperatorResourcePrunes indicates the resources pruned as a result of update
	OperatorResourcePrunes = monitoring.NewSum(
		"operator_resource_prunes",
		"Number of resources pruned by the operator",
		monitoring.WithLabels(ResourceKindLabel, CRRevisionLabel, CRNamespacedNameLabel),
	)

	// K8SPatchOverlayErrors counts the total number of K8S patch errors
	K8SPatchOverlayErrors = monitoring.NewSum(
		"operator_patch_errors",
		"Number of times K8S patch overlays failed",
	)

	// OperatorPathTranslations counts the translations from legacy API to new one
	OperatorPathTranslations = monitoring.NewSum(
		"operator_path_translations",
		"Number of times a legacy API path is translated",
		monitoring.WithLabels(PathLabel),
	)

	// CacheFlushes counts number of cache flushes
	CacheFlushes = monitoring.NewSum(
		"operator_cache_flushes",
		"number of times operator cache was flushed",
	)
)

func init() {
	monitoring.MustRegister(
		OperatorVersion,

		FetchCRError,
		CRMergeFailures,
		CRValidationFailure,
		CRDeletions,
		RenderManifestCount,

		OperatorResourceCount,
		OperatorResourceCreations,
		OperatorResourceUpdates,
		OperatorResourceDeletions,
		OperatorResourcePrunes,

		K8SPatchOverlayErrors,
		OperatorPathTranslations,
		CacheFlushes,
	)
}
