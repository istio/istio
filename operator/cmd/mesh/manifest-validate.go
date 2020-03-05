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
	"strconv"
	"strings"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/validate"
)

const (
	valuesGlobal = "values.global."
)

var (
	// Keep bool values as string to avoid type conversion of flags
	boolValues = []bool{true, false}

	// Ref: https://kubernetes.io/docs/concepts/configuration/overview/#container-images
	imagePullPolicy = []string{"Always", "IfNotPresent", "Never"}

	// Keep this list updated as per following
	// https://preliminary.istio.io/docs/setup/additional-setup/config-profiles/
	profile = []string{"default", "demo", "empty", "minimal", "preview", "remote", "separate"}

	// https://preliminary.istio.io/docs/reference/config/istio.operator.v1alpha1/#IstioOperatorSpec
	setFlagValues = map[string]interface{}{
		"profile": profile,

		"installPackagePath": validate.CheckIstioOperatorSpec,
		"hub":                validate.CheckIstioOperatorSpec,
		"tag":                validate.CheckIstioOperatorSpec,

		"sds.enabled":     boolValues,
		"imagePullPolicy": imagePullPolicy,

		"k8sIngress.enabled":     boolValues,
		"k8sIngress.enableHttps": boolValues,
		"k8sIngress.gatewayName": []string{"ingressgateway"},

		"mtls.auto":    boolValues,
		"mtls.enabled": boolValues,

		"controlPlaneSecurityEnabled": boolValues,

		"telemetry.enabled":                     boolValues,
		"security.components.nodeAgent.enabled": boolValues,
	}
)

// ValidateSetFlags performs validation for the values provided in --set flags
func ValidateSetFlags(setOverlay []string) (errs util.Errors) {
	if len(setOverlay) == 0 {
		return nil
	}

	for _, flags := range setOverlay {

		if !isValidFlagFormat(flags) {
			errs = append(errs, fmt.Errorf("\n Invalid flag format %q", flags))
			return
		}

		flagName, flagValue := splitSetFlags(flags)

		if isFlagNameAvailable(flagName) {
			errs = verifyValues(flagName, flagValue)
		} else {
			errs = append(errs, fmt.Errorf("\n Invalid flag: %q", valuesGlobal+flagName))
		}
	}
	return
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
func splitSetFlags(flags string) (string, string) {
	flag := strings.Split(flags, "=")
	flagName, flagValue := flag[0], flag[1]

	if strings.HasPrefix(flagName, valuesGlobal) {
		flagName = strings.TrimPrefix(flagName, valuesGlobal)
	}
	return flagName, flagValue
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

func verifyValues(flagName, flagValue string) (errs util.Errors) {
	val := getFlagValue(flagName)
	switch val.(type) {
	case []string:
		if !containString(val.([]string), flagValue) {
			errs = append(errs, fmt.Errorf("\n Unsupported value: %q, supported values for: %q is %q",
				flagValue, flagName, strings.Join(val.([]string), ", ")))
		}
	case []bool:
		_, err := strconv.ParseBool(flagValue)
		if err != nil {
			errs = append(errs, fmt.Errorf("\n Unsupported value: %q, supported values for: %q is %t",
				flagValue, flagName, boolValues))
		}
	case validate.ValidatorFunc:
		iops := &v1alpha1.IstioOperatorSpec{Tag: flagValue}
		if err := validate.CheckIstioOperatorSpec(iops, true); len(err) != 0 {
			errs = append(errs, fmt.Errorf(err.Error()))
		}
	}
	return
}
