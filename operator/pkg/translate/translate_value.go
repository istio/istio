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
	"errors"
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

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
	skipTranslate = map[name.ComponentName]bool{
		name.IstioBaseComponentName:          true,
		name.IstioOperatorComponentName:      true,
		name.IstioOperatorCustomResourceName: true,
		name.CNIComponentName:                true,
		name.IstiodRemoteComponentName:       true,
		name.ZtunnelComponentName:            true,
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
		return "", errors.New(warning)
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

		if _, err := tpath.Delete(valueTree, util.ToYAMLPath(inPath)); err != nil {
			return err
		}
	}
	return nil
}

// renderComponentName renders a template of the form <path>{{.ComponentName}}<path> with
// the supplied parameters.
func renderComponentName(tmpl string, componentName string) (string, error) {
	type temp struct {
		ValueComponentName string
	}
	return util.RenderTemplate(tmpl, temp{componentName})
}
