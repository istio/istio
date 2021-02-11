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

// General boilerplate.
const (
	// Boilerplate messages applicable all over the code base.

	// Action
	ActionIfErrPersistsCheckBugList            = "If this error persists, " + ActionCheckBugList
	ActionIfErrSureCorrectConfigContactSupport = "If you are sure your configuration is correct, " + ActionCheckBugList
	ActionCheckBugList                         = "see https://istio.io/latest/about/bugs for possible solutions."

	// LikelyCause
	LikelyCauseAPIServer     = "a problem with the Kubernetes API server"
	LikelyCauseConfiguration = "an incorrect or badly formatted configuration"
	LikelyCauseSoftwareBug   = "an issue with the Istio code"

	// Is the error permanent?
	TransiencePermanentForInstall = "If the error occurred immediately after installation, it is likely permanent."
)

// Operator specific
const (
	// Impact
	OperatorImpactFailedToGetObjectFromAPIServer = "If the error is transient, the impact is low. If permanent, " +
		"updates for the objects cannot be processed leading to an out of sync control plane."
	OperatorImpactNoUpdates = "In this error state, changes to the IstioOperator CR will not result in the Istio " +
		"control plane being updated."
)

var (
	OperatorFailedToGetObjectFromAPIServer = &structured.Error{
		MoreInfo: "Failed to get an object from the Kubernetes API server, because of a transient " +
			"API server error or the object no longer exists.",
		Impact:      OperatorImpactFailedToGetObjectFromAPIServer,
		LikelyCause: formatCauses(LikelyCauseAPIServer) + " " + TransiencePermanentForInstall,
		Action: "If the error is because the object was deleted, it can be safely ignored. Otherwise, if the " +
			"error persists, " + ActionCheckBugList,
	}
	OperatorFailedToGetObjectInCallback = &structured.Error{
		MoreInfo: "A Kubernetes update for an IstioOperator resource did not " +
			"contain an IstioOperator object.",
		Impact:      OperatorImpactNoUpdates,
		LikelyCause: formatCauses(LikelyCauseAPIServer) + " " + TransiencePermanentForInstall,
		Action:      ActionIfErrPersistsCheckBugList,
	}
	OperatorFailedToAddFinalizer = &structured.Error{
		MoreInfo: "A finalizer could not be added to the IstioOperator resource. The " +
			"controller uses the finalizer to temporarily prevent the resource from being deleted while the Istio " +
			"control plane is being deleted.",
		Impact: "When the IstioOperator resource is deleted, the Istio control plane may " +
			"not be fully removed.",
		LikelyCause: formatCauses(LikelyCauseAPIServer),
		Action:      "Delete and re-add the IstioOperator resource. " + ActionIfErrPersistsCheckBugList,
	}
	OperatorFailedToRemoveFinalizer = &structured.Error{
		MoreInfo: "The finalizer set by the operator controller could not be removed " +
			"when the IstioOperator resource was deleted.",
		Impact:      "The IstioOperator resource can not be removed by the operator controller.",
		LikelyCause: formatCauses(LikelyCauseAPIServer),
		Action:      "Remove the IstioOperator resource finalizer manually using kubectl edit.",
	}
	OperatorFailedToMergeUserIOP = &structured.Error{
		MoreInfo: "The values in the selected spec.profile could not be merged with " +
			"the user IstioOperator resource.",
		Impact: "The operator controller cannot create and act upon the user " +
			"defined IstioOperator resource. The Istio control plane will not be installed or updated.",
		LikelyCause: formatCauses(LikelyCauseConfiguration, LikelyCauseSoftwareBug),
		Action: "Check that the IstioOperator resource has the correct syntax. " +
			ActionIfErrSureCorrectConfigContactSupport,
	}
	OperatorFailedToConfigure = &structured.Error{
		MoreInfo: "the IstioOperator Resource could not be applied on the cluster " +
			"because of incompatible Kubernetes settings",
		Impact:      OperatorImpactNoUpdates,
		LikelyCause: formatCauses(LikelyCauseConfiguration),
		Action: "Esnure that IstioOperator config is compatible with current Kubernetes version." +
			ActionIfErrSureCorrectConfigContactSupport,
	}
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
