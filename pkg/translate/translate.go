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

// Package translate defines translations from installer proto to values.yaml.
package translate

import (
	"bytes"
	"fmt"
	"html/template"
	"reflect"
	"strings"

	"github.com/ghodss/yaml"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/manifest"
	"istio.io/operator/pkg/name"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/version"
	"istio.io/pkg/log"
)

var (
	// DebugPackage controls detailed debug output for this package.
	DebugPackage = false
)

// Translator is a set of mappings to translate between API paths, charts, values.yaml and k8s paths.
type Translator struct {
	// Translations remain the same within a minor version.
	Version version.MinorVersion
	// APIMapping is a mapping between an API path and the corresponding values.yaml path using longest prefix
	// match. If the path is a non-leaf node, the output path is the matching portion of the path, plus any remaining
	// output path.
	APIMapping map[string]*Translation
	// KubernetesMapping defines mappings from an IstioControlPlane API paths to k8s resource paths.
	KubernetesMapping map[string]*Translation
	// FeatureComponentToValues maps from feature and component names to value paths in values.yaml schema.
	FeatureComponentToValues map[name.FeatureName]map[name.ComponentName]componentValuePaths
	// ComponentMaps is a set of mappings for each Istio component.
	ComponentMaps map[name.ComponentName]*ComponentMaps
}

// ComponentMaps is a set of mappings for an Istio component.
type ComponentMaps struct {
	// ResourceName maps a ComponentName to the name of the rendered k8s resource.
	ResourceName string
	// ContainerName maps a ComponentName to the name of the container in a Deployment.
	ContainerName string
	// HelmSubdir is a mapping between a component name and the subdirectory of the component Chart.
	HelmSubdir string
	// ToHelmValuesNames is the baseYAML component name used in values YAML files in component charts.
	ToHelmValuesNames string
	// AlwaysEnabled controls whether a component can be turned off through IstioControlPlaneSpec.
	AlwaysEnabled bool
}

// TranslationFunc maps a yamlStr API path into a YAML values tree.
type TranslationFunc func(t *Translation, root map[string]interface{}, valuesPath string, value interface{}) error

// Translation is a mapping to an output path using a translation function.
type Translation struct {
	outPath         string
	translationFunc TranslationFunc
}

// componentValuePaths is a group of paths that exists in both IstioControlPlane and values.yaml.
type componentValuePaths struct {
	enabled   string
	namespace string
}

