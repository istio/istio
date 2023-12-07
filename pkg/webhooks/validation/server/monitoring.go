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

package server

import (
	"strconv"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/monitoring"
)

const (
	group       = "group"
	version     = "version"
	resourceTag = "resource"
	reason      = "reason"
	status      = "status"
)

var (
	// GroupTag holds the resource group for the context.
	GroupTag = monitoring.CreateLabel(group)

	// VersionTag holds the resource version for the context.
	VersionTag = monitoring.CreateLabel(version)

	// ResourceTag holds the resource name for the context.
	ResourceTag = monitoring.CreateLabel(resourceTag)

	// ReasonTag holds the error reason for the context.
	ReasonTag = monitoring.CreateLabel(reason)

	// StatusTag holds the error code for the context.
	StatusTag = monitoring.CreateLabel(status)
)

var (
	metricValidationPassed = monitoring.NewSum(
		"galley_validation_passed",
		"Resource is valid",
	)
	metricValidationFailed = monitoring.NewSum(
		"galley_validation_failed",
		"Resource validation failed",
	)
	metricValidationHTTPError = monitoring.NewSum(
		"galley_validation_http_error",
		"Resource validation http serve errors",
	)
)

func reportValidationFailed(request *kube.AdmissionRequest, reason string, dryRun bool) {
	if dryRun {
		return
	}
	metricValidationFailed.
		With(GroupTag.Value(request.Resource.Group)).
		With(VersionTag.Value(request.Resource.Version)).
		With(ResourceTag.Value(request.Resource.Resource)).
		With(ReasonTag.Value(reason)).
		Increment()
}

func reportValidationPass(request *kube.AdmissionRequest) {
	metricValidationPassed.
		With(GroupTag.Value(request.Resource.Group)).
		With(VersionTag.Value(request.Resource.Version)).
		With(ResourceTag.Value(request.Resource.Resource)).
		Increment()
}

func reportValidationHTTPError(status int) {
	metricValidationHTTPError.
		With(StatusTag.Value(strconv.Itoa(status))).
		Increment()
}

const (
	reasonUnsupportedOperation = "unsupported_operation"
	reasonYamlDecodeError      = "yaml_decode_error"
	reasonUnknownType          = "unknown_type"
	reasonCRDConversionError   = "crd_conversion_error"
	reasonInvalidConfig        = "invalid_resource"
)
