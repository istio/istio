// Copyright 2020 Istio Authors
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

package mesh

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gogo/protobuf/types"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/validate"
	"istio.io/istio/pkg/config/validation"
)

const (
	traceSamplingMin float64 = 0.0
	traceSamplingMax float64 = 100.0
)

var (
	// Boolean values for --set flags
	boolValues = []bool{true, false}

	// Ref: https://kubernetes.io/docs/concepts/configuration/overview/#container-images
	imagePullPolicy = []string{"Always", "IfNotPresent", "Never"}

	// Keep this list updated as per following
	// https://preliminary.istio.io/docs/setup/additional-setup/config-profiles/
	profile = []string{"default", "demo", "empty", "minimal", "preview", "remote", "separate"}

	// https://preliminary.istio.io/docs/reference/config/istio.operator.v1alpha1/#IstioOperatorSpec
	setFlagValues = map[string]interface{}{
		"profile": profile,

		"installPackagePath": validate.InstallPackagePath,
		"hub":                validate.Hub,
		"tag":                validate.Tag,

		"namespace": validate.CheckNamespaceName,
		"revision":  validate.CheckRevision,

		// MeshConfig related flags
		// https://preliminary.istio.io/docs/reference/config/istio.mesh.v1alpha1.html#MeshConfig
		"values.global.disablePolicyChecks":                 boolValues,
		"values.global.policyCheckFailOpen":                 boolValues,
		"values.global.disableReportBatch":                  boolValues,
		"values.global.enableClientSidePolicyCheck":         boolValues,
		"values.global.enableTracing":                       boolValues,
		"values.global.mtls.auto":                           boolValues,
		"values.global.mtls.enabled":                        boolValues,
		"values.mixer.telemetry.sessionAffinityEnabled":     boolValues,
		"values.global.proxy.envoyAccessLogService.enabled": boolValues,
		"values.global.proxy.protocolDetectionTimeout":      validateDuration,
		"values.global.proxy.dnsRefreshRate":                validateDuration,
		"values.global.connectTimeout":                      validateDuration,
		"values.mixer.telemetry.reportBatchMaxTime":         validateDuration,
		"values.mixer.telemetry.reportBatchMaxEntries":      validation.ValidateReportBatchMaxEntries,
		"values.global.outboundTrafficPolicy.mode":          []string{"REGISTRY_ONLY", "ALLOW_ANY"},

		"security.components.nodeAgent.enabled": boolValues,

		// Possible values for Istio components
		// https://preliminary.istio.io/docs/reference/config/istio.operator.v1alpha1/#IstioComponentSetSpec
		"components.base.enabled":            boolValues,
		"components.pilot.enabled":           boolValues,
		"components.proxy.enabled":           boolValues,
		"components.sidecarInjector.enabled": boolValues,
		"components.policy.enabled":          boolValues,
		"components.telemetry.enabled":       boolValues,
		"components.citadel.enabled":         boolValues,
		"components.nodeAgent.enabled":       boolValues,
		"components.galley.enabled":          boolValues,
		"components.cni.enabled":             boolValues,
		"components.ingressGateways.enabled": boolValues,
		"components.egressGateways.enabled":  boolValues,

		"values.global.controlPlaneSecurityEnabled": boolValues,
		"values.global.k8sIngress.enabled":          boolValues,
		"values.global.k8sIngress.enableHttps":      boolValues,
		"values.global.k8sIngress.gatewayName":      []string{"ingressgateway"},
		"values.global.sds.enabled":                 boolValues,
		"values.global.imagePullPolicy":             imagePullPolicy,

		"values.telemetry.enabled":                  boolValues,
		"values.pilot.traceSampling":                isValidTraceSampling,
		"values.pilot.policy.enabled":               boolValues,
		"values.prometheus.enabled":                 boolValues,
		"values.mixer.adapters.stackdriver.enabled": boolValues,

		"values.gateways.istio-ingressgateway.enabled":     boolValues,
		"values.gateways.istio-ingressgateway.sds.enabled": boolValues,
		"values.gateways.enabled":                          boolValues,
	}
)

// ValidateSetFlags performs validation for the values provided in --set flags
func ValidateSetFlags(setOverlay []string) (errs util.Errors) {
	if len(setOverlay) == 0 {
		return
	}

	for _, flags := range setOverlay {

		if !isValidFlagFormat(flags) {
			errs = append(errs, fmt.Errorf("\n Invalid flag format %q", flags))
			return
		}

		flagName, flagValue := splitSetFlags(flags)

		if isFlagNameAvailable(flagName) {
			if err := verifyValues(flagName, flagValue); err != nil {
				errs = append(errs, err)
			}
		} else {
			// Skip this step until all the possible combination
			// for flags and its validations are ready
			// errs = append(errs, fmt.Errorf("\n Invalid flagName: %q", flagName))
		}
	}
	return
}

