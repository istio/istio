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

// Package metrics defines metrics and monitoring functionality
// used throughout operator.
package metrics

import (
	"istio.io/pkg/monitoring"
)

var (
	// OperatorVersionLabel describes version of running binary.
	OperatorVersionLabel = monitoring.MustCreateLabel("version")

	// MergeErrorLabel describes the type of merge error.
	MergeErrorLabel = monitoring.MustCreateLabel("error_type")

	// RenderErrorLabel describes the type of the error while rendering.
	RenderErrorLabel = monitoring.MustCreateLabel("render_error")

	// CRFetchErrorReasonLabel describes the reason/HTTP code
	// for failing to fetch CR.
	CRFetchErrorReasonLabel = monitoring.MustCreateLabel("reason")

	// ComponentNameLabel represents istio component name - like
	// core, pilot, istio-cni etc.
	ComponentNameLabel = monitoring.MustCreateLabel("component")

	// ResourceKindLabel indicates the kind of resource owned
	// or created or updated or deleted or pruned by operator.
	ResourceKindLabel = monitoring.MustCreateLabel("kind")
)

// MergeErrorType describes the class of errors that could
// occur while merging profile, user supplied YAML, values
// overridden by --set and so on.
type MergeErrorType string

const (
	// CannotFetchProfileError occurs when profile cannot be found.
	CannotFetchProfileError MergeErrorType = "cannot_fetch_profile"

	// OverlayError overlaying YAMLs to combine profile, user
	// defined settings in CR, Hub-tag etc fails.
	OverlayError MergeErrorType = "overlay"

	// IOPFormatError occurs when supplied CR cannot be marshaled
	// or unmarshaled to/from YAML.
	IOPFormatError MergeErrorType = "iop_format"

	// TranslateValuesError occurs when translating from legacy API fails.
	TranslateValuesError MergeErrorType = "translate_values"

	// InternalYAMLParseError occurs when spec section in merged CR
	// cannot be accessed for some reason (either missing or multiple).
	InternalYAMLParseError MergeErrorType = "internal_yaml_parse"
)

// RenderErrorType describes the class of errors that could
// occur while rendering Kubernetes manifest from given CR.
type RenderErrorType string

const (
	RenderNotStartedError RenderErrorType = "render_not_started"

	// HelmTranslateIOPToValuesError describes render error where renderer for
	// a component cannot create values.yaml tree from given CR.
	HelmTranslateIOPToValuesError RenderErrorType = "helm_translate_iop_to_values"

	// HelmChartRenderError describes error where Helm charts cannot be rendered
	// for the generated values.yaml tree.
	HelmChartRenderError RenderErrorType = "helm_chart_render"

	// K8SSettingsOverlayError describes the K8s overlay error after
	// rendering Helm charts successfully.
	K8SSettingsOverlayError RenderErrorType = "k8s_settings_overlay"

	// K8SManifestPatchError describes errors while patching generated manifest.
	K8SManifestPatchError RenderErrorType = "k8s_manifest_patch"
)

var (
	// Version is the version of the operator binary running currently.
	// This is required for fleet level metrics although it is available from
	// ControlZ (more precisely versionz endpoint).
	Version = monitoring.NewGauge(
		"version",
		"Version of operator binary",
		monitoring.WithLabels(OperatorVersionLabel),
	)

	// GetCRErrorTotal counts the number of times fetching
	// CR fails from API server.
	GetCRErrorTotal = monitoring.NewSum(
		"get_cr_error_total",
		"Number of times fetching CR from apiserver failed",
		monitoring.WithLabels(CRFetchErrorReasonLabel),
	)

	// CRMergeFailureTotal counts number of CR merge failures.
	CRMergeFailureTotal = monitoring.NewSum(
		"cr_merge_failure_total",
		"Number of IstioOperator CR merge failures",
		monitoring.WithLabels(MergeErrorLabel),
	)

	// CRDeletionTotal counts the number of times
	// IstioOperator CR was deleted.
	CRDeletionTotal = monitoring.NewSum(
		"cr_deletion_total",
		"Number of IstioOperator CR deleted",
	)

	// CRValidationErrorTotal counts the number of CR
	// validation failures.
	CRValidationErrorTotal = monitoring.NewSum(
		"cr_validation_error_total",
		"Number of IstioOperator CR validation failures",
	)

	// RenderManifestTotal counts the number of manifest
	// renders at each component level.
	RenderManifestTotal = monitoring.NewSum(
		"render_manifest_total",
		"Number of component manifests rendered",
		monitoring.WithLabels(ComponentNameLabel),
	)

	// OwnedResourceTotal indicates the number of resources
	// currently owned by the CR with given name and revision.
	OwnedResourceTotal = monitoring.NewGauge(
		"owned_resource_total",
		"Number of resources currently owned by the operator",
		monitoring.WithLabels(ResourceKindLabel),
	)

	// ResourceCreationTotal indicates the number of resources
	// created by the operator for a CR and revision.
	ResourceCreationTotal = monitoring.NewSum(
		"resource_creation_total",
		"Number of resources created by the operator",
		monitoring.WithLabels(ResourceKindLabel),
	)

	// ResourceUpdateTotal indicates the number of resources updated by
	// the operator in response to CR updates for a revision.
	ResourceUpdateTotal = monitoring.NewSum(
		"resource_update_total",
		"Number of resources updated by the operator",
		monitoring.WithLabels(ResourceKindLabel),
	)

	// ResourceDeletionTotal indicates the number of resources deleted
	// by the operator in response to CR update or delete operation (like
	// ingress-gateway which was enabled could be disabled and this requires
	// deleting ingress-gateway deployment).
	ResourceDeletionTotal = monitoring.NewSum(
		"resource_deletion_total",
		"Number of resources deleted by the operator",
		monitoring.WithLabels(ResourceKindLabel),
	)

	// ResourcePruneTotal indicates the resources pruned as a result of update.
	ResourcePruneTotal = monitoring.NewSum(
		"resource_prune_total",
		"Number of resources pruned by the operator",
		monitoring.WithLabels(ResourceKindLabel),
	)

	// ManifestPatchErrorTotal counts the total number of K8S patch errors.
	ManifestPatchErrorTotal = monitoring.NewSum(
		"manifest_patch_error_total",
		"Number of times K8S patch overlays failed",
	)

	// ManifestRenderErrorTotal counts errors occurred while rendering manifest.
	ManifestRenderErrorTotal = monitoring.NewSum(
		"manifest_render_error_total",
		"Number of times error occurred during rendering output manifest",
		monitoring.WithLabels(ComponentNameLabel, RenderErrorLabel),
	)

	// LegacyPathTranslationTotal counts the translations from legacy API to new one.
	LegacyPathTranslationTotal = monitoring.NewSum(
		"legacy_path_translation_total",
		"Number of times a legacy API path is translated",
	)

	// CacheFlushTotal counts number of cache flushes.
	CacheFlushTotal = monitoring.NewSum(
		"cache_flush_total",
		"number of times operator cache was flushed",
	)
)

func init() {
	monitoring.MustRegister(
		Version,

		GetCRErrorTotal,
		CRMergeFailureTotal,
		CRValidationErrorTotal,
		CRDeletionTotal,
		RenderManifestTotal,

		OwnedResourceTotal,
		ResourceCreationTotal,
		ResourceUpdateTotal,
		ResourceDeletionTotal,
		ResourcePruneTotal,

		ManifestPatchErrorTotal,
		ManifestRenderErrorTotal,
		LegacyPathTranslationTotal,
		CacheFlushTotal,
	)

	initOperatorCrdResourceMetrics()
}
