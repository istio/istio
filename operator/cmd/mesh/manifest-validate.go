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
	"strings"
)

const (
	valuesGlobal = "values.global"
)

var (
	// Keep bool values as string to avoid type conversion of flags
	boolValues = []string{"true", "false"}

	sds = map[string][]string{
		"enabled": boolValues,
	}

	imagePullPolicy = []string{"Always", "IfNotPresent", "Never"}

	k8sIngress = map[string]interface{}{
		"enabled":     boolValues,
		"enableHttps": boolValues,
		"gatewayName": []string{"ingressgateway"},
	}

	mtls = map[string]interface{}{
		"auto":    boolValues,
		"enabled": boolValues,
	}

	controlPlaneSecurityEnabled = boolValues

	telemetry = map[string]interface{}{
		"enabled": boolValues,
	}

	security = map[string]interface{}{
		"components": map[string]interface{}{
			"nodeAgent": map[string]interface{}{
				"enabled": boolValues,
			},
		},
	}

	profile = []string{"minimal", "remote", "sds", "default", "demo"}

	setFlagValues = map[string]interface{}{
		"sds":                         sds,
		"imagePullPolicy":             imagePullPolicy,
		"k8sIngress":                  k8sIngress,
		"mtls":                        mtls,
		"controlPlaneSecurityEnabled": controlPlaneSecurityEnabled,
		"telemetry":                   telemetry,
		"security":                    security,
		"profile":                     profile,
	}
)

// ValidateSetFlags performs validation for the values provided in --set flags
func ValidateSetFlags(setOverlay []string) error {
	if len(setOverlay) == 0 {
		return nil
	}

	for _, flags := range setOverlay {
		flag := strings.Split(flags, "=")
		flagName, flagValue := flag[0], flag[1]

		if strings.HasPrefix(flagName, valuesGlobal) {
			flagName = strings.Trim(flagName, valuesGlobal)
		}

		if val, ok := setFlagValues[flagName]; ok {
			if !containString(val.([]string), flagValue) {
				return fmt.Errorf("Unsuported value: %q, supported values for: %q is %q",
					flagValue, flagName, strings.Join(setFlagValues[flagName].([]string), ", "))
			}
		}
	}

	return nil
}

func containString(s []string, searchterm string) bool {
	for _, a := range s {
		if a == searchterm {
			return true
		}
	}
	return false
}
