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
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/proto"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes/scheme"

	"istio.io/api/operator/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/version"
	"istio.io/istio/operator/pkg/vfs"
	"istio.io/pkg/log"
)

const (
	// HelmValuesEnabledSubpath is the subpath from the component root to the enabled parameter.
	HelmValuesEnabledSubpath = "enabled"
	// HelmValuesNamespaceSubpath is the subpath from the component root to the namespace parameter.
	HelmValuesNamespaceSubpath = "namespace"
	// HelmValuesHubSubpath is the subpath from the component root to the hub parameter.
	HelmValuesHubSubpath = "hub"
	// HelmValuesTagSubpath is the subpath from the component root to the tag parameter.
	HelmValuesTagSubpath = "tag"
	// TranslateConfigFolder is the folder where we store translation configurations
	TranslateConfigFolder = "translateConfig"
	// TranslateConfigPrefix is the prefix of IstioOperator's translation configuration file
	TranslateConfigPrefix = "translateConfig-"

	// devDbg generates lots of output useful in development.
	devDbg = false
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
	APIMapping map[string]*Translation `yaml:"apiMapping"`
	// KubernetesMapping defines mappings from an IstioOperator API paths to k8s resource paths.
	KubernetesMapping map[string]*Translation `yaml:"kubernetesMapping"`
	// GlobalNamespaces maps feature namespaces to Helm global namespace definitions.
	GlobalNamespaces map[name.ComponentName]string `yaml:"globalNamespaces"`
	// ComponentMaps is a set of mappings for each Istio component.
	ComponentMaps map[name.ComponentName]*ComponentMaps `yaml:"componentMaps"`
}

// FeatureMap is a set of mappings for an Istio feature.
type FeatureMap struct {
	// Components contains list of components that belongs to the current feature.
	Components []name.ComponentName
}

// ComponentMaps is a set of mappings for an Istio component.
type ComponentMaps struct {
	// ResourceType maps a ComponentName to the type of the rendered k8s resource.
	ResourceType string
	// ResourceName maps a ComponentName to the name of the rendered k8s resource.
	ResourceName string
	// ContainerName maps a ComponentName to the name of the container in a Deployment.
	ContainerName string
	// HelmSubdir is a mapping between a component name and the subdirectory of the component Chart.
	HelmSubdir string
	// ToHelmValuesTreeRoot is the tree root in values YAML files for the component.
	ToHelmValuesTreeRoot string
	// SkipReverseTranslate defines whether reverse translate of this component need to be skipped.
	SkipReverseTranslate bool
}

// TranslationFunc maps a yamlStr API path into a YAML values tree.
type TranslationFunc func(t *Translation, root map[string]interface{}, valuesPath string, value interface{}) error

// Translation is a mapping to an output path using a translation function.
type Translation struct {
	// OutPath defines the position in the yaml file
	OutPath         string          `yaml:"outPath"`
	translationFunc TranslationFunc `yaml:"TranslationFunc,omitempty"`
}

// NewTranslator creates a new translator for minorVersion and returns a ptr to it.
func NewTranslator(minorVersion version.MinorVersion) (*Translator, error) {
	f := filepath.Join(TranslateConfigFolder, TranslateConfigPrefix+minorVersion.String()+".yaml")
	b, err := vfs.ReadFile(f)
	if err != nil {
		return nil, fmt.Errorf("could not read translateConfig file %s: %s", f, err)
	}
	t := &Translator{}
	err = yaml.Unmarshal(b, t)
	if err != nil {
		return nil, fmt.Errorf("could not Unmarshal translateConfig file %s: %s", f, err)
	}
	t.Version = minorVersion
	return t, nil
}

