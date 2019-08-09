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
	"istio.io/operator/pkg/name"
	"istio.io/operator/pkg/object"
	"istio.io/operator/pkg/tpath"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/version"
	"istio.io/pkg/log"
)

const (
	// HelmValuesEnabledSubpath is the subpath from the component root to the enabled parameter.
	HelmValuesEnabledSubpath = "enabled"
	// HelmValuesNamespaceSubpath is the subpath from the component root to the namespace parameter.
	HelmValuesNamespaceSubpath = "namespace"
)

var (
	scope = log.RegisterScope("translator", "API translator", 0)
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
	// ToFeature maps a component to its parent feature.
	ToFeature map[name.ComponentName]name.FeatureName
	// GlobalNamespaces maps feature namespaces to Helm global namespace definitions.
	GlobalNamespaces map[name.ComponentName]string
	// ComponentMaps is a set of mappings for each Istio component.
	ComponentMaps map[name.ComponentName]*ComponentMaps

	// featureToComponents maps feature names to their component names.
	featureToComponents map[name.FeatureName][]name.ComponentName
}

// ComponentMaps is a set of mappings for an Istio component.
type ComponentMaps struct {
	// ResourceName maps a ComponentName to the name of the rendered k8s resource.
	ResourceName string
	// ContainerName maps a ComponentName to the name of the container in a Deployment.
	ContainerName string
	// HelmSubdir is a mapping between a component name and the subdirectory of the component Chart.
	HelmSubdir string
	// ToHelmValuesTreeRoot is the tree root in values YAML files for the component.
	ToHelmValuesTreeRoot string
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

var (
	// translators is a map of minor versions to Translator for that version.
	// TODO: this should probably be moved out to a config file that's versioned.
	translators = map[version.MinorVersion]*Translator{
		version.NewMinorVersion(1, 3): {
			APIMapping: map[string]*Translation{
				"Hub":              {"global.hub", nil},
				"Tag":              {"global.tag", nil},
				"K8SDefaults":      {"global.resources", nil},
				"DefaultNamespace": {"global.istioNamespace", nil},

				"TrafficManagement.Components.Proxy.Common.Values": {"global.proxy", nil},

				"ConfigManagement.Components.Namespace": {"global.configNamespace", nil},

				"Policy.PolicyCheckFailOpen":       {"global.policyCheckFailOpen", nil},
				"Policy.OutboundTrafficPolicyMode": {"global.outboundTrafficPolicy.mode", nil},
				"Policy.Components.Namespace":      {"global.policyNamespace", nil},

				"Telemetry.Components.Namespace": {"global.telemetryNamespace", nil},

				"Security.ControlPlaneMtls.Value":    {"global.controlPlaneSecurityEnabled", nil},
				"Security.DataPlaneMtlsStrict.Value": {"global.mtls.enabled", nil},
				"Security.Components.Namespace":      {"global.securityNamespace", nil},
			},
			KubernetesMapping: map[string]*Translation{
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.Affinity": {
					"[Deployment:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].affinity",
					nil,
				},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.Env": {
					"[Deployment:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].env",
					nil,
				},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.HpaSpec": {
					"[HorizontalPodAutoscaler:{{.ResourceName}}].spec",
					nil,
				},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.ImagePullPolicy": {
					"[Deployment:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].imagePullPolicy",
					nil,
				},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.NodeSelector": {
					"[Deployment:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].nodeSelector",
					nil,
				},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.PodDisruptionBudget": {
					"[PodDisruptionBudget:{{.ResourceName}}].spec",
					nil,
				},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.PodAnnotations": {
					"[Deployment:{{.ResourceName}}].spec.template.metadata.annotations",
					nil,
				},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.PriorityClassName": {
					"[Deployment:{{.ResourceName}}].spec.template.spec.priorityClassName.",
					nil,
				},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.ReadinessProbe": {
					"[Deployment:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].readinessProbe",
					nil,
				},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.ReplicaCount": {
					"[Deployment:{{.ResourceName}}].spec.replicas",
					nil,
				},
				"{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8S.Resources": {
					"[Deployment:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].resources",
					nil,
				},
			},
			ToFeature: map[name.ComponentName]name.FeatureName{
				name.IstioBaseComponentName:       name.IstioBaseFeatureName,
				name.PilotComponentName:           name.TrafficManagementFeatureName,
				name.GalleyComponentName:          name.ConfigManagementFeatureName,
				name.SidecarInjectorComponentName: name.AutoInjectionFeatureName,
				name.PolicyComponentName:          name.PolicyFeatureName,
				name.TelemetryComponentName:       name.TelemetryFeatureName,
				name.CitadelComponentName:         name.SecurityFeatureName,
				name.CertManagerComponentName:     name.SecurityFeatureName,
				name.NodeAgentComponentName:       name.SecurityFeatureName,
				name.IngressComponentName:         name.GatewayFeatureName,
				name.EgressComponentName:          name.GatewayFeatureName,
			},
			GlobalNamespaces: map[name.ComponentName]string{
				name.PilotComponentName:      "istioNamespace",
				name.GalleyComponentName:     "configNamespace",
				name.TelemetryComponentName:  "telemetryNamespace",
				name.PolicyComponentName:     "policyNamespace",
				name.PrometheusComponentName: "prometheusNamespace",
				name.CitadelComponentName:    "securityNamespace",
			},
			ComponentMaps: map[name.ComponentName]*ComponentMaps{
				name.IstioBaseComponentName: {
					ToHelmValuesTreeRoot: "global",
					HelmSubdir:           "crds",
					AlwaysEnabled:        true,
				},
				name.PilotComponentName: {
					ResourceName:         "istio-pilot",
					ContainerName:        "discovery",
					HelmSubdir:           "istio-control/istio-discovery",
					ToHelmValuesTreeRoot: "pilot",
				},

				name.GalleyComponentName: {
					ResourceName:         "istio-galley",
					ContainerName:        "galley",
					HelmSubdir:           "istio-control/istio-config",
					ToHelmValuesTreeRoot: "galley",
				},
				name.SidecarInjectorComponentName: {
					ResourceName:         "istio-sidecar-injector",
					ContainerName:        "sidecar-injector-webhook",
					HelmSubdir:           "istio-control/istio-autoinject",
					ToHelmValuesTreeRoot: "sidecarInjectorWebhook",
				},
				name.PolicyComponentName: {
					ResourceName:         "istio-policy",
					ContainerName:        "mixer",
					HelmSubdir:           "istio-policy",
					ToHelmValuesTreeRoot: "mixer.policy",
				},
				name.TelemetryComponentName: {
					ResourceName:         "istio-telemetry",
					ContainerName:        "mixer",
					HelmSubdir:           "istio-telemetry/mixer-telemetry",
					ToHelmValuesTreeRoot: "mixer.telemetry",
				},
				name.CitadelComponentName: {
					ResourceName:         "istio-citadel",
					ContainerName:        "citadel",
					HelmSubdir:           "security/citadel",
					ToHelmValuesTreeRoot: "citadel",
				},
				name.NodeAgentComponentName: {
					ResourceName:         "istio-nodeagent",
					ContainerName:        "nodeagent",
					HelmSubdir:           "security/nodeagent",
					ToHelmValuesTreeRoot: "nodeagent",
				},
				name.CertManagerComponentName: {
					ResourceName:         "certmanager",
					ContainerName:        "certmanager",
					HelmSubdir:           "security/certmanager",
					ToHelmValuesTreeRoot: "certmanager",
				},
				name.IngressComponentName: {
					ResourceName:         "istio-ingressgateway",
					ContainerName:        "istio-proxy",
					HelmSubdir:           "gateways/istio-ingress",
					ToHelmValuesTreeRoot: "gateways.istio-ingressgateway",
				},
				name.EgressComponentName: {
					ResourceName:         "istio-egressgateway",
					ContainerName:        "istio-proxy",
					HelmSubdir:           "gateways/istio-egress",
					ToHelmValuesTreeRoot: "gateways.istio-egressgateway",
				},
			},
		},
	}
)

