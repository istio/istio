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
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/metrics"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/version"
	oversion "istio.io/istio/operator/version"
)

// ReverseTranslator is a set of mappings to translate between values.yaml and API paths, charts, k8s paths.
type ReverseTranslator struct {
	Version version.MinorVersion
	// APIMapping is Values.yaml path to API path mapping using longest prefix match. If the path is a non-leaf node,
	// the output path is the matching portion of the path, plus any remaining output path.
	APIMapping map[string]*Translation `yaml:"apiMapping,omitempty"`
	// KubernetesPatternMapping defines mapping patterns from k8s resource paths to IstioOperator API paths.
	KubernetesPatternMapping map[string]string `yaml:"kubernetesPatternMapping,omitempty"`
	// KubernetesMapping defines actual k8s mappings generated from KubernetesPatternMapping before each translation.
	KubernetesMapping map[string]*Translation `yaml:"kubernetesMapping,omitempty"`
	// GatewayKubernetesMapping defines actual k8s mappings for gateway components generated from KubernetesPatternMapping before each translation.
	GatewayKubernetesMapping gatewayKubernetesMapping `yaml:"GatewayKubernetesMapping,omitempty"`
	// ValuesToComponentName defines mapping from value path to component name in API paths.
	ValuesToComponentName map[string]name.ComponentName `yaml:"valuesToComponentName,omitempty"`
}

type gatewayKubernetesMapping struct {
	IngressMapping map[string]*Translation
	EgressMapping  map[string]*Translation
}

var (
	// Component enablement mapping. Ex "{{.ValueComponent}}.enabled": Components.{{.ComponentName}}.enabled}", nil},
	componentEnablementPattern = "Components.{{.ComponentName}}.Enabled"
	// specialComponentPath lists cases of component path of values.yaml we need to have special treatment.
	specialComponentPath = map[string]bool{
		"gateways":                      true,
		"gateways.istio-ingressgateway": true,
		"gateways.istio-egressgateway":  true,
	}

	skipTranslate = map[name.ComponentName]bool{
		name.IstioBaseComponentName:          true,
		name.IstioOperatorComponentName:      true,
		name.IstioOperatorCustomResourceName: true,
		name.CNIComponentName:                true,
		name.IstiodRemoteComponentName:       true,
	}

	gatewayPathMapping = map[string]name.ComponentName{
		"gateways.istio-ingressgateway": name.IngressComponentName,
		"gateways.istio-egressgateway":  name.EgressComponentName,
	}
)

// initAPIMapping generate the reverse mapping from original translator apiMapping.
func (t *ReverseTranslator) initAPIAndComponentMapping() {
	ts := NewTranslator()
	t.APIMapping = make(map[string]*Translation)
	t.KubernetesMapping = make(map[string]*Translation)
	t.ValuesToComponentName = make(map[string]name.ComponentName)
	for valKey, outVal := range ts.APIMapping {
		t.APIMapping[outVal.OutPath] = &Translation{valKey, nil}
	}
	for cn, cm := range ts.ComponentMaps {
		// we use dedicated translateGateway for gateway instead
		if !skipTranslate[cn] && !cm.SkipReverseTranslate && !cn.IsGateway() {
			t.ValuesToComponentName[cm.ToHelmValuesTreeRoot] = cn
		}
	}
}

// initK8SMapping generates the k8s settings mapping for components that are enabled based on templates.
func (t *ReverseTranslator) initK8SMapping() error {
	outputMapping := make(map[string]*Translation)
	for valKey, componentName := range t.ValuesToComponentName {
		for K8SValKey, outPathTmpl := range t.KubernetesPatternMapping {
			newKey, err := renderComponentName(K8SValKey, valKey)
			if err != nil {
				return err
			}
			newVal, err := renderFeatureComponentPathTemplate(outPathTmpl, componentName)
			if err != nil {
				return err
			}
			outputMapping[newKey] = &Translation{newVal, nil}
		}
	}

	t.KubernetesMapping = outputMapping

	igwOutputMapping := make(map[string]*Translation)
	egwOutputMapping := make(map[string]*Translation)
	for valKey, componentName := range gatewayPathMapping {
		mapping := igwOutputMapping
		if componentName == name.EgressComponentName {
			mapping = egwOutputMapping
		}
		for K8SValKey, outPathTmpl := range t.KubernetesPatternMapping {
			newKey, err := renderComponentName(K8SValKey, valKey)
			if err != nil {
				return err
			}
			newP := util.PathFromString(outPathTmpl)
			mapping[newKey] = &Translation{newP[len(newP)-2:].String(), nil}
		}
	}
	t.GatewayKubernetesMapping = gatewayKubernetesMapping{IngressMapping: igwOutputMapping, EgressMapping: egwOutputMapping}
	return nil
}