// OverlayK8sSettings overlays k8s settings from iop over the manifest objects, based on t's translation mappings.
func (t *Translator) OverlayK8sSettings(yml string, iop *v1alpha1.IstioOperatorSpec, componentName name.ComponentName,
	resourceName string, addonName string, index int) (string, error) {
	objects, err := object.ParseK8sObjectsFromYAMLManifest(yml)
	if err != nil {
		return "", err
	}
	scope.Debugf("Manifest contains the following objects:")
	for _, o := range objects {
		scope.Debugf("%s", o.HashNameKind())
	}
	// om is a map of kind:name string to Object ptr.
	om := objects.ToNameKindMap()
	for inPath, v := range t.KubernetesMapping {
		inPath, err := renderFeatureComponentPathTemplate(inPath, componentName)
		if err != nil {
			return "", err
		}
		inPath = strings.Replace(inPath, "gressGateways.", "gressGateways."+fmt.Sprint(index)+".", 1)
		scope.Debugf("Checking for path %s in IstioOperatorSpec", inPath)

		m, found, err := getK8SSpecFromIOP(iop, componentName, inPath, addonName)
		if err != nil {
			return "", err
		}
		if !found {
			scope.Debugf("path %s not found in IstioOperatorSpec, skip mapping.", inPath)
			continue
		}
		if mstr, ok := m.(string); ok && mstr == "" {
			scope.Debugf("path %s is empty string, skip mapping.", inPath)
			continue
		}
		// Zero int values are due to proto3 compiling to scalars rather than ptrs. Skip these because values of 0 are
		// the default in destination fields and need not be set explicitly.
		if mint, ok := util.ToIntValue(m); ok && mint == 0 {
			scope.Debugf("path %s is int 0, skip mapping.", inPath)
			continue
		}
		if componentName == name.IstioBaseComponentName {
			return "", fmt.Errorf("base component can only have k8s.overlays, not other K8s settings")
		}
		outPath, err := t.renderResourceComponentPathTemplate(v.OutPath, componentName, resourceName, addonName, iop.Revision)
		if err != nil {
			return "", err
		}
		scope.Debugf("path has value in IstioOperatorSpec, mapping to output path %s", outPath)
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
			// skip to overlay the K8s settings if the corresponding resource doesn't exist.
			scope.Infof("resource Kind:name %s doesn't exist in the output manifest, skip overlay.", pe)
			continue
		}

		// strategic merge overlay m to the base object oo
		mergedObj, err := MergeK8sObject(oo, m, path[1:])
		if err != nil {
			return "", err
		}
		// Update the original object in objects slice, since the output should be ordered.
		*(om[pe]) = *mergedObj
	}

	return objects.YAMLManifest()
}