// NewTranslator creates a new Translator for minorVersion and returns a ptr to it.
func NewTranslator(minorVersion version.MinorVersion) (*Translator, error) {
	t := translators[minorVersion]
	if t == nil {
		return nil, fmt.Errorf("no translator available for version %s", minorVersion)
	}

	t.featureToComponents = make(map[name.FeatureName][]name.ComponentName)
	for c, f := range t.ToFeature {
		t.featureToComponents[f] = append(t.featureToComponents[f], c)
	}
	return t, nil
}

// OverlayK8sSettings overlays k8s settings from icp over the manifest objects, based on t's translation mappings.
func (t *Translator) OverlayK8sSettings(yml string, icp *v1alpha2.IstioControlPlaneSpec, componentName name.ComponentName) (string, error) {
	objects, err := object.ParseK8sObjectsFromYAMLManifest(yml)
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
		inPath, err := renderFeatureComponentPathTemplate(inPath, t.ToFeature[componentName], componentName)
		if err != nil {
			return "", err
		}
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
		// Zero int values are due to proto3 compiling to scalars rather than ptrs. Skip these because values of 0 are
		// the default in destination fields and need not be set explicitly.
		if mint, ok := util.ToIntValue(m); ok && mint == 0 {
			log.Infof("path %s is int 0, skip mapping.", inPath)
			continue
		}
		overlayYAML, err := yaml.Marshal(m)
		if err != nil {
			return "", err
		}
		outPath, err := t.renderResourceComponentPathTemplate(v.outPath, componentName)
		if err != nil {
			return "", err
		}
		log.Infof("path has value in IstioControlPlaneSpec, mapping to output path %s", outPath)
		path := util.PathFromString(outPath)
		pe := path[0]
		// Output path must start with [kind:name], which is used to map to the object to overlay.
		if !util.IsKVPathElement(pe) {
			return "", fmt.Errorf("path %s has an unexpected first element %s in OverlayK8sSettings", path, pe)
		}
		// After brackets are removed, the remaining "kind:name" is the same format as the keys in om.
		pe, _ = util.RemoveBrackets(pe)
		oo, ok := om[pe]
		if !ok {
			return "", fmt.Errorf("resource Kind:name %s doesn't exist in the output manifest:\n%s", pe, yml)
		}

		baseYAML, err := oo.YAML()
		if err != nil {
			return "", fmt.Errorf("could not marshal to YAML in OverlayK8sSettings: %s", err)
		}
		mergedYAML, err := overlayK8s(baseYAML, overlayYAML, path[1:])
		if err != nil {
			return "", err
		}
		scope.Debugf("baseYAML:\n%s\n, overlayYAML:\n%s\n, mergedYAML:\n%s\n", string(baseYAML), string(overlayYAML), string(mergedYAML))

		mergedObj, err := object.ParseYAMLToK8sObject(mergedYAML)
		if err != nil {
			return "", fmt.Errorf("could not ParseYAMLToK8sObject in OverlayK8sSettings: %s", err)
		}
		// Update the original object in objects slice, since the output should be ordered.
		*(om[pe]) = *mergedObj
	}

	return objects.YAMLManifest()
}