// NewReverseTranslator creates a new ReverseTranslator for minorVersion and returns a ptr to it.
func NewReverseTranslator() *ReverseTranslator {
	rt := &ReverseTranslator{
		KubernetesPatternMapping: map[string]string{
			"{{.ValueComponentName}}.env":                   "Components.{{.ComponentName}}.K8s.Env",
			"{{.ValueComponentName}}.autoscaleEnabled":      "Components.{{.ComponentName}}.K8s.HpaSpec",
			"{{.ValueComponentName}}.imagePullPolicy":       "Components.{{.ComponentName}}.K8s.ImagePullPolicy",
			"{{.ValueComponentName}}.nodeSelector":          "Components.{{.ComponentName}}.K8s.NodeSelector",
			"{{.ValueComponentName}}.tolerations":           "Components.{{.ComponentName}}.K8s.Tolerations",
			"{{.ValueComponentName}}.podDisruptionBudget":   "Components.{{.ComponentName}}.K8s.PodDisruptionBudget",
			"{{.ValueComponentName}}.podAnnotations":        "Components.{{.ComponentName}}.K8s.PodAnnotations",
			"{{.ValueComponentName}}.priorityClassName":     "Components.{{.ComponentName}}.K8s.PriorityClassName",
			"{{.ValueComponentName}}.readinessProbe":        "Components.{{.ComponentName}}.K8s.ReadinessProbe",
			"{{.ValueComponentName}}.replicaCount":          "Components.{{.ComponentName}}.K8s.ReplicaCount",
			"{{.ValueComponentName}}.resources":             "Components.{{.ComponentName}}.K8s.Resources",
			"{{.ValueComponentName}}.rollingMaxSurge":       "Components.{{.ComponentName}}.K8s.Strategy",
			"{{.ValueComponentName}}.rollingMaxUnavailable": "Components.{{.ComponentName}}.K8s.Strategy",
			"{{.ValueComponentName}}.serviceAnnotations":    "Components.{{.ComponentName}}.K8s.ServiceAnnotations",
		},
	}
	rt.initAPIAndComponentMapping()
	rt.Version = oversion.OperatorBinaryVersion.MinorVersion
	return rt
}

// TranslateFromValueToSpec translates from values.yaml value to IstioOperatorSpec.
func (t *ReverseTranslator) TranslateFromValueToSpec(values []byte, force bool) (controlPlaneSpec *v1alpha1.IstioOperatorSpec, err error) {
	yamlTree := make(map[string]any)
	err = yaml.Unmarshal(values, &yamlTree)
	if err != nil {
		return nil, fmt.Errorf("error when unmarshalling into untype tree %v", err)
	}

	outputTree := make(map[string]any)
	err = t.TranslateTree(yamlTree, outputTree, nil)
	if err != nil {
		return nil, err
	}
	outputVal, err := yaml.Marshal(outputTree)
	if err != nil {
		return nil, err
	}

	cpSpec := &v1alpha1.IstioOperatorSpec{}
	err = util.UnmarshalWithJSONPB(string(outputVal), cpSpec, force)

	if err != nil {
		return nil, fmt.Errorf("error when unmarshalling into control plane spec %v, \nyaml:\n %s", err, outputVal)
	}

	return cpSpec, nil
}

// TranslateTree translates input value.yaml Tree to ControlPlaneSpec Tree.
func (t *ReverseTranslator) TranslateTree(valueTree map[string]any, cpSpecTree map[string]any, path util.Path) error {
	// translate enablement and namespace
	err := t.setEnablementFromValue(valueTree, cpSpecTree)
	if err != nil {
		return fmt.Errorf("error when translating enablement and namespace from value.yaml tree: %v", err)
	}
	// translate with api mapping
	err = t.translateAPI(valueTree, cpSpecTree)
	if err != nil {
		return fmt.Errorf("error when translating value.yaml tree with global mapping: %v", err)
	}

	// translate with k8s mapping
	if err := t.TranslateK8S(valueTree, cpSpecTree); err != nil {
		return err
	}

	if err := t.translateGateway(valueTree, cpSpecTree); err != nil {
		return fmt.Errorf("error when translating gateway with kubernetes mapping: %v", err.Error())
	}
	// translate remaining untranslated paths into component values
	err = t.translateRemainingPaths(valueTree, cpSpecTree, nil)
	if err != nil {
		return fmt.Errorf("error when translating remaining path: %v", err)
	}
	return nil
}

