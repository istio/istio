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
	"unicode"

	"istio.io/api/operator/v1alpha1"
	valuesv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
)

const (
	validationMethodName = "Validate"
)

type deprecatedSettings struct {
	old string
	new string
	// In ordered to distinguish between unset for non-pointer values, we need to specify the default value
	def interface{}
}

// ValidateConfig  calls validation func for every defined element in Values
func ValidateConfig(failOnMissingValidation bool, iopls *v1alpha1.IstioOperatorSpec) (util.Errors, string) {
	var validationErrors util.Errors
	var warningMessage string
	iopvalString := util.ToYAML(iopls.Values)
	values := &valuesv1alpha1.Values{}
	if err := util.UnmarshalValuesWithJSONPB(iopvalString, values, true); err != nil {
		return util.NewErrs(err), ""
	}

	validationErrors = util.AppendErrs(validationErrors, ValidateSubTypes(reflect.ValueOf(values).Elem(), failOnMissingValidation, values, iopls))
	// TODO: change back to return err when have other validation cases, warning for automtls check only.
	if err := validateFeatures(values, iopls).ToError(); err != nil {
		warningMessage = fmt.Sprintf("feature validation warning: %v\n", err.Error())
	}
	warningMessage += deprecatedSettingsMessage(iopls)
	return validationErrors, warningMessage
}

// Converts from struct paths to helm paths
// Global.Proxy.AccessLogFormat -> global.proxy.accessLogFormat
func firstCharsToLower(s string) string {
	// Use a closure here to remember state.
	// Hackish but effective. Depends on Map scanning in order and calling
	// the closure once per rune.
	prev := '.'
	return strings.Map(
		func(r rune) rune {
			if prev == '.' {
				prev = r
				return unicode.ToLower(r)
			}
			prev = r
			return r
		},
		s)
}