// ProtoToValues traverses the supplied IstioOperatorSpec and returns a values.yaml translation from it.
func (t *Translator) ProtoToValues(ii *v1alpha1.IstioOperatorSpec) (string, error) {
	root := make(map[string]interface{})

	errs := t.ProtoToHelmValues(ii, root, nil)
	if len(errs) != 0 {
		return "", errs.ToError()
	}

	// Special additional handling not covered by simple translation rules.
	if err := t.setComponentProperties(root, ii); err != nil {
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

// TranslateHelmValues creates a Helm values.yaml config data tree from iop using the given translator.
func (t *Translator) TranslateHelmValues(iop *v1alpha1.IstioOperatorSpec, componentsSpec interface{}, componentName name.ComponentName) (string, error) {
	globalVals, globalUnvalidatedVals, apiVals := make(map[string]interface{}), make(map[string]interface{}), make(map[string]interface{})

	// First, translate the IstioOperator API to helm Values.
	apiValsStr, err := t.ProtoToValues(iop)
	if err != nil {
		return "", err
	}
	err = yaml.Unmarshal([]byte(apiValsStr), &apiVals)
	if err != nil {
		return "", err
	}

	if devDbg {
		scope.Infof("Values translated from IstioOperator API:\n%s", apiValsStr)
	}

	// Add global overlay from IstioOperatorSpec.Values/UnvalidatedValues.
	_, err = tpath.SetFromPath(iop, "Values", &globalVals)
	if err != nil {
		return "", err
	}
	_, err = tpath.SetFromPath(iop, "UnvalidatedValues", &globalUnvalidatedVals)
	if err != nil {
		return "", err
	}
	if devDbg {
		scope.Infof("Values from IstioOperatorSpec.Values:\n%s", util.ToYAML(globalVals))
		scope.Infof("Values from IstioOperatorSpec.UnvalidatedValues:\n%s", util.ToYAML(globalUnvalidatedVals))
	}
	mergedVals, err := util.OverlayTrees(apiVals, globalVals)
	if err != nil {
		return "", err
	}
	mergedVals, err = util.OverlayTrees(mergedVals, globalUnvalidatedVals)
	if err != nil {
		return "", err
	}

	mergedYAML, err := yaml.Marshal(mergedVals)
	if err != nil {
		return "", err
	}

	mergedYAML, err = applyGatewayTranslations(mergedYAML, componentName, componentsSpec)
	if err != nil {
		return "", err
	}

	return string(mergedYAML), err
}

// applyGatewayTranslations writes gateway name gwName at the appropriate values path in iop and maps k8s.service.ports
// to values. It returns the resulting YAML tree.
func applyGatewayTranslations(iop []byte, componentName name.ComponentName, componentSpec interface{}) ([]byte, error) {
	if !componentName.IsGateway() {
		return iop, nil
	}
	iopt := make(map[string]interface{})
	if err := yaml.Unmarshal(iop, &iopt); err != nil {
		return nil, err
	}
	gwSpec := componentSpec.(*v1alpha1.GatewaySpec)
	k8s := gwSpec.K8S
	switch componentName {
	case name.IngressComponentName:
		setYAMLNodeByMapPath(iopt, util.PathFromString("gateways.istio-ingressgateway.name"), gwSpec.Name)
		if len(gwSpec.Label) != 0 {
			setYAMLNodeByMapPath(iopt, util.PathFromString("gateways.istio-ingressgateway.labels"), gwSpec.Label)
		}
		if k8s != nil && k8s.Service != nil {
			setYAMLNodeByMapPath(iopt, util.PathFromString("gateways.istio-ingressgateway.ports"), k8s.Service.Ports)
		}
	case name.EgressComponentName:
		setYAMLNodeByMapPath(iopt, util.PathFromString("gateways.istio-egressgateway.name"), gwSpec.Name)
		if len(gwSpec.Label) != 0 {
			setYAMLNodeByMapPath(iopt, util.PathFromString("gateways.istio-egressgateway.labels"), gwSpec.Label)
		}
		if k8s != nil && k8s.Service != nil {
			setYAMLNodeByMapPath(iopt, util.PathFromString("gateways.istio-egressgateway.ports"), k8s.Service.Ports)
		}
	}
	return yaml.Marshal(iopt)
}

// setYAMLNodeByMapPath sets the value at the given path to val in treeNode. The path cannot traverse lists and
// treeNode must be a YAML tree unmarshaled into a plain map data structure.
func setYAMLNodeByMapPath(treeNode interface{}, path util.Path, val interface{}) {
	if len(path) == 0 || treeNode == nil {
		return
	}
	pe := path[0]
	switch nt := treeNode.(type) {
	case map[interface{}]interface{}:
		if len(path) == 1 {
			nt[pe] = val
			return
		}
		if nt[pe] == nil {
			return
		}
		setYAMLNodeByMapPath(nt[pe], path[1:], val)
	case map[string]interface{}:
		if len(path) == 1 {
			nt[pe] = val
			return
		}
		if nt[pe] == nil {
			return
		}
		setYAMLNodeByMapPath(nt[pe], path[1:], val)
	}
}

// ComponentMap returns a ComponentMaps struct ptr for the given component name if one exists.
// If the name of the component is lower case, the function will use the capitalized version
// of the name.
func (t *Translator) ComponentMap(cns string) *ComponentMaps {
	cn := name.TitleCase(name.ComponentName(cns))
	return t.ComponentMaps[cn]
}

// ProtoToHelmValues function below is used by third party for integrations and has to be public

// ProtoToHelmValues takes an interface which must be a struct ptr and recursively iterates through all its fields.
// For each leaf, if looks for a mapping from the struct data path to the corresponding YAML path and if one is
// found, it calls the associated mapping function if one is defined to populate the values YAML path.
// If no mapping function is defined, it uses the default mapping function.
func (t *Translator) ProtoToHelmValues(node interface{}, root map[string]interface{}, path util.Path) (errs util.Errors) {
	scope.Debugf("ProtoToHelmValues with path %s, %v (%T)", path, node, node)
	if util.IsValueNil(node) {
		return nil
	}

	vv := reflect.ValueOf(node)
	vt := reflect.TypeOf(node)
	switch vt.Kind() {
	case reflect.Ptr:
		if !util.IsNilOrInvalidValue(vv.Elem()) {
			errs = util.AppendErrs(errs, t.ProtoToHelmValues(vv.Elem().Interface(), root, path))
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
			errs = util.AppendErrs(errs, t.ProtoToHelmValues(fieldValue.Interface(), root, append(path, fieldName)))
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
			errs = util.AppendErrs(errs, t.ProtoToHelmValues(vv.Index(i).Interface(), root, path))
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

// setComponentProperties translates properties (e.g., enablement and namespace) of each component
// in the baseYAML values tree, based on feature/component inheritance relationship.
func (t *Translator) setComponentProperties(root map[string]interface{}, iop *v1alpha1.IstioOperatorSpec) error {
	var keys []string
	for k := range t.ComponentMaps {
		if k != name.IngressComponentName && k != name.EgressComponentName {
			keys = append(keys, string(k))
		}
	}
	sort.Strings(keys)
	l := len(keys)
	for i := l - 1; i >= 0; i-- {
		cn := name.ComponentName(keys[i])
		c := t.ComponentMaps[cn]
		e, err := t.IsComponentEnabled(cn, iop)
		if err != nil {
			return err
		}

		enablementPath := c.ToHelmValuesTreeRoot
		// CNI calls itself "cni" in the chart but "istio_cni" for enablement outside of the chart.
		if cn == name.CNIComponentName {
			enablementPath = "istio_cni"
		}
		if err := tpath.WriteNode(root, util.PathFromString(enablementPath+"."+HelmValuesEnabledSubpath), e); err != nil {
			return err
		}

		ns, err := name.Namespace(cn, iop)
		if err != nil {
			return err
		}
		if err := tpath.WriteNode(root, util.PathFromString(c.ToHelmValuesTreeRoot+"."+HelmValuesNamespaceSubpath), ns); err != nil {
			return err
		}

		hub, found, _ := tpath.GetFromStructPath(iop, "Components."+string(cn)+".Hub")
		// Unmarshal unfortunately creates struct fields with "" for unset values. Skip these cases to avoid
		// overwriting current value with an empty string.
		hubStr, ok := hub.(string)
		if found && !(ok && hubStr == "") {
			if err := tpath.WriteNode(root, util.PathFromString(c.ToHelmValuesTreeRoot+"."+HelmValuesHubSubpath), hub); err != nil {
				return err
			}
		}

		tag, found, _ := tpath.GetFromStructPath(iop, "Components."+string(cn)+".Tag")
		tagStr, ok := tag.(string)
		if found && !(ok && tagStr == "") {
			if err := tpath.WriteNode(root, util.PathFromString(c.ToHelmValuesTreeRoot+"."+HelmValuesTagSubpath), tag); err != nil {
				return err
			}
		}
	}

	for cn, gns := range t.GlobalNamespaces {
		ns, err := name.Namespace(cn, iop)
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
func (t *Translator) IsComponentEnabled(cn name.ComponentName, iop *v1alpha1.IstioOperatorSpec) (bool, error) {
	if t.ComponentMaps[cn] == nil {
		return false, nil
	}
	return IsComponentEnabledInSpec(cn, iop)
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

	if m.OutPath == "" {
		return "", m
	}

	out := m.OutPath + "." + path[len(p):].String()
	scope.Debugf("translating %s to %s", path, out)
	return out, m
}

// renderFeatureComponentPathTemplate renders a template of the form <path>{{.ComponentName}}<path> with
// the supplied parameters.
func renderFeatureComponentPathTemplate(tmpl string, componentName name.ComponentName) (string, error) {
	type Temp struct {
		ComponentName name.ComponentName
	}
	ts := Temp{
		ComponentName: componentName,
	}
	return util.RenderTemplate(tmpl, ts)
}

// renderResourceComponentPathTemplate renders a template of the form <path>{{.ResourceName}}<path>{{.ContainerName}}<path> with
// the supplied parameters.
func (t *Translator) renderResourceComponentPathTemplate(tmpl string, componentName name.ComponentName,
	resourceName, addonName, revision string) (string, error) {
	cn := string(componentName)
	if componentName == name.AddonComponentName {
		cn = addonName
	}
	cmp := t.ComponentMap(cn)
	if cmp == nil {
		return "", fmt.Errorf("component: %s does not exist in the componentMap", addonName)
	}
	if resourceName == "" {
		resourceName = cmp.ResourceName
	}
	// The istiod resource will be istiod-<REVISION>, so we need to append the revision suffix
	if revision != "" && resourceName == "istiod" {
		resourceName += "-" + revision
	}
	ts := struct {
		ResourceType  string
		ResourceName  string
		ContainerName string
	}{
		ResourceType:  cmp.ResourceType,
		ResourceName:  resourceName,
		ContainerName: cmp.ContainerName,
	}
	return util.RenderTemplate(tmpl, ts)
}

// getK8SSpecFromIOP is helper function to get k8s spec node from iop using inPath
// 1. if component is not an addonComponent, get the node directly from iop using the inPath.
// 2. otherwise convert the inPath and point the root to the entry of addonComponents map with addonName as key.
// e.x: original inPath: "Components.AddonComponents.K8S.ReplicaCount" and root: iop would be converted to
// new inPath: "K8S.ReplicaCount" and root: "iop.AddonComponents.addonName"
func getK8SSpecFromIOP(iop *v1alpha1.IstioOperatorSpec, componentName name.ComponentName, inPath string,
	addonName string) (m interface{}, found bool, err error) {
	if componentName != name.AddonComponentName {
		return tpath.GetFromStructPath(iop, inPath)
	}
	inPath = strings.Replace(inPath, "Components.AddonComponents.", "", 1)
	scope.Debugf("Checking for path %s in K8S Spec of AddonComponents: %s", inPath, addonName)
	addonMaps := iop.AddonComponents
	if extSpec, ok := addonMaps[addonName]; ok {
		m, found, err = tpath.GetFromStructPath(extSpec, inPath)
		if err != nil {
			return nil, false, err
		}
	} else {
		scope.Debugf("path %s not found in AddonComponents.%s, skip mapping.", inPath, addonName)
		return nil, false, nil
	}
	return m, found, nil
}

// defaultTranslationFunc is the default translation to values. It maps a Go data path into a YAML path.
func defaultTranslationFunc(m *Translation, root map[string]interface{}, valuesPath string, value interface{}) error {
	var path []string

	if util.IsEmptyString(value) {
		scope.Debugf("Skip empty string value for path %s", m.OutPath)
		return nil
	}
	if valuesPath == "" {
		scope.Debugf("Not mapping to values, resources path is %s", m.OutPath)
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

// MergeK8sObject function below is used by third party for integrations and has to be public

// MergeK8sObject does strategic merge for overlayNode on the base object.
func MergeK8sObject(base *object.K8sObject, overlayNode interface{}, path util.Path) (*object.K8sObject, error) {
	overlay, err := createPatchObjectFromPath(overlayNode, path)
	if err != nil {
		return nil, err
	}
	overlayYAML, err := yaml.Marshal(overlay)
	if err != nil {
		return nil, err
	}
	overlayJSON, err := yaml.YAMLToJSON(overlayYAML)
	if err != nil {
		return nil, fmt.Errorf("yamlToJSON error in overlayYAML: %s\n%s", err, overlayYAML)
	}
	baseJSON, err := base.JSON()
	if err != nil {
		return nil, err
	}

	// get a versioned object from the scheme, we can use the strategic patching mechanism
	// (i.e. take advantage of patchStrategy in the type)
	versionedObject, err := scheme.Scheme.New(base.GroupVersionKind())
	if err != nil {
		return nil, err
	}
	// strategic merge patch
	newBytes, err := strategicpatch.StrategicMergePatch(baseJSON, overlayJSON, versionedObject)
	if err != nil {
		return nil, fmt.Errorf("get error: %s to merge patch:\n%s for base:\n%s", err, overlayJSON, baseJSON)
	}

	newObj, err := object.ParseJSONToK8sObject(newBytes)
	if err != nil {
		return nil, err
	}

	return newObj, nil
}

// createPatchObjectFromPath constructs patch object for node with path, returns nil object and error if the path is invalid.
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
func createPatchObjectFromPath(node interface{}, path util.Path) (map[string]interface{}, error) {
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

// IOPStoIOP takes an IstioOperatorSpec and returns a corresponding IstioOperator with the given name and namespace.
func IOPStoIOP(iops proto.Message, name, namespace string) (*iopv1alpha1.IstioOperator, error) {
	iopStr, err := IOPStoIOPstr(iops, name, namespace)
	if err != nil {
		return nil, err
	}
	iop := &iopv1alpha1.IstioOperator{}
	if err := util.UnmarshalWithJSONPB(iopStr, iop, false); err != nil {
		return nil, err
	}
	return iop, nil
}

// IOPStoIOPstr takes an IstioOperatorSpec and returns a corresponding IstioOperator string with the given name and namespace.
func IOPStoIOPstr(iops proto.Message, name, namespace string) (string, error) {
	iopsStr, err := util.MarshalWithJSONPB(iops)
	if err != nil {
		return "", err
	}
	spec, err := tpath.AddSpecRoot(iopsStr)
	if err != nil {
		return "", err
	}

	tmpl := `
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  namespace: {{ .Namespace }}
  name: {{ .Name }} 
`
	// Passing into template causes reformatting, use simple concatenation instead.
	tmpl += spec

	type Temp struct {
		Namespace string
		Name      string
	}
	ts := Temp{
		Namespace: namespace,
		Name:      name,
	}
	return util.RenderTemplate(tmpl, ts)
}