// TranslateK8S is a helper function to translate k8s settings from values.yaml to IstioOperator, except for gateways.
func (t *ReverseTranslator) TranslateK8S(valueTree map[string]any, cpSpecTree map[string]any) error {
	// translate with k8s mapping
	if err := t.initK8SMapping(); err != nil {
		return fmt.Errorf("error when initiating k8s mapping: %v", err)
	}
	if err := t.translateK8sTree(valueTree, cpSpecTree, t.KubernetesMapping); err != nil {
		return fmt.Errorf("error when translating value.yaml tree with kubernetes mapping: %v", err)
	}
	return nil
}

// setEnablementFromValue translates the enablement value of components in the values.yaml
// tree, based on feature/component inheritance relationship.
func (t *ReverseTranslator) setEnablementFromValue(valueSpec map[string]any, root map[string]any) error {
	for _, cni := range t.ValuesToComponentName {
		enabled, pathExist, err := IsComponentEnabledFromValue(cni, valueSpec)
		if err != nil {
			return err
		}
		if !pathExist {
			continue
		}
		tmpl := componentEnablementPattern
		ceVal, err := renderFeatureComponentPathTemplate(tmpl, cni)
		if err != nil {
			return err
		}
		outCP := util.ToYAMLPath(ceVal)
		// set component enablement
		if err := tpath.WriteNode(root, outCP, enabled); err != nil {
			return err
		}
	}

	return nil
}

// WarningForGatewayK8SSettings creates deprecated warning messages
// when user try to set kubernetes settings for gateways via values api.
func (t *ReverseTranslator) WarningForGatewayK8SSettings(valuesOverlay string) (string, error) {
	gwOverlay, err := tpath.GetConfigSubtree(valuesOverlay, "gateways")
	if err != nil {
		return "", fmt.Errorf("error getting gateways overlay from valuesOverlayYaml %v", err)
	}
	if gwOverlay == "" {
		return "", nil
	}
	var deprecatedFields []string
	for inPath := range t.GatewayKubernetesMapping.IngressMapping {
		_, found, err := tpath.GetPathContext(valuesOverlay, util.ToYAMLPath(inPath), false)
		if err != nil {
			scope.Debug(err.Error())
			continue
		}
		if found {
			deprecatedFields = append(deprecatedFields, inPath)
		}
	}
	for inPath := range t.GatewayKubernetesMapping.EgressMapping {
		_, found, err := tpath.GetPathContext(valuesOverlay, util.ToYAMLPath(inPath), false)
		if err != nil {
			scope.Debug(err.Error())
			continue
		}
		if found {
			deprecatedFields = append(deprecatedFields, inPath)
		}
	}
	if len(deprecatedFields) == 0 {
		return "", nil
	}
	warningMessage := fmt.Sprintf("using deprecated values api paths: %s.\n"+
		" please use k8s spec of gateway components instead\n", strings.Join(deprecatedFields, ","))
	return warningMessage, nil
}

// translateGateway handles translation for gateways specific configuration
func (t *ReverseTranslator) translateGateway(valueSpec map[string]any, root map[string]any) error {
	for inPath, outPath := range gatewayPathMapping {
		enabled, pathExist, err := IsComponentEnabledFromValue(outPath, valueSpec)
		if err != nil {
			return err
		}
		if !pathExist && !enabled {
			continue
		}
		gwSpecs := make([]map[string]any, 1)
		gwSpec := make(map[string]any)
		gwSpecs[0] = gwSpec
		gwSpec["enabled"] = enabled
		gwSpec["name"] = util.ToYAMLPath(inPath)[1]
		outCP := util.ToYAMLPath("Components." + string(outPath))

		if enabled {
			mapping := t.GatewayKubernetesMapping.IngressMapping
			if outPath == name.EgressComponentName {
				mapping = t.GatewayKubernetesMapping.EgressMapping
			}
			err = t.translateK8sTree(valueSpec, gwSpec, mapping)
			if err != nil {
				return err
			}
		}
		err = tpath.WriteNode(root, outCP, gwSpecs)
		if err != nil {
			return err
		}
	}
	return nil
}

