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
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	wrappers "google.golang.org/protobuf/types/known/wrapperspb"
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
	def any
}

// ValidateConfig  calls validation func for every defined element in Values
func ValidateConfig(failOnMissingValidation bool, iopls *v1alpha1.IstioOperatorSpec) (util.Errors, string) {
	var validationErrors util.Errors
	var warningMessages []string
	iopvalString := util.ToYAMLWithJSONPB(iopls.Values)
	values := &valuesv1alpha1.Values{}
	if err := util.UnmarshalWithJSONPB(iopvalString, values, true); err != nil {
		return util.NewErrs(err), ""
	}

	validationErrors = util.AppendErrs(validationErrors, ValidateSubTypes(reflect.ValueOf(values).Elem(), failOnMissingValidation, values, iopls))

	featureErrors, featureWarningMessages := validateFeatures(values, iopls)
	validationErrors = util.AppendErrs(validationErrors, featureErrors)
	warningMessages = append(warningMessages, featureWarningMessages...)

	deprecatedErrors, deprecatedWarningMessages := checkDeprecatedSettings(iopls)
	if deprecatedErrors != nil {
		validationErrors = util.AppendErr(validationErrors, deprecatedErrors)
	}
	warningMessages = append(warningMessages, deprecatedWarningMessages...)
	return validationErrors, strings.Join(warningMessages, "\n")
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

func checkDeprecatedSettings(iop *v1alpha1.IstioOperatorSpec) (util.Errors, []string) {
	var errs util.Errors
	messages := []string{}
	warningSettings := []deprecatedSettings{
		{"Values.global.certificates", "meshConfig.certificates", nil},
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
		{"Values.global.proxy.holdApplicationUntilProxyStarts", "meshConfig.defaultConfig.holdApplicationUntilProxyStarts", false},
		{"Values.pilot.ingress", "meshConfig.ingressService, meshConfig.ingressControllerMode, and meshConfig.ingressClass", nil},
		{"Values.global.mtls.enabled", "the PeerAuthentication resource", nil},
		{"Values.global.mtls.auto", "meshConfig.enableAutoMtls", nil},
		{"Values.global.tracer.lightstep.address", "meshConfig.defaultConfig.tracing.lightstep.address", ""},
		{"Values.global.tracer.lightstep.accessToken", "meshConfig.defaultConfig.tracing.lightstep.accessToken", ""},
		{"Values.global.tracer.zipkin.address", "meshConfig.defaultConfig.tracing.zipkin.address", nil},
		{"Values.global.tracer.stackdriver.debug", "meshConfig.defaultConfig.tracing.stackdriver.debug", false},
		{"Values.global.tracer.stackdriver.maxNumberOfAttributes", "meshConfig.defaultConfig.tracing.stackdriver.maxNumberOfAttributes", 0},
		{"Values.global.tracer.stackdriver.maxNumberOfAnnotations", "meshConfig.defaultConfig.tracing.stackdriver.maxNumberOfAnnotations", 0},
		{"Values.global.tracer.stackdriver.maxNumberOfMessageEvents", "meshConfig.defaultConfig.tracing.stackdriver.maxNumberOfMessageEvents", 0},
		{"Values.global.tracer.datadog.address", "meshConfig.defaultConfig.tracing.datadog.address", ""},
		{"Values.global.meshExpansion.enabled", "Gateway and other Istio networking resources, such as in samples/multicluster/", false},
		{"Values.gateways.istio-ingressgateway.meshExpansionPorts", "components.ingressGateways[name=istio-ingressgateway].k8s.service.ports", nil},
		{"AddonComponents.istiocoredns.Enabled", "the in-proxy DNS capturing (ISTIO_META_DNS_CAPTURE)", false},
		{"Values.istiocoredns.enabled", "the in-proxy DNS capturing (ISTIO_META_DNS_CAPTURE)", false},
		// nolint: lll
		{"Values.global.jwtPolicy", "Values.global.jwtPolicy=third-party-jwt. See https://istio.io/latest/docs/ops/best-practices/security/#configure-third-party-service-account-tokens for more information", "third-party-jwt"},
		{
			"Values.telemetry.v2.stackdriver.logging", "Values.telemetry.v2.stackdriver.outboundAccessLogging and Values.telemetry.v2.stackdriver.inboundAccessLogging",
			false,
		},
		{"Values.global.centralIstiod", "Values.global.externalIstiod", false},
		{"Values.global.arch", "the affinity of k8s settings", nil},
	}

	failHardSettings := []deprecatedSettings{
		{"Values.grafana.enabled", "the samples/addons/ deployments", false},
		{"Values.tracing.enabled", "the samples/addons/ deployments", false},
		{"Values.kiali.enabled", "the samples/addons/ deployments", false},
		{"Values.prometheus.enabled", "the samples/addons/ deployments", false},
		{"AddonComponents.grafana.Enabled", "the samples/addons/ deployments", false},
		{"AddonComponents.tracing.Enabled", "the samples/addons/ deployments", false},
		{"AddonComponents.kiali.Enabled", "the samples/addons/ deployments", false},
		{"AddonComponents.prometheus.Enabled", "the samples/addons/ deployments", false},
	}

	for _, d := range warningSettings {
		v, f, _ := tpath.GetFromStructPath(iop, d.old)
		if f {
			switch t := v.(type) {
			// need to do conversion for bool value defined in IstioOperator component spec.
			case *wrappers.BoolValue:
				v = t.Value
			}
			if v != d.def {
				messages = append(messages, fmt.Sprintf("! %s is deprecated; use %s instead", firstCharsToLower(d.old), d.new))
			}
		}
	}
	for _, d := range failHardSettings {
		v, f, _ := tpath.GetFromStructPath(iop, d.old)
		if f {
			switch t := v.(type) {
			// need to do conversion for bool value defined in IstioOperator component spec.
			case *wrappers.BoolValue:
				v = t.Value
			}
			if v != d.def {
				ms := fmt.Sprintf("! %s is deprecated; use %s instead", firstCharsToLower(d.old), d.new)
				errs = util.AppendErr(errs, errors.New(ms+"\n"))
			}
		}
	}
	return errs, messages
}

type FeatureValidator func(*valuesv1alpha1.Values, *v1alpha1.IstioOperatorSpec) (util.Errors, []string)

// validateFeatures check whether the config sematically make sense. For example, feature X and feature Y can't be enabled together.
func validateFeatures(values *valuesv1alpha1.Values, spec *v1alpha1.IstioOperatorSpec) (errs util.Errors, warnings []string) {
	validators := []FeatureValidator{
		CheckServicePorts,
		CheckAutoScaleAndReplicaCount,
	}

	for _, validator := range validators {
		newErrs, newWarnings := validator(values, spec)
		errs = util.AppendErrs(errs, newErrs)
		warnings = append(warnings, newWarnings...)
	}

	return
}

// CheckAutoScaleAndReplicaCount warns when autoscaleEnabled is true and k8s replicaCount is set.
func CheckAutoScaleAndReplicaCount(values *valuesv1alpha1.Values, spec *v1alpha1.IstioOperatorSpec) (errs util.Errors, warnings []string) {
	if values.GetPilot().GetAutoscaleEnabled().GetValue() && spec.GetComponents().GetPilot().GetK8S().GetReplicaCount() > 1 {
		warnings = append(warnings,
			"components.pilot.k8s.replicaCount should not be set when values.pilot.autoscaleEnabled is true")
	}

	validateGateways := func(gateways []*v1alpha1.GatewaySpec, gwType string) {
		const format = "components.%sGateways[name=%s].k8s.replicaCount should not be set when values.gateways.istio-%sgateway.autoscaleEnabled is true"
		for _, gw := range gateways {
			if gw.GetK8S().GetReplicaCount() != 0 {
				warnings = append(warnings, fmt.Sprintf(format, gwType, gw.Name, gwType))
			}
		}
	}

	if values.GetGateways().GetIstioIngressgateway().GetAutoscaleEnabled().GetValue() {
		validateGateways(spec.GetComponents().GetIngressGateways(), "ingress")
	}

	if values.GetGateways().GetIstioEgressgateway().GetAutoscaleEnabled().GetValue() {
		validateGateways(spec.GetComponents().GetEgressGateways(), "egress")
	}

	return
}

// CheckServicePorts validates Service ports. Specifically, this currently
// asserts that all ports will bind to a port number greater than 1024 when not
// running as root.
func CheckServicePorts(values *valuesv1alpha1.Values, spec *v1alpha1.IstioOperatorSpec) (errs util.Errors, warnings []string) {
	if !values.GetGateways().GetIstioIngressgateway().GetRunAsRoot().GetValue() {
		errs = util.AppendErrs(errs, validateGateways(spec.GetComponents().GetIngressGateways(), "istio-ingressgateway"))
	}
	if !values.GetGateways().GetIstioEgressgateway().GetRunAsRoot().GetValue() {
		errs = util.AppendErrs(errs, validateGateways(spec.GetComponents().GetEgressGateways(), "istio-egressgateway"))
	}
	for _, raw := range values.GetGateways().GetIstioIngressgateway().GetIngressPorts() {
		p := raw.AsMap()
		var tp int
		if p["targetPort"] != nil {
			t, ok := p["targetPort"].(float64)
			if !ok {
				continue
			}
			tp = int(t)
		}

		rport, ok := p["port"].(float64)
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
	return
}

func validateGateways(gw []*v1alpha1.GatewaySpec, name string) util.Errors {
	// nolint: lll
	format := "port %v/%v in gateway %v invalid: targetPort is set to %d, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.%s.runAsRoot=true"
	var errs util.Errors
	for _, gw := range gw {
		for _, p := range gw.GetK8S().GetService().GetPorts() {
			tp := 0
			if p.TargetPort != nil && p.TargetPort.Type == int64(intstr.String) {
				// Do not validate named ports
				continue
			}
			if p.TargetPort != nil && p.TargetPort.Type == int64(intstr.Int) {
				tp = int(p.TargetPort.IntVal.GetValue())
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
