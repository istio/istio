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

package errdict

import (
	"strings"

	"istio.io/pkg/structured"
)

const (
	LikelyCauseFirstPrefix = "The likely cause is "
	LikeyCauseSecondPrefix = "Another possible cause could be "
)

func fixFormat(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimSuffix(s, ".")
}

func formatCauses(causes ...string) string {
	if len(causes) == 0 {
		return ""
	}
	out := LikelyCauseFirstPrefix + fixFormat(causes[0]) + "."
	for _, c := range causes[1:] {
		out += LikeyCauseSecondPrefix + fixFormat(c) + "."
	}
	return out
}

// General messages
const (
	// Boilerplate messages applicable all over the code base.

	// Action
	ActionIfErrPersistsContactSupport          = "If this error persists, " + ActionContactSupport
	ActionIfErrSureCorrectConfigContactSupport = "If you are sure your configuration is correct, " + ActionContactSupport
	ActionContactSupport                       = "contact support if using managed Istio. Otherwise check if the issue is already fixed in " +
		"another version of Istio at https://github.com/istio/istio/issues, raise an issue, or ask for help in " +
		"discuss.istio.io."

	// LikelyCause
	LikelyCauseAPIServer     = "a problem with the k8s API server"
	LikelyCauseConfiguration = "an incorrect or badly formatted configuration"
	LikelyCauseSoftwareBug   = "an issue with the Istio code"
	LikelyCauseTransience    = "If the error occurred immediately after installation, it is likely permanent."

	// Specific errors, also applicable all over the code base.

	// FailedToGetObjectFromAPIServer
	ImpactFailedToGetObjectFromAPIServer = "If the error is transient, the impact is low. If permanent, updates for " +
		"the objects cannot be processed leading to an out of sync control plane."
	LikelyCauseFailedToGetObjectFromAPIServer = "The most likely cause is a problem with the k8s API server - high " +
		"load or a transient state."
)

var (
	GeneralFailedToGetObjectFromAPIServer = &structured.Error{
		MoreInfo: "Failed to get an object from the k8s API server, because of a transient " +
			"error or the object no longer exists.",
		Impact:      ImpactFailedToGetObjectFromAPIServer,
		Action:      ActionIfErrPersistsContactSupport,
		LikelyCause: formatCauses(LikelyCauseFailedToGetObjectFromAPIServer) + " " + LikelyCauseTransience,
	}
)

// Operator specific
const (
	// NoUpdates
	OperatorImpactNoUpdates = "In this error state, changes to the IstioOperator CR will not result in the Istio " +
		"control plane being updated."
)

var (
	OperatorFailedToGetObjectInCallback = &structured.Error{
		MoreInfo: "This error occurred because a k8s update for an IstioOperator CR did not " +
			"contain an IstioOperator object.",
		Impact:      OperatorImpactNoUpdates,
		Action:      ActionIfErrPersistsContactSupport,
		LikelyCause: formatCauses(LikelyCauseAPIServer) + " " + LikelyCauseTransience,
	}
	OperatorFailedToAddFinalizer = &structured.Error{
		MoreInfo: "This error occurred because a finalizer could not be added to the IstioOperator CR. The " +
			"controller uses the finalizer to temporarily prevent the CR from being deleted while the Istio control " +
			"plane is being deleted. ",
		Impact: "The error indicates that when the IstioOperator CR is deleted, the Istio control plane may " +
			"not be fully removed.",
		Action:      "Delete and re-add the IstioOperator CR. " + ActionIfErrPersistsContactSupport,
		LikelyCause: formatCauses(LikelyCauseAPIServer),
	}
	OperatorFailedToRemoveFinalizer = &structured.Error{
		MoreInfo: "This error occurred because the finalizer set by the operator controller could not be removed " +
			"when the IstioOperator CR was deleted.",
		Impact:      "The error indicates that the IstioOperator CR can not be removed by the operator controller.",
		Action:      "Remove the IstioOperator CR finalizer manually using kubectl edit.",
		LikelyCause: formatCauses(LikelyCauseAPIServer),
	}
	OperatorFailedToMergeUserIOP = &structured.Error{
		MoreInfo: "This error occurred because the values in the selected spec.profile could not be merged with " +
			"the user IstioOperator CR.",
		Impact: "The impact of this error is that the operator controller cannot create and act upon the user " +
			"defined IstioOperator CR. The Istio control plane will not be installed or updated.",
		Action: "Check that the IstioOperator CR has the correct syntax. " +
			ActionIfErrSureCorrectConfigContactSupport,
		LikelyCause: formatCauses(LikelyCauseConfiguration, LikelyCauseSoftwareBug),
	}
)