// TranslateK8SfromValueToIOP use reverse translation to convert k8s settings defined in values API to IOP API.
// this ensures that user overlays that set k8s through spec.values
// are not overridden by spec.components.X.k8s settings in the base profiles
func (t *ReverseTranslator) TranslateK8SfromValueToIOP(userOverlayYaml string) (string, error) {
	valuesOverlay, err := tpath.GetConfigSubtree(userOverlayYaml, "spec.values")
	if err != nil {
		scope.Debugf("no spec.values section from userOverlayYaml %v", err)
		return "", nil
	}
	valuesOverlayTree := make(map[string]any)
	err = yaml.Unmarshal([]byte(valuesOverlay), &valuesOverlayTree)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling values overlay yaml into untype tree %v", err)
	}
	iopSpecTree := make(map[string]any)
	iopSpecOverlay, err := tpath.GetConfigSubtree(userOverlayYaml, "spec")
	if err != nil {
		return "", fmt.Errorf("error getting iop spec subtree from overlay yaml %v", err)
	}
	err = yaml.Unmarshal([]byte(iopSpecOverlay), &iopSpecTree)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling spec overlay yaml into tree %v", err)
	}
	if err = t.TranslateK8S(valuesOverlayTree, iopSpecTree); err != nil {
		return "", err
	}
	warning, err := t.WarningForGatewayK8SSettings(valuesOverlay)
	if err != nil {
		return "", fmt.Errorf("error handling values gateway k8s settings: %v", err)
	}
	if warning != "" {
		return "", fmt.Errorf(warning)
	}
	iopSpecTreeYAML, err := yaml.Marshal(iopSpecTree)
	if err != nil {
		return "", fmt.Errorf("error marshaling reverse translated tree %v", err)
	}
	iopTreeYAML, err := tpath.AddSpecRoot(string(iopSpecTreeYAML))
	if err != nil {
		return "", fmt.Errorf("error adding spec root: %v", err)
	}
	// overlay the reverse translated iopTreeYAML back to userOverlayYaml
	finalYAML, err := util.OverlayYAML(userOverlayYaml, iopTreeYAML)
	if err != nil {
		return "", fmt.Errorf("failed to overlay the reverse translated iopTreeYAML: %v", err)
	}
	return finalYAML, err
}

// translateStrategy translates Deployment Strategy related configurations from helm values.yaml tree.
func translateStrategy(fieldName string, outPath string, value any, cpSpecTree map[string]any) error {
	fieldMap := map[string]string{
		"rollingMaxSurge":       "maxSurge",
		"rollingMaxUnavailable": "maxUnavailable",
	}
	newFieldName, ok := fieldMap[fieldName]
	if !ok {
		return fmt.Errorf("expected field name found in values.yaml: %s", fieldName)
	}
	outPath += ".rollingUpdate." + newFieldName

	scope.Debugf("path has value in helm Value.yaml tree, mapping to output path %s", outPath)
	if err := tpath.WriteNode(cpSpecTree, util.ToYAMLPath(outPath), value); err != nil {
		return err
	}
	return nil
}

