// Copyright 2019 Istio Authors
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

package name

import (
	"fmt"
	"reflect"

	protobuf "github.com/gogo/protobuf/types"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/util"
	"istio.io/pkg/log"
)

var (
	scope = log.RegisterScope("name", "name", 0)
)

// FeatureName is a feature name string, typed to constrain allowed values.
type FeatureName string

const (
	// OperatorAPINamespace is the API namespace for operator config.
	// TODO: move this to a base definitions file when one is created.
	OperatorAPINamespace = "operator.istio.io"
)

const (
	// IstioFeature names, must be the same as feature names defined in the IstioControlPlane proto, since these are
	// used to reference structure paths.
	IstioBaseFeatureName         FeatureName = "Base"
	TrafficManagementFeatureName FeatureName = "TrafficManagement"
	PolicyFeatureName            FeatureName = "Policy"
	TelemetryFeatureName         FeatureName = "Telemetry"
	SecurityFeatureName          FeatureName = "Security"
	ConfigManagementFeatureName  FeatureName = "ConfigManagement"
	AutoInjectionFeatureName     FeatureName = "AutoInjection"
	GatewayFeatureName           FeatureName = "Gateways"
	ThirdPartyFeatureName        FeatureName = "ThirdParty"
)

// ComponentName is a component name string, typed to constrain allowed values.
type ComponentName string

const (
	// IstioComponent names corresponding to the IstioControlPlane proto component names. Must be the same, since these
	// are used for struct traversal.
	IstioBaseComponentName       ComponentName = "crds"
	PilotComponentName           ComponentName = "Pilot"
	GalleyComponentName          ComponentName = "Galley"
	SidecarInjectorComponentName ComponentName = "Injector"
	PolicyComponentName          ComponentName = "Policy"
	TelemetryComponentName       ComponentName = "Telemetry"
	CitadelComponentName         ComponentName = "Citadel"
	CertManagerComponentName     ComponentName = "CertManager"
	NodeAgentComponentName       ComponentName = "NodeAgent"
	IngressComponentName         ComponentName = "IngressGateway"
	EgressComponentName          ComponentName = "EgressGateway"

	// The following are third party components, not a part of the IstioControlPlaneAPI but still installed in some
	// profiles through the Helm API.
	PrometheusComponentName         ComponentName = "Prometheus"
	PrometheusOperatorComponentName ComponentName = "PrometheusOperator"
	GrafanaComponentName            ComponentName = "Grafana"
	KialiComponentName              ComponentName = "Kiali"
	CNIComponentName                ComponentName = "CNI"
	TracingComponentName            ComponentName = "Tracing"
)

var (
	ComponentNameToFeatureName = map[ComponentName]FeatureName{
		IstioBaseComponentName:       IstioBaseFeatureName,
		PilotComponentName:           TrafficManagementFeatureName,
		GalleyComponentName:          ConfigManagementFeatureName,
		SidecarInjectorComponentName: AutoInjectionFeatureName,
		PolicyComponentName:          PolicyFeatureName,
		TelemetryComponentName:       TelemetryFeatureName,
		CitadelComponentName:         SecurityFeatureName,
		CertManagerComponentName:     SecurityFeatureName,
		NodeAgentComponentName:       SecurityFeatureName,
		IngressComponentName:         GatewayFeatureName,
		EgressComponentName:          GatewayFeatureName,
		// External
		PrometheusComponentName:         TelemetryFeatureName,
		PrometheusOperatorComponentName: TelemetryFeatureName,
		GrafanaComponentName:            TelemetryFeatureName,
		KialiComponentName:              TelemetryFeatureName,
		TracingComponentName:            TelemetryFeatureName,
		// ThirdParty
		CNIComponentName: ThirdPartyFeatureName,
	}
)

// ManifestMap is a map of ComponentName to its manifest string.
type ManifestMap map[ComponentName]string

// IsFeatureEnabledInSpec reports whether the given feature is enabled in the given spec.
// This follows the logic description in IstioControlPlane proto.
// IsFeatureEnabledInSpec assumes that controlPlaneSpec has been validated.
func IsFeatureEnabledInSpec(featureName FeatureName, controlPlaneSpec *v1alpha2.IstioControlPlaneSpec) (bool, error) {
	featureNodeI, found, err := GetFromStructPath(controlPlaneSpec, string(featureName)+".Enabled")
	if err != nil {
		return false, fmt.Errorf("error in IsFeatureEnabledInSpec GetFromStructPath featureEnabled for feature=%s: %s", featureName, err)
	}
	if !found || featureNodeI == nil {
		return false, nil
	}
	featureNode, ok := featureNodeI.(*protobuf.BoolValue)
	if !ok {
		return false, fmt.Errorf("feature %s enabled has bad type %T, expect *protobuf.BoolValue", featureName, featureNodeI)
	}
	if featureNode == nil || !featureNode.Value {
		return false, nil
	}
	return featureNode.Value, nil
}

