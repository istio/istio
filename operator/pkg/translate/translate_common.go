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

package translate

import (
	"fmt"

	"github.com/gogo/protobuf/types"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
)

// IsComponentEnabledInSpec reports whether the given component is enabled in the given spec.
// IsComponentEnabledInSpec assumes that controlPlaneSpec has been validated.
// TODO: remove extra validations when comfort level is high enough.
func IsComponentEnabledInSpec(componentName name.ComponentName, controlPlaneSpec *v1alpha1.IstioOperatorSpec) (bool, error) {
	componentNodeI, found, err := tpath.GetFromStructPath(controlPlaneSpec, "Components."+string(componentName)+".Enabled")
	if err != nil {
		return false, fmt.Errorf("error in IsComponentEnabledInSpec GetFromStructPath componentEnabled for component=%s: %s",
			componentName, err)
	}
	if !found || componentNodeI == nil {
		return false, nil
	}
	componentNode, ok := componentNodeI.(*types.BoolValue)
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
	t := NewTranslator()
	cnMap, ok := t.ComponentMaps[cn]
	if !ok {
		return false, false, nil
	}
	valuePath := cnMap.ToHelmValuesTreeRoot
	enabledPath := valuePath + ".enabled"
	enableNodeI, found, err := tpath.Find(valueSpec, util.ToYAMLPath(enabledPath))
	if err != nil {
		return false, false, fmt.Errorf("error finding component enablement path: %s in helm value.yaml tree", enabledPath)
	}
	if !found {
		// Some components do not specify enablement should be treated as enabled if the root node in the component subtree exists.
		_, found, err := tpath.Find(valueSpec, util.ToYAMLPath(valuePath))
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

// OverlayValuesEnablement overlays any enablement in values path from the user file overlay or set flag overlay.
// The overlay is translated from values to the corresponding addonComponents enablement paths.
func OverlayValuesEnablement(baseYAML, fileOverlayYAML, setOverlayYAML string) (string, error) {
	overlayYAML, err := util.OverlayYAML(fileOverlayYAML, setOverlayYAML)
	if err != nil {
		return "", fmt.Errorf("could not overlay user config over base: %s", err)
	}

	return YAMLTree(overlayYAML, baseYAML, name.ValuesEnablementPathMap)
}

// GetEnabledComponents get all the enabled components from the given istio operator spec
func GetEnabledComponents(iopSpec *v1alpha1.IstioOperatorSpec) ([]string, error) {
	var enabledComponents []string
	if iopSpec.Components != nil {
		for _, c := range name.AllCoreComponentNames {
			enabled, err := IsComponentEnabledInSpec(c, iopSpec)
			if err != nil {
				return nil, fmt.Errorf("failed to check if component: %s is enabled or not: %v", string(c), err)
			}
			if enabled {
				enabledComponents = append(enabledComponents, string(c))
			}
		}
		for _, c := range iopSpec.Components.IngressGateways {
			if c.Enabled.GetValue() {
				enabledComponents = append(enabledComponents, string(name.IngressComponentName))
				break
			}
		}
		for _, c := range iopSpec.Components.EgressGateways {
			if c.Enabled.GetValue() {
				enabledComponents = append(enabledComponents, string(name.EgressComponentName))
				break
			}
		}
	}

	return enabledComponents, nil
}
