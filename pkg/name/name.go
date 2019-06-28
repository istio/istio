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
	DebugPackage = false
)

// FeatureName is a feature name string, typed to constrain allowed values.
type FeatureName string

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
)

// ComponentName is a component name string, typed to constrain allowed values.
type ComponentName string

const (
	// IstioComponent names corresponding to the IstioControlPlane proto component names. Must be the same, since these
	// are used for struct traversal.
	IstioBaseComponentName       ComponentName = "crds"
	PilotComponentName           ComponentName = "Pilot"
	GalleyComponentName          ComponentName = "Galley"
	SidecarInjectorComponentName ComponentName = "SidecarInjector"
	PolicyComponentName          ComponentName = "Policy"
	TelemetryComponentName       ComponentName = "Telemetry"
	CitadelComponentName         ComponentName = "Citadel"
	CertManagerComponentName     ComponentName = "CertManager"
	NodeAgentComponentName       ComponentName = "NodeAgent"
	IngressComponentName         ComponentName = "Ingress"
	EgressComponentName          ComponentName = "Egress"
)

// ManifestMap is a map of ComponentName to its manifest string.
type ManifestMap map[ComponentName]string

// IsComponentEnabled reports whether the given feature and component are enabled in the given spec. The logic is, in
// order of evaluation:
// 1. if the feature is not defined, the component is disabled, else
// 2. if the feature is disabled, the component is disabled, else
// 3. if the component is not defined, it is reported disabled, else
// 4. if the component disabled, it is reported disabled, else
// 5. the component is enabled.
// This follows the logic description in IstioControlPlane proto.
func IsComponentEnabled(featureName FeatureName, componentName ComponentName, installSpec *v1alpha2.IstioControlPlaneSpec) bool {
	featureNodeI, found, err := GetFromStructPath(installSpec, string(featureName)+".Enabled")
	if err != nil {
		log.Error(err.Error())
		return false
	}
	if !found || featureNodeI == nil {
		return false
	}
	featureNode, ok := featureNodeI.(*protobuf.BoolValue)
	if !ok {
		log.Errorf("feature %s enabled has bad type %T, expect *protobuf.BoolValue", featureName, featureNodeI)
	}
	if featureNode == nil || !featureNode.Value {
		return false
	}

	componentNodeI, found, err := GetFromStructPath(installSpec, string(featureName)+".Components."+string(componentName)+".Common.Enabled")
	if err != nil {
		log.Error(err.Error())
		return featureNode.Value
	}
	if !found || componentNodeI == nil {
		return featureNode.Value
	}
	componentNode, ok := componentNodeI.(*protobuf.BoolValue)
	if !ok {
		log.Errorf("component %s enabled has bad type %T, expect *protobuf.BoolValue", componentName, componentNodeI)
		return featureNode.Value
	}
	if componentNode == nil {
		return featureNode.Value
	}
	return componentNode.Value
}

// Namespace returns the namespace for the component. It follows these rules:
// 1. If CustomPackagePath is unset, log and error and return the empty string.
// 2. If the feature and component namespaces are unset, return CustomPackagePath.
// 3. If the feature namespace is set but component name is unset, return the feature namespace.
// 4. Otherwise return the component namespace.
func Namespace(featureName FeatureName, componentName ComponentName, controlPlaneSpec *v1alpha2.IstioControlPlaneSpec) string {
	defaultNamespaceI, found, err := GetFromStructPath(controlPlaneSpec, "DefaultNamespacePrefix")
	if !found {
		log.Error("can't find any default for DefaultNamespacePrefix")
		return ""
	}
	if err != nil {
		log.Error(err.Error())
		return ""

	}
	defaultNamespace, ok := defaultNamespaceI.(string)
	if !ok {
		log.Errorf("CustomPackagePath has bad type %T, expect string", defaultNamespaceI)
		return ""
	}

	featureNamespace := defaultNamespace
	featureNodeI, found, err := GetFromStructPath(controlPlaneSpec, string(featureName)+"Components.Namespace")
	if err != nil {
		log.Error(err.Error())
		return featureNamespace
	}
	if found && featureNodeI != nil {
		featureNamespace, ok = featureNodeI.(string)
		if !ok {
			log.Errorf("feature %s namespace has bad type %T, expect string", featureName, featureNodeI)
			return defaultNamespace
		}
		if featureNamespace == "" {
			featureNamespace = defaultNamespace
		}
	}

	componentNodeI, found, err := GetFromStructPath(controlPlaneSpec, string(featureName)+".Components."+string(componentName)+".Common.Namespace")
	if err != nil {
		log.Error(err.Error())
		return featureNamespace
	}
	if !found {
		return featureNamespace
	}
	if componentNodeI == nil {
		return featureNamespace
	}
	componentNamespace, ok := componentNodeI.(string)
	if !ok {
		log.Errorf("component %s enabled has bad type %T, expect string", componentName, componentNodeI)
		return featureNamespace
	}
	if componentNamespace == "" {
		return featureNamespace
	}
	return componentNamespace
}

// GetFromStructPath returns the value at path from the given node, or false if the path does not exist.
// Node and all intermediate along path must be type struct ptr.
func GetFromStructPath(node interface{}, path string) (interface{}, bool, error) {
	return getFromStructPath(node, util.PathFromString(path))
}

// getFromStructPath is the internal implementation of GetFromStructPath which recurses through a tree of Go structs
// given a path. It terminates when the end of the path is reached or a path element does not exist.
func getFromStructPath(node interface{}, path util.Path) (interface{}, bool, error) {
	dbgPrint("getFromStructPath path=%s, node(%T)", path, node)
	if len(path) == 0 {
		dbgPrint("getFromStructPath returning node(%T)%v", node, node)
		return node, !util.IsValueNil(node), nil
	}
	kind := reflect.TypeOf(node).Kind()
	var structElems reflect.Value
	switch kind {
	case reflect.Map, reflect.Slice:
		if len(path) != 0 {
			return nil, false, fmt.Errorf("getFromStructPath path %s, unsupported leaf type %T", path, node)
		}
	case reflect.Ptr:
		structElems = reflect.ValueOf(node).Elem()
		if reflect.TypeOf(structElems).Kind() != reflect.Struct {
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

// TODO: move these out to a separate package.
// SetFromPath sets out with the value at path from node. out is not set if the path doesn't exist or the value is nil.
// All intermediate along path must be type struct ptr. Out must be either a struct ptr or map ptr.
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

func dbgPrint(v ...interface{}) {
	if !DebugPackage {
		return
	}
	log.Infof(fmt.Sprintf(v[0].(string), v[1:]...))
}