var (
	// Translators is a map of minor versions to Translator for that version.
	// TODO: this should probably be moved out to a config file that's versioned.
	Translators = map[version.MinorVersion]*Translator{
		version.NewMinorVersion(1, 2): {
			APIMapping: map[string]*Translation{
				"Hub":         {"global.hub", nil},
				"Tag":         {"global.tag", nil},
				"K8SDefaults": {"global.resources", nil},

				"TrafficManagement.Components.Proxy.Common.Values": {"global.proxy", nil},

				"PolicyTelemetry.PolicyCheckFailOpen":       {"global.policyCheckFailOpen", nil},
				"PolicyTelemetry.OutboundTrafficPolicyMode": {"global.outboundTrafficPolicy.mode", nil},

				"Security.ControlPlaneMtls.Value":    {"global.controlPlaneSecurityEnabled", nil},
				"Security.DataPlaneMtlsStrict.Value": {"global.mtls.enabled", nil},
			},
			KubernetesMapping: map[string]*Translation{
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.Affinity":            {"[Deployment:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].affinity", nil},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.Env":                 {"[Deployment:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].env", nil},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.HpaSpec":             {"[HorizontalPodAutoscaler:{{.ResourceName}}].spec", nil},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.ImagePullPolicy":     {"[Deployment:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].imagePullPolicy", nil},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.NodeSelector":        {"[Deployment:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].nodeSelector", nil},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.PodDisruptionBudget": {"[PodDisruptionBudget:{{.ResourceName}}].spec", nil},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.PodAnnotations":      {"[Deployment:{{.ResourceName}}].spec.template.metadata.annotations", nil},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.PriorityClassName":   {"[Deployment:{{.ResourceName}}].spec.template.spec.priorityClassName.", nil},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.ReadinessProbe":      {"[Deployment:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].readinessProbe", nil},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.ReplicaCount":        {"[Deployment:{{.ResourceName}}].spec.replicas", nil},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.Resources":           {"[Deployment:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].resources", nil},
			},
			ComponentMaps: map[name.ComponentName]*ComponentMaps{
				name.IstioBaseComponentName: {
					ToHelmValuesNames: "global",
					HelmSubdir:        "crds",
					AlwaysEnabled:     true,
				},
				name.PilotComponentName: {
					ResourceName:      "istio-pilot",
					ContainerName:     "discovery",
					HelmSubdir:        "istio-control/istio-discovery",
					ToHelmValuesNames: "pilot",
				},

				name.GalleyComponentName: {
					ResourceName:      "istio-galley",
					ContainerName:     "galley",
					HelmSubdir:        "istio-control/istio-config",
					ToHelmValuesNames: "galley",
				},
				name.SidecarInjectorComponentName: {
					ResourceName:      "istio-sidecar-injector",
					ContainerName:     "sidecar-injector-webhook",
					HelmSubdir:        "istio-control/istio-autoinject",
					ToHelmValuesNames: "sidecarInjectorWebhook",
				},
				name.PolicyComponentName: {
					ResourceName:      "istio-policy",
					ContainerName:     "mixer",
					HelmSubdir:        "istio-policy",
					ToHelmValuesNames: "mixer.policy",
				},
				name.TelemetryComponentName: {
					ResourceName:      "istio-telemetry",
					ContainerName:     "mixer",
					HelmSubdir:        "istio-telemetry/mixer-telemetry",
					ToHelmValuesNames: "mixer.telemetry",
				},
				name.CitadelComponentName: {
					ResourceName:      "istio-citadel",
					ContainerName:     "citadel",
					HelmSubdir:        "security/citadel",
					ToHelmValuesNames: "citadel",
				},
				name.NodeAgentComponentName: {
					ResourceName:      "istio-nodeagent",
					ContainerName:     "nodeagent",
					HelmSubdir:        "security/nodeagent",
					ToHelmValuesNames: "nodeAgent",
				},
				name.CertManagerComponentName: {
					ResourceName:      "certmanager",
					ContainerName:     "certmanager",
					HelmSubdir:        "security/certmanager",
					ToHelmValuesNames: "certManager",
				},
				name.IngressComponentName: {
					ResourceName:      "istio-ingressgateway",
					ContainerName:     "istio-proxy",
					HelmSubdir:        "gateways/istio-ingress",
					ToHelmValuesNames: "gateways.istio-ingressgateway",
				},
				name.EgressComponentName: {
					ResourceName:      "istio-egressgateway",
					ContainerName:     "istio-proxy",
					HelmSubdir:        "gateways/istio-egress",
					ToHelmValuesNames: "gateways.istio-egressgateway",
				},
			},
			FeatureComponentToValues: map[name.FeatureName]map[name.ComponentName]componentValuePaths{
				name.TrafficManagementFeatureName: {
					name.PilotComponentName: {
						enabled:   "pilot.enabled",
						namespace: "global.istioNamespace",
					},
				},
				name.PolicyFeatureName: {
					name.PolicyComponentName: {
						enabled:   "mixer.policy.enabled",
						namespace: "global.policyNamespace",
					},
				},
				name.ConfigManagementFeatureName: {
					name.GalleyComponentName: {
						enabled: "galley.enabled",
					},
				},
				name.TelemetryFeatureName: {
					name.TelemetryComponentName: {
						enabled:   "mixer.telemetry.enabled",
						namespace: "global.telemetryNamespace",
					},
				},
				// TODO: check if these really should be settable.
				name.SecurityFeatureName: {
					name.NodeAgentComponentName: {
						enabled:   "nodeagent.enabled",
						namespace: "global.istioNamespace",
					},
					name.CertManagerComponentName: {
						enabled:   "certmanager.enabled",
						namespace: "global.istioNamespace",
					},
					name.CitadelComponentName: {
						enabled:   "security.enabled",
						namespace: "global.istioNamespace",
					},
				},
			},
		},
	}
)

