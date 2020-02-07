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

package translate

import (
	"fmt"
	"strings"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	binversion "istio.io/istio/operator/version"
)

var (
	// LegacyAddonComponentPathMap defines reverse translation mapping for legacy addon component names.
	LegacyAddonComponentPathMap = make(map[string]string)
)

func init() {
	for n := range name.AddonComponentNamesMap {
		cn := strings.ToLower(string(n))
		valuePath := fmt.Sprintf("spec.values.%s.enabled", cn)
		iopPath := fmt.Sprintf("spec.addonComponents.%s.enabled", cn)
		LegacyAddonComponentPathMap[valuePath] = iopPath
	}
}

// IsComponentEnabledInSpec reports whether the given component is enabled in the given spec.
// IsComponentEnabledInSpec assumes that controlPlaneSpec has been validated.
// TODO: remove extra validations when comfort level is high enough.
func IsComponentEnabledInSpec(componentName name.ComponentName, controlPlaneSpec *v1alpha1.IstioOperatorSpec) (bool, error) {
	// for Istio components, check whether override path exist in values part first then ISCP.
	enabled, pathExist, err := IsComponentEnabledFromValue(componentName, controlPlaneSpec.Values)
	// only return value when path exists
	if err == nil && pathExist {
		return enabled, nil
	}
	if componentName == name.IngressComponentName {
		return len(controlPlaneSpec.Components.IngressGateways) != 0, nil
	}
	if componentName == name.EgressComponentName {
		return len(controlPlaneSpec.Components.EgressGateways) != 0, nil
	}
	if componentName == name.AddonComponentName {
		for _, ac := range controlPlaneSpec.AddonComponents {
			if ac.Enabled != nil && ac.Enabled.Value {
				return true, nil
			}
		}
		return false, nil
	}

	componentNodeI, found, err := tpath.GetFromStructPath(controlPlaneSpec, "Components."+string(componentName)+".Enabled")
	if err != nil {
		return false, fmt.Errorf("error in IsComponentEnabledInSpec GetFromStructPath componentEnabled for component=%s: %s",
			componentName, err)
	}
	if !found || componentNodeI == nil {
		return false, nil
	}
	componentNode, ok := componentNodeI.(*v1alpha1.BoolValueForPB)
	if !ok {
		return false, fmt.Errorf("component %s enabled has bad type %T, expect *v1alpha1.BoolValueForPB", componentName, componentNodeI)
	}
	if componentNode == nil {
		return false, nil
	}
	return componentNode.Value, nil
}

// IsComponentEnabledFromValue get whether component is enabled in helm value.yaml tree.
// valuePath points to component path in the values tree.
func IsComponentEnabledFromValue(cn name.ComponentName, valueSpec map[string]interface{}) (enabled bool, pathExist bool, err error) {
	t, err := NewTranslator(binversion.OperatorBinaryVersion.MinorVersion)
	if err != nil {
		return false, false, err
	}
	cnMap, ok := t.ComponentMaps[cn]
	if !ok {
		return false, false, nil
	}
	valuePath := cnMap.ToHelmValuesTreeRoot
	enabledPath := valuePath + ".enabled"
	enableNodeI, found, err := tpath.GetFromTreePath(valueSpec, util.ToYAMLPath(enabledPath))
	if err != nil {
		return false, false, fmt.Errorf("error finding component enablement path: %s in helm value.yaml tree", enabledPath)
	}
	if !found {
		// Some components do not specify enablement should be treated as enabled if the root node in the component subtree exists.
		_, found, err := tpath.GetFromTreePath(valueSpec, util.ToYAMLPath(valuePath))
		if err != nil {
			return false, false, err
		}
		if found {
			return true, false, nil
		}
		return false, false, nil
	}
	enableNode, ok := enableNodeI.(bool)
	if !ok {
		return false, true, fmt.Errorf("node at valuePath %s has bad type %T, expect bool", enabledPath, enableNodeI)
	}
	return enableNode, true, nil
}
