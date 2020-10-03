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

package istiocontrolplane

import "istio.io/pkg/monitoring"

var (
	mergeErrorType = monitoring.MustCreateLabel("error_type")
)

const (
	// Occurs when profile cannot be found
	cannotFindProfileError = "fetch_profile"

	// Once hub-tag overlay is generated, we need to merge it with
	// the starting profile. This error is reported if it fails
	cannotMergeProfileWithHubTagError = "overlay_hub_tag_on_profile"

	// We need to overlay the configuration defined by the user
	// with IstioOperator (IOP) CR. If rendering to YAML fails
	// then this error is reported.
	cannotMarshalUserIOPError = "user_iop_marshal"

	// Finally the user defined CR is overlaid on hub-tag and then
	// profile. After that we also overlay values passed by --set.
	// When it fails, we report this error
	cannotMergeFileOverlayWithProfileError = "overlay_file_on_profile"

	// After doing all the hard work, we can get this error if we
	// cannot get the spec subtree of IstioOperator CR.
	cannotGetSpecSubtreeError = "spec_subtree"
)

var (
	metricOperatorCRMergeFailures = monitoring.NewSum(
		"operator_cr_merge_failures",
		"Number of IstioOperator CR merge failures",
		monitoring.WithLabels(mergeErrorType),
	)
)

func init() {
	monitoring.MustRegister(
		metricOperatorCRMergeFailures,
	)
}

func countCRMergeFail(reason string) {
	metricOperatorCRMergeFailures.
		With(mergeErrorType.Value(reason)).
		Increment()
}