// OverlayK8sSettings overlays k8s settings from icp over the manifest objects, based on t's translation mappings.
func (t *Translator) OverlayK8sSettings(yml string, icp *v1alpha2.IstioControlPlaneSpec, featureName name.FeatureName, componentName name.ComponentName) (string, error) {
	objects, err := manifest.ParseObjectsFromYAMLManifest(yml)
	if err != nil {
		return "", err
	}
	log.Infof("Manifest contains the following objects:")
	for _, o := range objects {
		log.Infof("%s", o.HashNameKind())
	}
	// om is a map of kind:name string to Object ptr.
	om := objects.ToNameKindMap()
	for inPath, v := range t.KubernetesMapping {
		inPath = featureComponentString(inPath, featureName, componentName)
		log.Infof("Checking for path %s in IstioControlPlaneSpec", inPath)
		m, found, err := name.GetFromStructPath(icp, inPath)
		if err != nil {
			return "", err
		}
		if !found {
			log.Infof("path %s not found in IstioControlPlaneSpec, skip mapping.", inPath)
			continue
		}
		if mstr, ok := m.(string); ok && mstr == "" {
			log.Infof("path %s is empty string, skip mapping.", inPath)
			continue
		}
		overlayYAML, err := yaml.Marshal(m)
		if err != nil {
			return "", err
		}
		outPath := t.resourceContainerString(v.outPath, componentName)
		log.Infof("path has value in IstioControlPlaneSpec, mapping to output path %s", outPath)
		path := util.PathFromString(outPath)
		pe := path[0]
		// Output path must start with [kind:name], which is used to map to the object to overlay.
		if !util.IsKVPathElement(pe) {
			return "", fmt.Errorf("path %s has an unexpected first element %s in OverlayK8sSettings", path, pe)
		}
		// After brackets are removed, the remaining "kind:name" string maps to the right object in om.
		pe, _ = util.RemoveBrackets(pe)
		oo, ok := om[pe]
		if !ok {
			return "", fmt.Errorf("resource Kind:name %s doesn't exist in the output manifest:\n%s", pe, yml)
		}

		baseYAML, err := oo.YAML()
		if err != nil {
			return "", err
		}
		mergedYAML, err := overlayK8s(baseYAML, overlayYAML, path[1:])
		if err != nil {
			return "", err
		}
		dbgPrint("baseYAML:\n%s\n, overlayYAML:\n%s\n, mergedYAML:\n%s\n", string(baseYAML), string(overlayYAML), string(mergedYAML))

		mergedObj, err := manifest.ParseYAMLToObject(mergedYAML)
		if err != nil {
			return "", err
		}
		// Update the original object in objects slice, since the output should be ordered.
		*(om[pe]) = *mergedObj
	}

	return objects.YAML()
}

// overlayK8s overlays overlayYAML over baseYAML at the given path in baseYAML.
func overlayK8s(baseYAML, overlayYAML []byte, path util.Path) ([]byte, error) {
	base, overlayMap := make(map[string]interface{}), make(map[string]interface{})
	var overlay interface{} = overlayMap
	if err := yaml.Unmarshal(baseYAML, &base); err != nil {
		return nil, fmt.Errorf("overlayK8s: %s for baseYAML:\n%s", err, baseYAML)
	}
	if err := yaml.Unmarshal(overlayYAML, &overlayMap); err != nil {
		// May be a scalar type, try to unmarshal into interface instead.
		if err := yaml.Unmarshal(overlayYAML, &overlay); err != nil {
			return nil, fmt.Errorf("overlayK8s: %s for overlayYAML:\n%s", err, overlayYAML)
		}
	}
	if err := setTree(base, path, overlay); err != nil {
		return nil, err
	}
	return yaml.Marshal(base)
}

// ProtoToValues traverses the supplied IstioControlPlaneSpec and returns a values.yaml translation from it.
func (t *Translator) ProtoToValues(ii *v1alpha2.IstioControlPlaneSpec) (string, error) {
	root := make(map[string]interface{})

	errs := t.protoToValues(ii, root, nil)
	if len(errs) != 0 {
		return "", errs.ToError()
	}

	if err := t.setEnablementAndNamespaces(root, ii); err != nil {
		return "", err
	}

	if len(root) == 0 {
		return "", nil
	}

	y, err := yaml.Marshal(root)
	if err != nil {
		return "", util.AppendErr(errs, err).ToError()
	}

	return string(y), errs.ToError()
}