// IsComponentEnabledInSpec reports whether the given feature and component are enabled in the given spec. The logic is, in
// order of evaluation:
// 1. if the feature is not defined, the component is disabled, else
// 2. if the feature is disabled, the component is disabled, else
// 3. if the component is not defined, it is reported disabled, else
// 4. if the component disabled, it is reported disabled, else
// 5. the component is enabled.
// This follows the logic description in IstioControlPlane proto.
// IsComponentEnabledInSpec assumes that controlPlaneSpec has been validated.
// TODO: remove extra validations when comfort level is high enough.
func IsComponentEnabledInSpec(featureName FeatureName, componentName ComponentName, controlPlaneSpec *v1alpha2.IstioControlPlaneSpec) (bool, error) {
	featureNodeI, found, err := GetFromStructPath(controlPlaneSpec, string(featureName)+".Enabled")
	if err != nil {
		return false, fmt.Errorf("error in IsComponentEnabledInSpec GetFromStructPath featureEnabled for feature=%s, component=%s: %s",
			featureName, componentName, err)
	}
	if !found || featureNodeI == nil {
		return false, nil
	}
	featureNode, ok := featureNodeI.(*protobuf.BoolValue)
	if !ok {
		return false, fmt.Errorf("feature %s enabled has bad type %T, expect *protobuf.BoolValue", featureName, featureNodeI)
	}
	if featureNode == nil || !featureNode.Value {
		return false, nil
	}

	componentNodeI, found, err := GetFromStructPath(controlPlaneSpec, string(featureName)+".Components."+string(componentName)+".Enabled")
	if err != nil {
		return false, fmt.Errorf("error in IsComponentEnabledInSpec GetFromStructPath componentEnabled for feature=%s, component=%s: %s",
			featureName, componentName, err)
	}
	if !found || componentNodeI == nil {
		return featureNode.Value, nil
	}
	componentNode, ok := componentNodeI.(*protobuf.BoolValue)
	if !ok {
		return false, fmt.Errorf("component %s enabled has bad type %T, expect *protobuf.BoolValue", componentName, componentNodeI)
	}
	if componentNode == nil {
		return featureNode.Value, nil
	}
	return componentNode.Value, nil
}

// IsComponentEnabledFromValue get whether component is enabled in helm value.yaml tree.
// valuePath points to component path in the values tree.
func IsComponentEnabledFromValue(valuePath string, valueSpec map[string]interface{}) (bool, error) {
	enabledPath := valuePath + ".enabled"
	enableNodeI, found, err := GetFromTreePath(valueSpec, util.ToYAMLPath(enabledPath))
	if err != nil {
		return false, fmt.Errorf("error finding component enablement path: %s in helm value.yaml tree", enabledPath)
	}
	if !found {
		// Some components do not specify enablement should be treated as enabled if the root node in the component subtree exists.
		_, found, err := GetFromTreePath(valueSpec, util.ToYAMLPath(valuePath))
		if found && err == nil {
			return true, nil
		}
		return false, nil
	}
	enableNode, ok := enableNodeI.(bool)
	if !ok {
		return false, fmt.Errorf("node at valuePath %s has bad type %T, expect bool", enabledPath, enableNodeI)
	}
	return enableNode, nil
}

// NamespaceFromValue gets the namespace value in helm value.yaml tree.
func NamespaceFromValue(valuePath string, valueSpec map[string]interface{}) (string, error) {
	nsNodeI, found, err := GetFromTreePath(valueSpec, util.ToYAMLPath(valuePath))
	if err != nil {
		return "", fmt.Errorf("namespace path not found: %s from helm value.yaml tree", valuePath)
	}
	if !found || nsNodeI == nil {
		return "", nil
	}
	nsNode, ok := nsNodeI.(string)
	if !ok {
		return "", fmt.Errorf("node at helm value.yaml tree path %s has bad type %T, expect string", valuePath, nsNodeI)
	}
	return nsNode, nil
}

