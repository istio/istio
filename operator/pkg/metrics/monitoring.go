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

// MergeErrorType describes the class of errors that could
// occur while merging profile, user supplied YAML, values
// overridden by --set and so on.
type MergeErrorType string

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