// ValuesOverlaysToHelmValues translates from component value overlays to helm value overlay paths.
func (t *Translator) ValuesOverlaysToHelmValues(in map[string]interface{}, cname name.ComponentName) map[string]interface{} {
	out := make(map[string]interface{})
	toPath := t.ComponentMaps[cname].ToHelmValuesNames
	if toPath == "" {
		log.Errorf("missing translation path for %s in ValuesOverlaysToHelmValues", cname)
		return nil
	}
	pv := strings.Split(toPath, ".")
	cur := out
	for len(pv) > 1 {
		cur[pv[0]] = make(map[string]interface{})
		cur = cur[pv[0]].(map[string]interface{})
		pv = pv[1:]
	}
	cur[pv[0]] = in
	return out
}

// protoToValues takes an interface which must be a struct ptr and recursively iterates through all its fields.
// For each leaf, if looks for a mapping from the struct data path to the corresponding YAML path and if one is
// found, it calls the associated mapping function if one is defined to populate the values YAML path.
// If no mapping function is defined, it uses the default mapping function.
func (t *Translator) protoToValues(structPtr interface{}, root map[string]interface{}, path util.Path) (errs util.Errors) {
	dbgPrint("protoToValues with path %s, %v (%T)", path, structPtr, structPtr)
	if structPtr == nil {
		return nil
	}

	structElems := reflect.ValueOf(structPtr)
	if reflect.TypeOf(structPtr).Kind() == reflect.Ptr {
		structElems = structElems.Elem()
	}
	if reflect.TypeOf(structElems).Kind() != reflect.Struct {
		return util.NewErrs(fmt.Errorf("protoToValues path %s, value: %v, expected struct or struct ptr, got %T", path, structPtr, structPtr))
	}

	if util.IsNilOrInvalidValue(structElems) {
		return
	}

	for i := 0; i < structElems.NumField(); i++ {
		fieldName := structElems.Type().Field(i).Name
		fieldValue := structElems.Field(i)
		kind := structElems.Type().Field(i).Type.Kind()
		if a, ok := structElems.Type().Field(i).Tag.Lookup("json"); ok && a == "-" {
			continue
		}

		dbgPrint("Checking field %s", fieldName)
		switch kind {
		case reflect.Struct:
			dbgPrint("Struct")
			errs = util.AppendErrs(errs, t.protoToValues(fieldValue.Addr().Interface(), root, append(path, fieldName)))
		case reflect.Map:
			dbgPrint("Map")
			newPath := append(path, fieldName)
			for _, key := range fieldValue.MapKeys() {
				nnp := append(newPath, key.String())
				errs = util.AppendErrs(errs, t.insertLeaf(root, nnp, fieldValue.MapIndex(key)))
			}
		case reflect.Slice:
			dbgPrint("Slice")
			newPath := append(path, fieldName)
			for i := 0; i < fieldValue.Len(); i++ {
				errs = util.AppendErrs(errs, t.protoToValues(fieldValue.Index(i).Interface(), root, newPath))
			}
		case reflect.Ptr:
			if util.IsNilOrInvalidValue(fieldValue.Elem()) {
				continue
			}
			newPath := append(path, fieldName)
			if fieldValue.Elem().Kind() == reflect.Struct {
				dbgPrint("Struct Ptr")
				errs = util.AppendErrs(errs, t.protoToValues(fieldValue.Interface(), root, newPath))
			} else {
				dbgPrint("Leaf Ptr")
				errs = util.AppendErrs(errs, t.insertLeaf(root, newPath, fieldValue))
			}
		default:
			dbgPrint("field has kind %s", kind)
			if structElems.Field(i).CanInterface() {
				errs = util.AppendErrs(errs, t.insertLeaf(root, append(path, fieldName), fieldValue))
			}
		}
	}
	return errs
}

