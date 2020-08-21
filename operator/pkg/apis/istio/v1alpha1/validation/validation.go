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

	"k8s.io/apimachinery/pkg/util/intstr"

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
	validationErrors = util.AppendErrs(validationErrors, validateFeatures(values, iopls))
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
		{"Values.global.proxy.accessLogEncoding", "meshConfig.accessLogEncoding", valuesv1alpha1.AccessLogEncoding_JSON},
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
	mixerDeprecations := []deprecatedSettings{
		{"Values.telemetry.v1.enabled", "", false},
		{"Values.global.disablePolicyChecks", "", true},
		{"MeshConfig.disablePolicyChecks", "", true},
		{"Values.pilot.policy.enabled", "", false},
		{"Components.Telemetry.Enabled", "", false},
		{"Components.Policy.Enabled", "", false},
	}
	useMixerSettings := false
	mds := []string{}
	for _, d := range mixerDeprecations {
		v, f, _ := tpath.GetFromStructPath(iop, d.old)
		if f {
			switch t := v.(type) {
			case *v1alpha1.BoolValueForPB:
				v = t.Value
			}
			if v != d.def {
				useMixerSettings = true
				mds = append(mds, d.old)
			}
		}
	}
	const mixerDeprecatedMessage = "! %s is deprecated. Mixer is deprecated and will be removed" +
		" from Istio with the 1.8 release. Please consult our docs on the replacement."
	if useMixerSettings {
		messages = append(messages, fmt.Sprintf(mixerDeprecatedMessage, strings.Join(mds, ", ")))
	}

	return strings.Join(messages, "\n")
}

// validateFeatures check whether the config sematically make sense. For example, feature X and feature Y can't be enabled together.
func validateFeatures(values *valuesv1alpha1.Values, spec *v1alpha1.IstioOperatorSpec) util.Errors {
	return CheckServicePorts(values, spec)
}

// CheckServicePorts validates Service ports. Specifically, this currently
// asserts that all ports will bind to a port number greater than 1024 when not
// running as root.
func CheckServicePorts(values *valuesv1alpha1.Values, spec *v1alpha1.IstioOperatorSpec) util.Errors {
	var errs util.Errors
	if !values.GetGateways().GetIstioIngressgateway().GetRunAsRoot().GetValue() {
		errs = util.AppendErrs(errs, validateGateways(spec.GetComponents().GetIngressGateways(), "istio-ingressgateway"))
	}
	if !values.GetGateways().GetIstioEgressgateway().GetRunAsRoot().GetValue() {
		errs = util.AppendErrs(errs, validateGateways(spec.GetComponents().GetEgressGateways(), "istio-egressgateway"))
	}
	for _, port := range values.GetGateways().GetIstioIngressgateway().GetIngressPorts() {
		var tp int
		if port["targetPort"] != nil {
			t, ok := port["targetPort"].(float64)
			if !ok {
				continue
			}
			tp = int(t)
		}

		rport, ok := port["port"].(float64)
		if !ok {
			continue
		}
		portnum := int(rport)
		if tp == 0 && portnum > 1024 {
			// Target port defaults to port. If its >1024, it is safe.
			continue
		}
		if tp < 1024 {
			// nolint: lll
			errs = util.AppendErr(errs, fmt.Errorf("port %v is invalid: targetPort is set to %v, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.istio-ingressgateway.runAsRoot=true", portnum, tp))
		}
	}
	return errs
}

func validateGateways(gw []*v1alpha1.GatewaySpec, name string) util.Errors {
	// nolint: lll
	format := "port %v/%v in gateway %v invalid: targetPort is set to %d, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.%s.runAsRoot=true"
	var errs util.Errors
	for _, gw := range gw {
		for _, p := range gw.GetK8S().GetService().GetPorts() {
			tp := 0
			if p.TargetPort != nil && p.TargetPort.Type == intstr.String {
				// Do not validate named ports
				continue
			}
			if p.TargetPort != nil && p.TargetPort.Type == intstr.Int {
				tp = int(p.TargetPort.IntVal)
			}
			if tp == 0 && p.Port > 1024 {
				// Target port defaults to port. If its >1024, it is safe.
				continue
			}
			if tp < 1024 {
				errs = util.AppendErr(errs, fmt.Errorf(format, p.Name, p.Port, gw.Name, tp, name))
			}
		}
	}
	return errs
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