func deprecatedSettingsMessage(iop *v1alpha1.IstioOperatorSpec) string {
	messages := []string{}
	deprecations := []deprecatedSettings{
		{"Values.global.certificates", "meshConfig.certificates", nil},
		{"Values.global.trustDomainAliases", "meshConfig.trustDomainAliases", nil},
		{"Values.global.outboundTrafficPolicy", "meshConfig.outboundTrafficPolicy", nil},
		{"Values.global.localityLbSetting", "meshConfig.localityLbSetting", nil},
		{"Values.global.policyCheckFailOpen", "meshConfig.policyCheckFailOpen", false},
		{"Values.global.enableTracing", "meshConfig.enableTracing", false},
		{"Values.global.proxy.accessLogFormat", "meshConfig.accessLogFormat", ""},
		{"Values.global.proxy.accessLogFile", "meshConfig.accessLogFile", ""},
		{"Values.global.proxy.concurrency", "meshConfig.defaultConfig.concurrency", uint32(0)},
		{"Values.global.proxy.envoyAccessLogService", "meshConfig.defaultConfig.envoyAccessLogService", nil},
		{"Values.global.proxy.envoyAccessLogService.enabled", "meshConfig.enableEnvoyAccessLogService", nil},
		{"Values.global.proxy.envoyMetricsService", "meshConfig.defaultConfig.envoyMetricsService", nil},
		{"Values.global.proxy.protocolDetectionTimeout", "meshConfig.protocolDetectionTimeout", ""},
		{"Values.pilot.ingress", "meshConfig.ingressService, meshConfig.ingressControllerMode, and meshConfig.ingressClass", nil},
		{"Values.global.mtls.enabled", "the PeerAuthentication resource", nil},
		{"Values.global.mtls.auto", "meshConfig.enableAutoMtls", nil},
		{"Values.grafana.enabled", "the samples/addons/ deployments", false},
		{"Values.tracing.enabled", "the samples/addons/ deployments", false},
		{"Values.kiali.enabled", "the samples/addons/ deployments", false},
		{"Values.prometheus.enabled", "the samples/addons/ deployments", false},
		{"Values.global.tracer.lightstep.address", "meshConfig.defaultConfig.tracing.lightstep.address", ""},
		{"Values.global.tracer.lightstep.accessToken", "meshConfig.defaultConfig.tracing.lightstep.accessToken", ""},
		{"Values.global.tracer.zipkin.address", "meshConfig.defaultConfig.tracing.zipkin.address", nil},
		{"Values.global.tracer.stackdriver.debug", "meshConfig.defaultConfig.tracing.stackdriver.debug", false},
		{"Values.global.tracer.stackdriver.maxNumberOfAttributes", "meshConfig.defaultConfig.tracing.stackdriver.maxNumberOfAttributes", 0},
		{"Values.global.tracer.stackdriver.maxNumberOfAnnotations", "meshConfig.defaultConfig.tracing.stackdriver.maxNumberOfAnnotations", 0},
		{"Values.global.tracer.stackdriver.maxNumberOfMessageEvents", "meshConfig.defaultConfig.tracing.stackdriver.maxNumberOfMessageEvents", 0},
		{"Values.global.tracer.datadog.address", "meshConfig.defaultConfig.tracing.datadog.address", ""},
		{"Values.global.meshExpansion.enabled", "Gateway and other Istio networking resources, such as in samples/istiod-gateway/", false},
		{"Values.gateways.istio-ingressgateway.meshExpansionPorts", "components.ingressGateways[name=istio-ingressgateway].k8s.service.ports", nil},
		{"AddonComponents.grafana.Enabled", "the samples/addons/ deployments", false},
		{"AddonComponents.tracing.Enabled", "the samples/addons/ deployments", false},
		{"AddonComponents.kiali.Enabled", "the samples/addons/ deployments", false},
		{"AddonComponents.prometheus.Enabled", "the samples/addons/ deployments", false},
	}
	for _, d := range deprecations {
		// Grafana is a special case where its just an interface{}. A better fix would probably be defining
		// the types, but since this is deprecated this is easier
		v, f, _ := tpath.GetFromStructPath(iop, d.old)
		if f {
			switch t := v.(type) {
			// need to do conversion for bool value defined in IstioOperator component spec.
			case *v1alpha1.BoolValueForPB:
				v = t.Value
			}
			if v != d.def {
				messages = append(messages, fmt.Sprintf("! %s is deprecated; use %s instead", firstCharsToLower(d.old), d.new))
			}
		}
	}

	return strings.Join(messages, "\n")
}

// validateFeatures check whether the config sematically make sense. For example, feature X and feature Y can't be enabled together.
func validateFeatures(_ *valuesv1alpha1.Values, _ *v1alpha1.IstioOperatorSpec) util.Errors {
	return nil
}

func ValidateSubTypes(e reflect.Value, failOnMissingValidation bool, values *valuesv1alpha1.Values, iopls *v1alpha1.IstioOperatorSpec) util.Errors {
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
		validationErrors = append(validationErrors, ValidateSubTypes(e.Field(i), failOnMissingValidation, values, iopls)...)
	}

	return validationErrors
}

func processSlice(e reflect.Value, failOnMissingValidation bool, values *valuesv1alpha1.Values, iopls *v1alpha1.IstioOperatorSpec) util.Errors {
	var validationErrors util.Errors
	for i := 0; i < e.Len(); i++ {
		validationErrors = append(validationErrors, ValidateSubTypes(e.Index(i), failOnMissingValidation, values, iopls)...)
	}

	return validationErrors
}

func processMap(e reflect.Value, failOnMissingValidation bool, values *valuesv1alpha1.Values, iopls *v1alpha1.IstioOperatorSpec) util.Errors {
	var validationErrors util.Errors
	for _, k := range e.MapKeys() {
		v := e.MapIndex(k)
		validationErrors = append(validationErrors, ValidateSubTypes(v, failOnMissingValidation, values, iopls)...)
	}

	return validationErrors
}
