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
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
)

var (
	metricCertKeyUpdate = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "galley_validation_cert_key_updates",
		Help: "Galley validation webhook certiticate updates",
	})
	metricCertKeyUpdateError = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "galley_validation_cert_key_update_errors",
		Help: "Galley validation webhook certiticate updates errors",
	}, []string{"error"})
	metricValidationPassed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "galley_validation_passed",
		Help: "Resource is valid",
	}, []string{"group", "version", "resource"})
	metricValidationFailed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "galley_validation_failed",
		Help: "Resource validation failed",
	}, []string{"group", "version", "resource", "reason"})
	metricValidationHTTPError = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "galley_validation_http_error",
		Help: "Resource validation http serve errors",
	}, []string{"status"})
)

func init() {
	prometheus.MustRegister(
		metricCertKeyUpdate,
		metricCertKeyUpdateError,
		metricValidationPassed,
		metricValidationFailed,
		metricValidationHTTPError)
}

func reportValidationFailed(request *admissionv1beta1.AdmissionRequest, reason string) {
	metricValidationFailed.With(prometheus.Labels{
		"group":    request.Resource.Group,
		"version":  request.Resource.Version,
		"resource": request.Resource.Resource,
		"reason":   reason,
	}).Add(1)
}

func reportValidationPass(request *admissionv1beta1.AdmissionRequest) {
	metricValidationPassed.With(prometheus.Labels{
		"group":    request.Resource.Group,
		"version":  request.Resource.Version,
		"resource": request.Resource.Resource,
	}).Add(1)
}

func reportValidationHTTPError(status int) {
	metricValidationHTTPError.With(prometheus.Labels{
		"status": strconv.Itoa(status),
	}).Add(1)
}

const (
	reasonUnsupportedOperation = "unsupported_operation"
	reasonYamlDecodeError      = "yaml_decode_error"
	reasonUnknownType          = "unknown_type"
	reasonCRDConversionError   = "crd_conversion_error"
	reasonInvalidConfig        = "invalid_resource"
)