// verifyValues compares provided values with actual values and throw error if it is invalid
func verifyValues(flagName, flagValue string) error {
	val := getFlagValue(flagName)
	valType := reflect.TypeOf(val)

	switch val.(type) {
	case []string:
		if !containString(val.([]string), flagValue) {
			return fmt.Errorf("\n Unsupported value: %q, supported values for: %q is %q",
				flagValue, flagName, strings.Join(val.([]string), ", "))
		}
	case []bool:
		_, err := strconv.ParseBool(flagValue)
		if err != nil || flagValue == "0" || flagValue == "1" {
			return fmt.Errorf("\n Unsupported value: %q, supported values for: %q is %t",
				flagValue, flagName, boolValues)
		}
	}

	if valType == reflect.TypeOf(isValidTraceSampling) {
		fval, _ := strconv.ParseFloat(flagValue, 64)
		if !isValidTraceSampling(fval) {
			return fmt.Errorf("\n Unsupported value: %q, supported values for: %q is between %.1f to %.1f",
				flagValue, flagName, traceSamplingMin, traceSamplingMax)
		}
	}
	if flagName == "installPackagePath" {
		if err := validate.InstallPackagePath([]string{flagName}, flagValue); len(err) != 0 {
			return err
		}
	}
	if flagName == "hub" {
		if err := validate.Hub([]string{flagName}, flagValue); len(err) != 0 {
			return err
		}
	}
	if flagName == "tag" {
		if err := validate.Tag([]string{flagName}, flagValue); len(err) != 0 {
			return err
		}
	}
	if valType == reflect.TypeOf(validate.CheckNamespaceName) && flagName == "namespace" {
		if !validate.CheckNamespaceName(flagValue, false) {
			return fmt.Errorf("\n Unsupported format: %q for flag %q", flagValue, flagName)
		}
	}
	if valType == reflect.TypeOf(validate.CheckRevision) && flagName == "revision" {
		if !validate.CheckRevision(flagValue, false) {
			return fmt.Errorf("\n Unsupported format: %q for flag %q", flagValue, flagName)
		}
	}
	if valType == reflect.TypeOf(validateDuration) {
		if err := validateDuration(flagName, flagValue); err != nil {
			return fmt.Errorf("\n Unsupported value: %q for %q. \n Error: %v",
				flagValue, flagName, err)
		}
	}
	if valType == reflect.TypeOf(validation.ValidateReportBatchMaxEntries) {
		if err := validation.ValidateReportBatchMaxEntries(flagValue); err != nil {
			return fmt.Errorf("\n Unsupported value: %q for flag %q, use valid value eg: 100",
				flagValue, flagName)
		}
	}
	return nil
}

// isValidFlagFormat verifies if the flag have equal sign
func isValidFlagFormat(flag string) bool {
	return strings.Contains(flag, "=")
}

// isFlagNameAvailable checks if the flag provided is available in flag list
func isFlagNameAvailable(flagName string) bool {
	_, isAvailable := setFlagValues[flagName]
	return isAvailable
}

// getFlagValue gives searched flag values
func getFlagValue(flagName string) interface{} {
	if val, ok := setFlagValues[flagName]; ok {
		return val
	}
	return nil
}

// splitSetFlags separate flag name and its value
func splitSetFlags(flags string) (flagName, flagValue string) {
	flag := strings.Split(flags, "=")
	return flag[0], flag[1]
}

// containString verifies if the flag value is valid string value
func containString(s []string, searchterm string) bool {
	for _, a := range s {
		if a == searchterm {
			return true
		}
	}
	return false
}

// isValidTraceSampling validates pilot sampling rate
func isValidTraceSampling(n float64) bool {
	if n < traceSamplingMin || n > traceSamplingMax {
		return false
	}
	return true
}

// validateDuration verifies valid time duration for different flags
func validateDuration(flagName, duration string) (err error) {
	d, err := convertDuration(duration)
	if err != nil {
		return err
	}
	flag := strings.Split(flagName, ".")

	// extract the last part of the flag to match with cases
	switch flag[len(flag)-1] {
	case "protocolDetectionTimeout":
		return validation.ValidateProtocolDetectionTimeout(d)
	case "connectTimeout":
		return validation.ValidateConnectTimeout(d)
	case "dnsRefreshRate":
		return validation.ValidateDNSRefreshRate(d)
	case "reportBatchMaxTime":
		return validation.ValidateDuration(d)
	}
	return nil
}

func convertDuration(duration string) (*types.Duration, error) {
	dur, err := time.ParseDuration(duration)
	if err != nil {
		return nil, fmt.Errorf("Invalid duration format %q", duration)
	}
	return types.DurationProto(dur), nil
}