// GetFromTreePath returns the value at path from the given tree, or false if the path does not exist.
func GetFromTreePath(inputTree map[string]interface{}, path util.Path) (interface{}, bool, error) {
	scope.Debugf("GetFromTreePath path=%s", path)
	if len(path) == 0 {
		return nil, false, fmt.Errorf("path is empty")
	}
	val := inputTree[path[0]]
	if val == nil {
		return nil, false, nil
	}
	if len(path) == 1 {
		return val, true, nil
	}
	switch newRoot := val.(type) {
	case map[string]interface{}:
		return GetFromTreePath(newRoot, path[1:])
	case []interface{}:
		for _, node := range newRoot {
			nextVal, found, err := GetFromTreePath(node.(map[string]interface{}), path[1:])
			if err != nil {
				continue
			}
			if found {
				return nextVal, true, nil
			}
		}
		return nil, false, nil
	}
	return GetFromTreePath(val.(map[string]interface{}), path[1:])
}

// Namespace returns the namespace for the component. It follows these rules:
// 1. If DefaultNamespace is unset, log and error and return the empty string.
// 2. If the feature and component namespaces are unset, return DefaultNamespace.
// 3. If the feature namespace is set but component name is unset, return the feature namespace.
// 4. Otherwise return the component namespace.
// Namespace assumes that controlPlaneSpec has been validated.
// TODO: remove extra validations when comfort level is high enough.
func Namespace(featureName FeatureName, componentName ComponentName, controlPlaneSpec *v1alpha2.IstioControlPlaneSpec) (string, error) {
	defaultNamespaceI, found, err := GetFromStructPath(controlPlaneSpec, "DefaultNamespace")
	if !found {
		return "", fmt.Errorf("can't find any setting for defaultNamespace for feature=%s, component=%s", featureName, componentName)
	}
	if err != nil {
		return "", fmt.Errorf("error in Namepsace for feature=%s, component=%s: %s", featureName, componentName, err)

	}
	defaultNamespace, ok := defaultNamespaceI.(string)
	if !ok {
		return "", fmt.Errorf("defaultNamespace has bad type %T, expect string", defaultNamespaceI)
	}
	if defaultNamespace == "" {
		return "", fmt.Errorf("defaultNamespace must be set")
	}

	featureNamespace := defaultNamespace
	featureNodeI, found, err := GetFromStructPath(controlPlaneSpec, string(featureName)+".Components.Namespace")
	if err != nil {
		return "", fmt.Errorf("error in Namepsace GetFromStructPath featureNamespace for feature=%s, component=%s: %s", featureName, componentName, err)
	}
	if found && featureNodeI != nil {
		featureNamespace, ok = featureNodeI.(string)
		if !ok {
			return "", fmt.Errorf("feature %s namespace has bad type %T, expect string", featureName, featureNodeI)
		}
		if featureNamespace == "" {
			featureNamespace = defaultNamespace
		}
	}

	componentNodeI, found, err := GetFromStructPath(controlPlaneSpec, string(featureName)+".Components."+string(componentName)+".Namespace")
	if err != nil {
		return "", fmt.Errorf("error in Namepsace GetFromStructPath componentNamespace for feature=%s, component=%s: %s", featureName, componentName, err)
	}
	if !found {
		return featureNamespace, nil
	}
	if componentNodeI == nil {
		return featureNamespace, nil
	}
	componentNamespace, ok := componentNodeI.(string)
	if !ok {
		return "", fmt.Errorf("component %s enabled has bad type %T, expect string", componentName, componentNodeI)
	}
	if componentNamespace == "" {
		return featureNamespace, nil
	}
	return componentNamespace, nil
}

// GetFromStructPath returns the value at path from the given node, or false if the path does not exist.
// Node and all intermediate along path must be type struct ptr.
func GetFromStructPath(node interface{}, path string) (interface{}, bool, error) {
	return getFromStructPath(node, util.PathFromString(path))
}