// translateHPASpec translates HPA related configurations from helm values.yaml tree.
// do not translate if autoscaleEnabled is explicitly set to false
func translateHPASpec(inPath string, outPath string, valueTree map[string]any, cpSpecTree map[string]any) error {
	m, found, err := tpath.Find(valueTree, util.ToYAMLPath(inPath))
	if err != nil {
		return err
	}
	if found {
		asEnabled, ok := m.(bool)
		if !ok {
			return fmt.Errorf("expect autoscaleEnabled node type to be bool but got: %T", m)
		}
		if !asEnabled {
			return nil
		}
	}

	newP := util.PathFromString(inPath)
	// last path element is k8s setting name
	newPS := newP[:len(newP)-1].String()
	valMap := map[string]string{
		".autoscaleMin": ".minReplicas",
		".autoscaleMax": ".maxReplicas",
	}
	for key, newVal := range valMap {
		valPath := newPS + key
		asVal, found, err := tpath.Find(valueTree, util.ToYAMLPath(valPath))
		if found && err == nil {
			if err := setOutputAndClean(valPath, outPath+newVal, asVal, valueTree, cpSpecTree, true); err != nil {
				return err
			}
		}
	}
	valPath := newPS + ".cpu.targetAverageUtilization"
	asVal, found, err := tpath.Find(valueTree, util.ToYAMLPath(valPath))
	if found && err == nil {
		rs := make([]any, 1)
		rsVal := `
- type: Resource
  resource:
    name: cpu
    target:
      type: Utilization
      averageUtilization: %f`

		rsString := fmt.Sprintf(rsVal, asVal)
		if err = yaml.Unmarshal([]byte(rsString), &rs); err != nil {
			return err
		}
		if err := setOutputAndClean(valPath, outPath+".metrics", rs, valueTree, cpSpecTree, true); err != nil {
			return err
		}
	}

	// There is no direct source from value.yaml for scaleTargetRef value, we need to construct from component name
	if found {
		revision := ""
		rev, ok := cpSpecTree["revision"]
		if ok {
			revision = rev.(string)
		}
		st := make(map[string]any)
		stVal := `
apiVersion: apps/v1
kind: Deployment
name: %s`

		// need to do special handling for gateways
		if specialComponentPath[newPS] && len(newP) > 2 {
			newPS = newP[1 : len(newP)-1].String()
		}
		// convert from values component name to correct deployment target
		if newPS == "pilot" {
			newPS = "istiod"
			if revision != "" {
				newPS = newPS + "-" + revision
			}
		}
		stString := fmt.Sprintf(stVal, newPS)
		if err := yaml.Unmarshal([]byte(stString), &st); err != nil {
			return err
		}
		if err := setOutputAndClean(valPath, outPath+".scaleTargetRef", st, valueTree, cpSpecTree, false); err != nil {
			return err
		}
	}
	return nil
}

// setOutputAndClean is helper function to set value of iscp tree and clean the original value from value.yaml tree.
func setOutputAndClean(valPath, outPath string, outVal any, valueTree, cpSpecTree map[string]any, clean bool) error {
	scope.Debugf("path has value in helm Value.yaml tree, mapping to output path %s", outPath)

	if err := tpath.WriteNode(cpSpecTree, util.ToYAMLPath(outPath), outVal); err != nil {
		return err
	}
	if !clean {
		return nil
	}
	if _, err := tpath.Delete(valueTree, util.ToYAMLPath(valPath)); err != nil {
		return err
	}
	return nil
}

// translateEnv translates env value from helm values.yaml tree.
func translateEnv(outPath string, value any, cpSpecTree map[string]any) error {
	envMap, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("expect env node type to be map[string]interface{} but got: %T", value)
	}
	if len(envMap) == 0 {
		return nil
	}
	scope.Debugf("path has value in helm Value.yaml tree, mapping to output path %s", outPath)
	nc, found, _ := tpath.GetPathContext(cpSpecTree, util.ToYAMLPath(outPath), false)
	var envValStr []byte
	if nc != nil {
		envValStr, _ = yaml.Marshal(nc.Node)
	}
	if !found || strings.TrimSpace(string(envValStr)) == "{}" {
		scope.Debugf("path doesn't have value in k8s setting with output path %s, override with helm Value.yaml tree", outPath)
		outEnv := make([]map[string]any, len(envMap))
		keys := make([]string, 0, len(envMap))
		for k := range envMap {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			outEnv[i] = make(map[string]any)
			outEnv[i]["name"] = k
			outEnv[i]["value"] = fmt.Sprintf("%v", envMap[k])
		}
		if err := tpath.WriteNode(cpSpecTree, util.ToYAMLPath(outPath), outEnv); err != nil {
			return err
		}
	} else {
		scope.Debugf("path has value in k8s setting with output path %s, merge it with helm Value.yaml tree", outPath)
		keys := make([]string, 0, len(envMap))
		for k := range envMap {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			outEnv := make(map[string]any)
			outEnv["name"] = k
			outEnv["value"] = fmt.Sprintf("%v", envMap[k])
			if err := tpath.MergeNode(cpSpecTree, util.ToYAMLPath(outPath), outEnv); err != nil {
				return err
			}
		}
	}
	return nil
}