// overlayK8s overlays overlayYAML over baseYAML at the given path in baseYAML.
func overlayK8s(baseYAML, overlayYAML []byte, path util.Path) ([]byte, error) {
	base, overlayMap := make(map[string]interface{}), make(map[string]interface{})
	var overlay interface{} = overlayMap
	if err := yaml.Unmarshal(baseYAML, &base); err != nil {
		return nil, fmt.Errorf("failed to unmarshal in overlayK8s: %s for baseYAML:\n%s", err, baseYAML)
	}
	if err := yaml.Unmarshal(overlayYAML, &overlayMap); err != nil {
		// May be a scalar type, try to unmarshal into interface instead.
		if err := yaml.Unmarshal(overlayYAML, &overlay); err != nil {
			return nil, fmt.Errorf("failed to unmarshal in overlayK8s: %s for overlayYAML:\n%s", err, overlayYAML)
		}
	}
	if err := tpath.WriteNode(base, path, overlay); err != nil {
		return nil, err
	}
	return yaml.Marshal(base)
}

// ProtoToValues traverses the supplied IstioControlPlaneSpec and returns a values.yaml translation from it.
func (t *Translator) ProtoToValues(ii *v1alpha2.IstioControlPlaneSpec) (string, error) {
	root := make(map[string]interface{})

	errs := t.protoToHelmValues(ii, root, nil)
	if len(errs) != 0 {
		return "", errs.ToError()
	}

	// Enabled and namespace fields require special handling because of inheritance rules.
	if err := t.setEnablementAndNamespaces(root, ii); err != nil {
		return "", err
	}

	// Return blank string for empty case.
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
	toPath := t.ComponentMaps[cname].ToHelmValuesTreeRoot
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

// Components returns the Components under the featureName feature.
func (t *Translator) Components(featureName name.FeatureName) []name.ComponentName {
	return t.featureToComponents[featureName]
}

// protoToHelmValues takes an interface which must be a struct ptr and recursively iterates through all its fields.
// For each leaf, if looks for a mapping from the struct data path to the corresponding YAML path and if one is
// found, it calls the associated mapping function if one is defined to populate the values YAML path.
// If no mapping function is defined, it uses the default mapping function.
func (t *Translator) protoToHelmValues(node interface{}, root map[string]interface{}, path util.Path) (errs util.Errors) {
	scope.Debugf("protoToHelmValues with path %s, %v (%T)", path, node, node)
	if util.IsValueNil(node) {
		return nil
	}

	vv := reflect.ValueOf(node)
	vt := reflect.TypeOf(node)
	switch vt.Kind() {
	case reflect.Ptr:
		if !util.IsNilOrInvalidValue(vv.Elem()) {
			errs = util.AppendErrs(errs, t.protoToHelmValues(vv.Elem().Interface(), root, path))
		}
	case reflect.Struct:
		scope.Debug("Struct")
		for i := 0; i < vv.NumField(); i++ {
			fieldName := vv.Type().Field(i).Name
			fieldValue := vv.Field(i)
			scope.Debugf("Checking field %s", fieldName)
			if a, ok := vv.Type().Field(i).Tag.Lookup("json"); ok && a == "-" {
				continue
			}
			errs = util.AppendErrs(errs, t.protoToHelmValues(fieldValue.Interface(), root, append(path, fieldName)))
		}
	case reflect.Map:
		scope.Debug("Map")
		for _, key := range vv.MapKeys() {
			nnp := append(path, key.String())
			errs = util.AppendErrs(errs, t.insertLeaf(root, nnp, vv.MapIndex(key)))
		}
	case reflect.Slice:
		scope.Debug("Slice")
		for i := 0; i < vv.Len(); i++ {
			errs = util.AppendErrs(errs, t.protoToHelmValues(vv.Index(i).Interface(), root, path))
		}
	default:
		// Must be a leaf
		scope.Debugf("field has kind %s", vt.Kind())
		if vv.CanInterface() {
			errs = util.AppendErrs(errs, t.insertLeaf(root, path, vv))
		}
	}

	return errs
}

// setEnablementAndNamespaces translates the enablement and namespace value of each component in the baseYAML values
// tree, based on feature/component inheritance relationship.
func (t *Translator) setEnablementAndNamespaces(root map[string]interface{}, icp *v1alpha2.IstioControlPlaneSpec) error {
	for cn, c := range t.ComponentMaps {
		e, err := t.IsComponentEnabled(cn, icp)
		if err != nil {
			return err
		}
		if err := tpath.WriteNode(root, util.PathFromString(c.ToHelmValuesTreeRoot+"."+HelmValuesEnabledSubpath), e); err != nil {
			return err
		}

		ns, err := name.Namespace(t.ToFeature[cn], cn, icp)
		if err != nil {
			return err
		}
		if err := tpath.WriteNode(root, util.PathFromString(c.ToHelmValuesTreeRoot+"."+HelmValuesNamespaceSubpath), ns); err != nil {
			return err
		}
	}

	for cn, gns := range t.GlobalNamespaces {
		ns, err := name.Namespace(t.ToFeature[cn], cn, icp)
		if err != nil {
			return err
		}
		if err := tpath.WriteNode(root, util.PathFromString("global."+gns), ns); err != nil {
			return err
		}
	}

	return nil
}

// IsComponentEnabled reports whether the component with name cn is enabled, according to the translations in t,
// and the contents of ocp.
func (t *Translator) IsComponentEnabled(cn name.ComponentName, icp *v1alpha2.IstioControlPlaneSpec) (bool, error) {
	if t.ComponentMaps[cn].AlwaysEnabled {
		return true, nil
	}
	return name.IsComponentEnabledInSpec(t.ToFeature[cn], cn, icp)
}

// AllComponentsNames returns a slice of all components used in t.
func (t *Translator) AllComponentsNames() []name.ComponentName {
	var out []name.ComponentName
	for cn := range t.ComponentMaps {
		out = append(out, cn)
	}
	return out
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
	scope.Debugf("translating %s to %s", path, out)
	return out, m
}

// renderFeatureComponentPathTemplate renders a template of the form <path>{{.FeatureName}}<path>{{.ComponentName}}<path> with
// the supplied parameters.
func renderFeatureComponentPathTemplate(tmpl string, featureName name.FeatureName, componentName name.ComponentName) (string, error) {
	type Temp struct {
		FeatureName   name.FeatureName
		ComponentName name.ComponentName
	}
	ts := Temp{
		FeatureName:   featureName,
		ComponentName: componentName,
	}
	return renderTemplate(tmpl, ts)
}

// renderResourceComponentPathTemplate renders a template of the form <path>{{.ResourceName}}<path>{{.ContainerName}}<path> with
// the supplied parameters.
func (t *Translator) renderResourceComponentPathTemplate(tmpl string, componentName name.ComponentName) (string, error) {
	ts := struct {
		ResourceName  string
		ContainerName string
	}{
		ResourceName:  t.ComponentMaps[componentName].ResourceName,
		ContainerName: t.ComponentMaps[componentName].ContainerName,
	}
	return renderTemplate(tmpl, ts)
}

// helper method to render template
func renderTemplate(tmpl string, ts interface{}) (string, error) {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return "", err
	}
	buf := new(bytes.Buffer)
	err = t.Execute(buf, ts)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// defaultTranslationFunc is the default translation to values. It maps a Go data path into a YAML path.
func defaultTranslationFunc(m *Translation, root map[string]interface{}, valuesPath string, value interface{}) error {
	var path []string

	if util.IsEmptyString(value) {
		scope.Debugf("Skip empty string value for path %s", m.outPath)
		return nil
	}
	if valuesPath == "" {
		scope.Debugf("Not mapping to values, resources path is %s", m.outPath)
		return nil
	}

	for _, p := range util.PathFromString(valuesPath) {
		path = append(path, firstCharToLower(p))
	}

	return tpath.WriteNode(root, path, value)
}

func firstCharToLower(s string) string {
	return strings.ToLower(s[0:1]) + s[1:]
}
