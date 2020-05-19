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

package validation

import (
	"fmt"
	"reflect"
	"strings"

	"istio.io/istio/operator/pkg/tpath"

	"istio.io/api/operator/v1alpha1"
	valuesv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/util"
)

const (
	validationMethodName = "Validate"
)

// ValidateConfig  calls validation func for every defined element in Values
func ValidateConfig(failOnMissingValidation bool, iopvalues map[string]interface{},
	iopls *v1alpha1.IstioOperatorSpec) (util.Errors, string) {
	var validationErrors util.Errors
	var warningMessage string
	iopvalString := util.ToYAML(iopvalues)
	values := &valuesv1alpha1.Values{}
	if err := util.UnmarshalValuesWithJSONPB(iopvalString, values, true); err != nil {
		return util.NewErrs(err), ""
	}
	validationErrors = util.AppendErrs(validationErrors, validateSubTypes(reflect.ValueOf(values).Elem(), failOnMissingValidation, values, iopls))
	// TODO: change back to return err when have other validation cases, warning for automtls check only.
	if err := validateFeatures(values, iopls).ToError(); err != nil {
		warningMessage = fmt.Sprintf("feature validation warning: %v\n", err.Error())
	}
	warningMessage += deprecatedSettingsMessage(values)
	return validationErrors, warningMessage
}

// Converts from helm paths to struct paths
// global.proxy.accessLogFormat -> Global.Proxy.AccessLogFormat
func firstCharsToUpper(s string) string {
	res := []string{}
	for _, ss := range strings.Split(s, ".") {
		res = append(res, strings.Title(ss))
	}
	return strings.Join(res, ".")
}

func deprecatedSettingsMessage(values *valuesv1alpha1.Values) string {
	messages := []string{}
	deprecations := []struct {
		old string
		new string
		// In ordered to distinguish between unset for non-pointer values, we need to specify the default value
		def interface{}
	}{
		{"global.certificates", "meshConfig.certificates", nil},
		{"global.trustDomainAliases", "meshConfig.trustDomainAliases", nil},
		{"global.outboundTrafficPolicy", "meshConfig.outboundTrafficPolicy", nil},
		{"global.localityLbSetting", "meshConfig.localityLbSetting", nil},
		{"global.policyCheckFailOpen", "meshConfig.policyCheckFailOpen", false},
		{"global.enableTracing", "meshConfig.enableTracing", false},
		{"global.proxy.accessLogFormat", "meshConfig.accessLogFormat", ""},
		{"global.proxy.accessLogFile", "meshConfig.accessLogFile", ""},
		{"global.proxy.accessLogEncoding", "meshConfig.accessLogEncoding", valuesv1alpha1.AccessLogEncoding_JSON},
		{"global.proxy.concurrency", "meshConfig.concurrency", uint32(0)},
		{"global.disablePolicyChecks", "meshConfig.disablePolicyChecks", nil},
		{"global.proxy.envoyAccessLogService", "meshConfig.envoyAccessLogService", nil},
		{"global.proxy.envoyMetricsService", "meshConfig.envoyMetricsService", nil},
		{"global.proxy.protocolDetectionTimeout", "meshConfig.protocolDetectionTimeout", ""},
		{"mixer.telemetry.reportBatchMaxEntries", "meshConfig.reportBatchMaxEntries", uint32(0)},
		{"mixer.telemetry.reportBatchMaxTime", "meshConfig.reportBatchMaxTime", ""},
		{"pilot.ingress", "meshConfig.ingressService, meshConfig.ingressControllerMode, and meshConfig.ingressClass", nil},
		{"global.mtls.enabled", "the PeerAuthentication resource", nil},
		{"global.mtls.auto", "meshConfig.enableAutoMtls", nil},
	}
	for _, d := range deprecations {
		v, f, _ := tpath.GetFromStructPath(values, firstCharsToUpper(d.old))
		if f && v != d.def {
			messages = append(messages, fmt.Sprintf("! %s is deprecated; use %s instead", d.old, d.new))
		}
	}

	return strings.Join(messages, "\n")
}

// validateFeatures check whether the config sematically make sense. For example, feature X and feature Y can't be enabled together.
func validateFeatures(_ *valuesv1alpha1.Values, _ *v1alpha1.IstioOperatorSpec) util.Errors {
	return nil
}

func validateSubTypes(e reflect.Value, failOnMissingValidation bool, values *valuesv1alpha1.Values, iopls *v1alpha1.IstioOperatorSpec) util.Errors {
	// Dealing with receiver pointer and receiver value
	ptr := e
	k := e.Kind()
	if k == reflect.Ptr || k == reflect.Interface {
		e = e.Elem()
	}
	if !e.IsValid() {
		return nil
	}
	// check for method on value
	method := e.MethodByName(validationMethodName)
	if !method.IsValid() {
		method = ptr.MethodByName(validationMethodName)
	}

	var validationErrors util.Errors
	if util.IsNilOrInvalidValue(method) {
		if failOnMissingValidation {
			validationErrors = append(validationErrors, fmt.Errorf("type %s is missing Validation method", e.Type().String()))
		}
	} else {
		r := method.Call([]reflect.Value{reflect.ValueOf(failOnMissingValidation), reflect.ValueOf(values), reflect.ValueOf(iopls)})[0].Interface().(util.Errors)
		if len(r) != 0 {
			validationErrors = append(validationErrors, r...)
		}
	}
	// If it is not a struct nothing to do, returning previously collected validation errors
	if e.Kind() != reflect.Struct {
		return validationErrors
	}
	for i := 0; i < e.NumField(); i++ {
		// Corner case of a slice of something, if something is defined type, then process it recursiveley.
		if e.Field(i).Kind() == reflect.Slice {
			validationErrors = append(validationErrors, processSlice(e.Field(i), failOnMissingValidation, values, iopls)...)
			continue
		}
		if e.Field(i).Kind() == reflect.Map {
			validationErrors = append(validationErrors, processMap(e.Field(i), failOnMissingValidation, values, iopls)...)
			continue
		}
		// Validation is not required if it is not a defined type
		if e.Field(i).Kind() != reflect.Interface && e.Field(i).Kind() != reflect.Ptr {
			continue
		}
		val := e.Field(i).Elem()
		if util.IsNilOrInvalidValue(val) {
			continue
		}
		validationErrors = append(validationErrors, validateSubTypes(e.Field(i), failOnMissingValidation, values, iopls)...)
	}

	return validationErrors
}

func processSlice(e reflect.Value, failOnMissingValidation bool, values *valuesv1alpha1.Values, iopls *v1alpha1.IstioOperatorSpec) util.Errors {
	var validationErrors util.Errors
	for i := 0; i < e.Len(); i++ {
		validationErrors = append(validationErrors, validateSubTypes(e.Index(i), failOnMissingValidation, values, iopls)...)
	}

	return validationErrors
}

func processMap(e reflect.Value, failOnMissingValidation bool, values *valuesv1alpha1.Values, iopls *v1alpha1.IstioOperatorSpec) util.Errors {
	var validationErrors util.Errors
	for _, k := range e.MapKeys() {
		v := e.MapIndex(k)
		validationErrors = append(validationErrors, validateSubTypes(v, failOnMissingValidation, values, iopls)...)
	}

	return validationErrors
}
