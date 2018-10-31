// Copyright 2018 Istio Authors
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

package validation

import (
	"context"
	"strconv"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"

	"go.opencensus.io/tag"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
)

const (
	errorStr = "error"
	group    = "group"
	version  = "version"
	resource = "resource"
	reason   = "reason"
	status   = "status"
)

var (
	// ErrorTag holds the error for the context.
	ErrorTag tag.Key

	// GroupTag holds the resource group for the context.
	GroupTag tag.Key

	// VersionTag holds the resource version for the context.
	VersionTag tag.Key

	// ResourceTag holds the resource name for the context.
	ResourceTag tag.Key

	// ReasonTag holds the error reason for the context.
	ReasonTag tag.Key

	// StatusTag holds the error code for the context.
	StatusTag tag.Key
)

var (
	metricCertKeyUpdate = stats.Int64(
		"galley/validation/cert_key_updates",
		"Galley validation webhook certificate updates",
		stats.UnitDimensionless)
	metricCertKeyUpdateError = stats.Int64(
		"galley/validation/cert_key_update_errors",
		"Galley validation webhook certificate updates errors",
		stats.UnitDimensionless)
	metricValidationPassed = stats.Int64(
		"galley/validation/passed",
		"Resource is valid",
		stats.UnitDimensionless)
	metricValidationFailed = stats.Int64(
		"galley/validation/failed",
		"Resource validation failed",
		stats.UnitDimensionless)
	metricValidationHTTPError = stats.Int64(
		"galley/validation/http_error",
		"Resource validation http serve errors",
		stats.UnitDimensionless)
	metricWebhookConfigurationUpdateError = stats.Int64(
		"galley/validation/config_update_error",
		"k8s webhook configuration update error",
		stats.UnitDimensionless)
	metricWebhookConfigurationUpdates = stats.Int64(
		"galley/validation_config_updates",
		"k8s webhook configuration updates",
		stats.UnitDimensionless)
	metricWebhookConfigurationLoadError = stats.Int64(
		"galley/validation/config_load_error",
		"k8s webhook configuration (re)load error",
		stats.UnitDimensionless)
	metricWebhookConfigurationLoad = stats.Int64(
		"galley/validation/config_load",
		"k8s webhook configuration (re)loads",
		stats.UnitDimensionless)
)

func newView(measure stats.Measure, keys []tag.Key, aggregation *view.Aggregation) *view.View {
	return &view.View{
		Name:        measure.Name(),
		Description: measure.Description(),
		Measure:     measure,
		TagKeys:     keys,
		Aggregation: aggregation,
	}
}

func init() {
	var err error
	if ErrorTag, err = tag.NewKey(errorStr); err != nil {
		panic(err)
	}
	if GroupTag, err = tag.NewKey(group); err != nil {
		panic(err)
	}
	if VersionTag, err = tag.NewKey(version); err != nil {
		panic(err)
	}
	if ResourceTag, err = tag.NewKey(resource); err != nil {
		panic(err)
	}
	if ReasonTag, err = tag.NewKey(reason); err != nil {
		panic(err)
	}
	if StatusTag, err = tag.NewKey(status); err != nil {
		panic(err)
	}

	var noKeys []tag.Key
	errorKey := []tag.Key{ErrorTag}
	resourceKeys := []tag.Key{GroupTag, VersionTag, ResourceTag}
	resourceErrorKeys := []tag.Key{GroupTag, VersionTag, ResourceTag, ReasonTag}
	statusKey := []tag.Key{StatusTag}

	err = view.Register(
		newView(metricCertKeyUpdate, noKeys, view.Count()),
		newView(metricCertKeyUpdateError, errorKey, view.Count()),
		newView(metricValidationPassed, resourceKeys, view.Count()),
		newView(metricValidationFailed, resourceErrorKeys, view.Count()),
		newView(metricValidationHTTPError, statusKey, view.Count()),
		newView(metricWebhookConfigurationUpdateError, errorKey, view.Count()),
		newView(metricWebhookConfigurationUpdates, noKeys, view.Count()),
		newView(metricWebhookConfigurationLoadError, errorKey, view.Count()),
		newView(metricWebhookConfigurationLoad, noKeys, view.Count()),
	)

	if err != nil {
		panic(err)
	}
}

func reportValidationFailed(request *admissionv1beta1.AdmissionRequest, reason string) {
	ctx, err := tag.New(context.Background(),
		tag.Insert(GroupTag, request.Resource.Group),
		tag.Insert(VersionTag, request.Resource.Version),
		tag.Insert(ResourceTag, request.Resource.Resource),
		tag.Insert(ReasonTag, reason))
	if err != nil {
		scope.Errorf("Error creating monitoring context for reportValidationFailed: %v", err)
	} else {
		stats.Record(ctx, metricValidationFailed.M(1))
	}
}

func reportValidationPass(request *admissionv1beta1.AdmissionRequest) {
	ctx, err := tag.New(context.Background(),
		tag.Insert(GroupTag, request.Resource.Group),
		tag.Insert(VersionTag, request.Resource.Version),
		tag.Insert(ResourceTag, request.Resource.Resource))
	if err != nil {
		scope.Errorf("Error creating monitoring context for reportValidationPass: %v", err)
	} else {
		stats.Record(ctx, metricValidationPassed.M(1))
	}
}

func reportValidationHTTPError(status int) {
	ctx, err := tag.New(context.Background(), tag.Insert(StatusTag, strconv.Itoa(status)))
	if err != nil {
		scope.Errorf("Error creating monitoring context for reportValidationHTTPError: %v", err)
	} else {
		stats.Record(ctx, metricValidationHTTPError.M(1))
	}
}

func reportValidationConfigUpdateError(err error) {
	ctx, err := tag.New(context.Background(), tag.Insert(ErrorTag, err.Error()))
	if err != nil {
		scope.Errorf("Error creating monitoring context for reportValidationConfigUpdateError: %v", err)
	} else {
		stats.Record(ctx, metricWebhookConfigurationUpdateError.M(1))
	}
}

func reportValidationConfigLoadError(err error) {
	ctx, err := tag.New(context.Background(), tag.Insert(ErrorTag, err.Error()))
	if err != nil {
		scope.Errorf("Error creating monitoring context for reportValidationConfigLoadError: %v", err)
	} else {
		stats.Record(ctx, metricWebhookConfigurationLoadError.M(1))
	}
}

func reportValidationConfigLoad() {
	stats.Record(context.Background(), metricWebhookConfigurationLoad.M(1))
}

func reportValidationConfigUpdate() {
	stats.Record(context.Background(), metricWebhookConfigurationUpdates.M(1))
}

func reportValidationCertKeyUpdate() {
	stats.Record(context.Background(), metricCertKeyUpdate.M(1))
}

func reportValidationCertKeyUpdateError(err error) {
	ctx, err := tag.New(context.Background(), tag.Insert(ErrorTag, err.Error()))
	if err != nil {
		scope.Errorf("Error creating monitoring context for reportValidationCertKeyUpdateError: %v", err)
	} else {
		stats.Record(ctx, metricCertKeyUpdateError.M(1))
	}
}

const (
	reasonUnsupportedOperation = "unsupported_operation"
	reasonYamlDecodeError      = "yaml_decode_error"
	reasonUnknownType          = "unknown_type"
	reasonCRDConversionError   = "crd_conversion_error"
	reasonInvalidConfig        = "invalid_resource"
)