// getFromStructPath is the internal implementation of GetFromStructPath which recurses through a tree of Go structs
// given a path. It terminates when the end of the path is reached or a path element does not exist.
func getFromStructPath(node interface{}, path util.Path) (interface{}, bool, error) {
	scope.Debugf("getFromStructPath path=%s, node(%T)", path, node)
	if len(path) == 0 {
		scope.Debugf("getFromStructPath returning node(%T)%v", node, node)
		return node, !util.IsValueNil(node), nil
	}
	kind := reflect.TypeOf(node).Kind()
	var structElems reflect.Value
	switch kind {
	case reflect.Map, reflect.Slice:
		if len(path) == 0 {
			return nil, false, fmt.Errorf("getFromStructPath path %s, unsupported leaf type %T", path, node)
		}
	case reflect.Ptr:
		structElems = reflect.ValueOf(node).Elem()
		if !util.IsStruct(structElems) {
			return nil, false, fmt.Errorf("getFromStructPath path %s, expected struct ptr, got %T", path, node)
		}
	default:
		return nil, false, fmt.Errorf("getFromStructPath path %s, unsupported type %T", path, node)
	}

	if util.IsNilOrInvalidValue(structElems) {
		return nil, false, nil
	}

	for i := 0; i < structElems.NumField(); i++ {
		fieldName := structElems.Type().Field(i).Name

		if fieldName != path[0] {
			continue
		}

		fv := structElems.Field(i)
		return getFromStructPath(fv.Interface(), path[1:])
	}

	return nil, false, nil
}

// SetFromPath sets out with the value at path from node. out is not set if the path doesn't exist or the value is nil.
// All intermediate along path must be type struct ptr. Out must be either a struct ptr or map ptr.
// TODO: move these out to a separate package (istio/istio#15494).
func SetFromPath(node interface{}, path string, out interface{}) (bool, error) {
	val, found, err := GetFromStructPath(node, path)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	if util.IsValueNil(val) {
		return true, nil
	}

	return true, Set(val, out)
}

// Set sets out with the value at path from node. out is not set if the path doesn't exist or the value is nil.
func Set(val, out interface{}) error {
	// Special case: map out type must be set through map ptr.
	if util.IsMap(val) && util.IsMapPtr(out) {
		reflect.ValueOf(out).Elem().Set(reflect.ValueOf(val))
		return nil
	}
	if util.IsSlice(val) && util.IsSlicePtr(out) {
		reflect.ValueOf(out).Elem().Set(reflect.ValueOf(val))
		return nil
	}

	if reflect.TypeOf(val) != reflect.TypeOf(out) {
		return fmt.Errorf("setFromPath from type %T != to type %T, %v", val, out, util.IsSlicePtr(out))
	}

	if !reflect.ValueOf(out).CanSet() {
		return fmt.Errorf("can't set %v(%T) to out type %T", val, val, out)
	}
	reflect.ValueOf(out).Set(reflect.ValueOf(val))
	return nil
}

// CreatePatchObjectFromPath constructs patch object for node with path, returns nil object and error if the path is invalid.
// eg. node:
//     - name: NEW_VAR
//       value: new_value
// and path:
//       spec.template.spec.containers.[name:discovery].env
//     will constructs the following patch object:
//       spec:
//         template:
//           spec:
//             containers:
//             - name: discovery
//               env:
//               - name: NEW_VAR
//                 value: new_value
func CreatePatchObjectFromPath(node interface{}, path util.Path) (map[string]interface{}, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("empty path %s", path)
	}
	if util.IsKVPathElement(path[0]) {
		return nil, fmt.Errorf("path %s has an unexpected first element %s", path, path[0])
	}
	length := len(path)
	if util.IsKVPathElement(path[length-1]) {
		return nil, fmt.Errorf("path %s has an unexpected last element %s", path, path[length-1])
	}

	patchObj := make(map[string]interface{})
	var currentNode, nextNode interface{}
	nextNode = patchObj
	for i, pe := range path {
		currentNode = nextNode
		// last path element
		if i == length-1 {
			currentNode, ok := currentNode.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("path %s has an unexpected non KV element %s", path, pe)
			}
			currentNode[pe] = node
			break
		}

		if util.IsKVPathElement(pe) {
			currentNode, ok := currentNode.([]interface{})
			if !ok {
				return nil, fmt.Errorf("path %s has an unexpected KV element %s", path, pe)
			}
			k, v, err := util.PathKV(pe)
			if err != nil {
				return nil, err
			}
			if k == "" || v == "" {
				return nil, fmt.Errorf("path %s has an invalid KV element %s", path, pe)
			}
			currentNode[0] = map[string]interface{}{k: v}
			nextNode = currentNode[0]
			continue
		}

		currentNode, ok := currentNode.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("path %s has an unexpected non KV element %s", path, pe)
		}
		// next path element determines the next node type
		if util.IsKVPathElement(path[i+1]) {
			currentNode[pe] = make([]interface{}, 1)
		} else {
			currentNode[pe] = make(map[string]interface{})
		}
		nextNode = currentNode[pe]
	}
	return patchObj, nil
}