// translateK8sTree is internal method for translating K8s configurations from value.yaml tree.
func (t *ReverseTranslator) translateK8sTree(valueTree map[string]any,
	cpSpecTree map[string]any, mapping map[string]*Translation,
) error {
	for inPath, v := range mapping {
		scope.Debugf("Checking for k8s path %s in helm Value.yaml tree", inPath)
		path := util.PathFromString(inPath)
		k8sSettingName := ""
		if len(path) != 0 {
			k8sSettingName = path[len(path)-1]
		}
		if k8sSettingName == "autoscaleEnabled" {
			if err := translateHPASpec(inPath, v.OutPath, valueTree, cpSpecTree); err != nil {
				return fmt.Errorf("error in translating K8s HPA spec: %s", err)
			}
			metrics.LegacyPathTranslationTotal.Increment()
			continue
		}
		m, found, err := tpath.Find(valueTree, util.ToYAMLPath(inPath))
		if err != nil {
			return err
		}
		if !found {
			scope.Debugf("path %s not found in helm Value.yaml tree, skip mapping.", inPath)
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

		switch k8sSettingName {
		case "env":
			err := translateEnv(v.OutPath, m, cpSpecTree)
			if err != nil {
				return fmt.Errorf("error in translating k8s Env: %s", err)
			}

		case "rollingMaxSurge", "rollingMaxUnavailable":
			err := translateStrategy(k8sSettingName, v.OutPath, m, cpSpecTree)
			if err != nil {
				return fmt.Errorf("error in translating k8s Strategy: %s", err)
			}

		default:
			if util.IsValueNilOrDefault(m) {
				continue
			}
			output := util.ToYAMLPath(v.OutPath)
			scope.Debugf("path has value in helm Value.yaml tree, mapping to output path %s", output)

			if err := tpath.WriteNode(cpSpecTree, output, m); err != nil {
				return err
			}
		}
		metrics.LegacyPathTranslationTotal.Increment()

		if _, err := tpath.Delete(valueTree, util.ToYAMLPath(inPath)); err != nil {
			return err
		}
	}
	return nil
}

// translateRemainingPaths translates remaining paths that are not available in existing mappings.
func (t *ReverseTranslator) translateRemainingPaths(valueTree map[string]any,
	cpSpecTree map[string]any, path util.Path,
) error {
	for key, val := range valueTree {
		newPath := append(path, key)
		// value set to nil means no translation needed or being translated already.
		if val == nil {
			continue
		}
		switch node := val.(type) {
		case map[string]any:
			err := t.translateRemainingPaths(node, cpSpecTree, newPath)
			if err != nil {
				return err
			}
		case []any:
			if err := tpath.WriteNode(cpSpecTree, util.ToYAMLPath("Values."+newPath.String()), node); err != nil {
				return err
			}
		// remaining leaf need to be put into root.values
		default:
			if t.isEnablementPath(newPath) {
				continue
			}
			if err := tpath.WriteNode(cpSpecTree, util.ToYAMLPath("Values."+newPath.String()), val); err != nil {
				return err
			}
		}
	}
	return nil
}

// translateAPI is internal method for translating value.yaml tree based on API mapping.
func (t *ReverseTranslator) translateAPI(valueTree map[string]any,
	cpSpecTree map[string]any,
) error {
	for inPath, v := range t.APIMapping {
		scope.Debugf("Checking for path %s in helm Value.yaml tree", inPath)
		m, found, err := tpath.Find(valueTree, util.ToYAMLPath(inPath))
		if err != nil {
			return err
		}
		if !found {
			scope.Debugf("path %s not found in helm Value.yaml tree, skip mapping.", inPath)
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

		path := util.ToYAMLPath(v.OutPath)
		scope.Debugf("path has value in helm Value.yaml tree, mapping to output path %s", path)
		metrics.LegacyPathTranslationTotal.
			With(metrics.ResourceKindLabel.Value(inPath)).Increment()

		if err := tpath.WriteNode(cpSpecTree, path, m); err != nil {
			return err
		}

		if _, err := tpath.Delete(valueTree, util.ToYAMLPath(inPath)); err != nil {
			return err
		}
	}
	return nil
}

// isEnablementPath is helper function to check whether paths represent enablement of components in values.yaml
func (t *ReverseTranslator) isEnablementPath(path util.Path) bool {
	if len(path) < 2 || path[len(path)-1] != "enabled" {
		return false
	}

	pf := path[:len(path)-1].String()
	if specialComponentPath[pf] {
		return true
	}

	_, exist := t.ValuesToComponentName[pf]
	return exist
}

// renderComponentName renders a template of the form <path>{{.ComponentName}}<path> with
// the supplied parameters.
func renderComponentName(tmpl string, componentName string) (string, error) {
	type temp struct {
		ValueComponentName string
	}
	return util.RenderTemplate(tmpl, temp{componentName})
}