// setEnablementAndNamespaces translates the enablement and namespace value of each component in the baseYAML values
// tree, based on feature/component inheritance relationship.
func (t *Translator) setEnablementAndNamespaces(root map[string]interface{}, ii *v1alpha2.IstioControlPlaneSpec) error {
	for fn, f := range t.FeatureComponentToValues {
		for cn, c := range f {
			if c.enabled != "" {
				if err := setTree(root, util.PathFromString(c.enabled), name.IsComponentEnabled(fn, cn, ii)); err != nil {
					return err
				}
			}
			if c.namespace != "" {
				if err := setTree(root, util.PathFromString(c.namespace), name.Namespace(fn, cn, ii)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// insertLeaf inserts a leaf with value into root at path, which is first mapped using t.APIMapping.
func (t *Translator) insertLeaf(root map[string]interface{}, path util.Path, value reflect.Value) (errs util.Errors) {
	// Must be a scalar leaf. See if we have a mapping.
	valuesPath, m := getValuesPathMapping(t.APIMapping, path)
	var v interface{}
	if value.Kind() == reflect.Ptr {
		v = value.Elem().Interface()
	} else {
		v = value.Interface()
	}
	switch {
	case m == nil:
		break
	case m.translationFunc == nil:
		// Use default translation which just maps to a different part of the tree.
		errs = util.AppendErr(errs, defaultTranslationFunc(m, root, valuesPath, v))
	default:
		// Use a custom translation function.
		errs = util.AppendErr(errs, m.translationFunc(m, root, valuesPath, v))
	}
	return errs
}

// getValuesPathMapping tries to map path against the passed in mappings with a longest prefix match. If a matching prefix
// is found, it returns the translated YAML path and the corresponding translation.
// e.g. for mapping "a.b"  -> "1.2", the input path "a.b.c.d" would yield "1.2.c.d".
func getValuesPathMapping(mappings map[string]*Translation, path util.Path) (string, *Translation) {
	p := path
	var m *Translation
	for ; len(p) > 0; p = p[0 : len(p)-1] {
		m = mappings[p.String()]
		if m != nil {
			break
		}
	}
	if m == nil {
		return "", nil
	}

	if m.outPath == "" {
		return "", m
	}

	out := m.outPath + "." + path[len(p):].String()
	dbgPrint("translating %s to %s", path, out)
	return out, m
}

// featureComponentString renders a template of the form <path>{{.FeatureName}}<path>{{.ComponentName}}<path> with
// the supplied parameters.
func featureComponentString(tmpl string, featureName name.FeatureName, componentName name.ComponentName) string {
	type Temp struct {
		FeatureName   name.FeatureName
		ComponentName name.ComponentName
	}
	ts := Temp{
		FeatureName:   featureName,
		ComponentName: componentName,
	}
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		log.Error(err.Error())
		return err.Error()
	}
	buf := new(bytes.Buffer)
	err = t.Execute(buf, ts)
	if err != nil {
		log.Error(err.Error())
		return err.Error()
	}
	return buf.String()
}

// resourceContainerString renders a template of the form <path>{{.ResourceName}}<path>{{.ContainerName}}<path> with
// the supplied parameters.
func (t *Translator) resourceContainerString(tmpl string, componentName name.ComponentName) string {
	ts := struct {
		ResourceName  string
		ContainerName string
	}{
		ResourceName:  t.ComponentMaps[componentName].ResourceName,
		ContainerName: t.ComponentMaps[componentName].ContainerName,
	}
	// TODO: address comment
	// Can extract the template execution part to a common method, so for each
	// rendering method just need to create a template struct and call this common method
	tm, err := template.New("").Parse(tmpl)
	if err != nil {
		return err.Error()
	}
	buf := new(bytes.Buffer)
	err = tm.Execute(buf, ts)
	if err != nil {
		return err.Error()
	}
	return buf.String()
}

// defaultTranslationFunc is the default translation to values. It maps a Go data path into a YAML path.
func defaultTranslationFunc(m *Translation, root map[string]interface{}, valuesPath string, value interface{}) error {
	var path []string

	if util.IsEmptyString(value) {
		dbgPrint("Skip empty string value for path %s", m.outPath)
		return nil
	}
	if valuesPath == "" {
		dbgPrint("Not mapping to values, resources path is %s", m.outPath)
		return nil
	}

	for _, p := range util.PathFromString(valuesPath) {
		path = append(path, firstCharToLower(p))
	}

	return setTree(root, path, value)
}

func dbgPrint(v ...interface{}) {
	if !DebugPackage {
		return
	}
	log.Infof(fmt.Sprintf(v[0].(string), v[1:]...))
}

func firstCharToLower(s string) string {
	return strings.ToLower(s[0:1]) + s[1:]
}
