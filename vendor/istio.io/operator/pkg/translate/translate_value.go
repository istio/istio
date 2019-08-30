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

package translate

import (
	"fmt"

	"github.com/ghodss/yaml"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/name"
	"istio.io/operator/pkg/tpath"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/version"
	"istio.io/pkg/log"
)

// ReverseTranslator is a set of mappings to translate between values.yaml and API paths, charts, k8s paths.
type ReverseTranslator struct {
	Version version.MinorVersion
	// APIMapping is Values.yaml path to API path mapping using longest prefix match. If the path is a non-leaf node,
	// the output path is the matching portion of the path, plus any remaining output path.
	APIMapping map[string]*Translation
	// KubernetesPatternMapping defines mapping patterns from k8s resource paths to IstioControlPlane API paths.
	KubernetesPatternMapping map[string]string
	// KubernetesMapping defines actual k8s mappings generated from KubernetesPatternMapping before each translation.
	KubernetesMapping map[string]*Translation
	// ValuesToFeatureComponentName defines mapping from value path to feature and component name in API paths.
	ValuesToComponentName map[string]name.ComponentName
	// NamespaceMapping maps namespace defined in value.yaml to that in API spec. Not every components have namespace defined in value.yaml tree.
	NamespaceMapping map[string][]string
}

var (
	// ReverseTranslators maps a minor version to a corresponding ReverseTranslator.
	ReverseTranslators = map[version.MinorVersion]*ReverseTranslator{
		version.NewMinorVersion(1, 3): {
			APIMapping: map[string]*Translation{},
			KubernetesPatternMapping: map[string]string{
				"{{.ValueComponentName}}.podAntiAffinityLabelSelector": "{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8s." +
					"Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution",
				"{{.ValueComponentName}}.podAntiAffinityTermLabelSelector": "{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8s." +
					"Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution",
				"{{.ValueComponentName}}.env":                 "{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8s.Env",
				"{{.ValueComponentName}}.autoscaleEnabled":    "{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8s.HpaSpec",
				"{{.ValueComponentName}}.imagePullPolicy":     "{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8s.ImagePullPolicy",
				"{{.ValueComponentName}}.nodeSelector":        "{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8s.NodeSelector",
				"{{.ValueComponentName}}.podDisruptionBudget": "{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8s.PodDisruptionBudget",
				"{{.ValueComponentName}}.podAnnotations":      "{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8s.PodAnnotations",
				"{{.ValueComponentName}}.priorityClassName":   "{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8s.PriorityClassName",
				"{{.ValueComponentName}}.readinessProbe":      "{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8s.ReadinessProbe",
				"{{.ValueComponentName}}.replicaCount":        "{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8s.ReplicaCount",
				"{{.ValueComponentName}}.resources":           "{{.FeatureName}}.Components.{{.ComponentName}}.Common.K8s.Resources",
			},
			KubernetesMapping:     map[string]*Translation{},
			ValuesToComponentName: map[string]name.ComponentName{},
			NamespaceMapping: map[string][]string{
				"global.istioNamespace":     {"security.components.namespace"},
				"global.telemetryNamespace": {"telemetry.components.namespace"},
				"global.policyNamespace":    {"policy.components.namespace"},
				"global.configNamespace":    {"configManagement.components.namespace"},
			},
		},
	}
	// Component enablement mapping. Ex "{{.ValueComponent}}.enabled": {"{{.FeatureName}}.Components.{{.ComponentName}}.Common.enabled}", nil},
	// Feature enablement mapping. Ex: "{{.ValueComponent}}.enabled": {"{{.FeatureName}}.enabled}", nil},
	componentEnablementPattern = "{{.FeatureName}}.Components.{{.ComponentName}}.Common.Enabled"
	componentValuesPattern     = "{{.FeatureName}}.Components.{{.ComponentName}}.Common.Values"
)

// initAPIMapping generate the reverse mapping from original translator apiMapping.
func (t *ReverseTranslator) initAPIAndComponentMapping(vs version.MinorVersion) error {
	ts, err := NewTranslator(vs)
	if err != nil {
		return err
	}
	for valKey, outVal := range ts.APIMapping {
		t.APIMapping[outVal.outPath] = &Translation{valKey, nil}
	}

	for cn, cm := range ts.ComponentMaps {
		if cn != name.IstioBaseComponentName {
			t.ValuesToComponentName[cm.ToHelmValuesTreeRoot] = cn
		}
	}
	return nil
}

// initK8SMapping generates the k8s settings mapping for components that are enabled based on templates.
func (t *ReverseTranslator) initK8SMapping(valueTree map[string]interface{}) error {
	outputMapping := make(map[string]*Translation)
	for valKey, componentName := range t.ValuesToComponentName {
		featureName := name.ComponentNameToFeatureName[componentName]
		cnEnabled, err := name.IsComponentEnabledFromValue(valKey, valueTree)
		if err != nil {
			return err
		}
		if !cnEnabled {
			log.Infof("Component:%s disabled, skip k8s mapping", componentName)
			continue
		}
		for K8SValKey, outPathTmpl := range t.KubernetesPatternMapping {
			newKey, err := renderComponentName(K8SValKey, valKey)
			if err != nil {
				return err
			}
			newVal, err := renderFeatureComponentPathTemplate(outPathTmpl, featureName, componentName)
			if err != nil {
				return err
			}
			outputMapping[newKey] = &Translation{newVal, nil}
		}
	}

	t.KubernetesMapping = outputMapping
	return nil
}

// NewReverseTranslator creates a new ReverseTranslator for minorVersion and returns a ptr to it.
func NewReverseTranslator(minorVersion version.MinorVersion) (*ReverseTranslator, error) {
	t := ReverseTranslators[minorVersion]
	if t == nil {
		return nil, fmt.Errorf("no value.yaml translator available for version %s", minorVersion)
	}

	err := t.initAPIAndComponentMapping(minorVersion)
	if err != nil {
		return nil, fmt.Errorf("error initialize API mapping: %s", err)
	}
	return t, nil
}

// TranslateFromValueToSpec translates from values struct to IstioControlPlaneSpec.
func (t *ReverseTranslator) TranslateFromValueToSpec(values *v1alpha2.Values) (controlPlaneSpec *v1alpha2.IstioControlPlaneSpec, err error) {
	valueYaml, err := yaml.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("error when marshalling value struct %v", err)
	}

	var yamlTree = make(map[string]interface{})
	err = yaml.Unmarshal(valueYaml, &yamlTree)
	if err != nil {
		return nil, fmt.Errorf("error when unmarshalling into untype tree %v", err)
	}

	outputTree := make(map[string]interface{})
	err = t.TranslateTree(yamlTree, outputTree, nil)
	if err != nil {
		return nil, err
	}
	outputVal, err := yaml.Marshal(outputTree)
	if err != nil {
		return nil, err
	}

	var cpSpec = &v1alpha2.IstioControlPlaneSpec{}
	err = util.UnmarshalWithJSONPB(string(outputVal), cpSpec)

	if err != nil {
		return nil, fmt.Errorf("error when unmarshalling into control plane spec %v, \nyaml: %s", err, outputVal)
	}

	return cpSpec, nil
}

// TranslateTree translates input value.yaml Tree to ControlPlaneSpec Tree.
func (t *ReverseTranslator) TranslateTree(valueTree map[string]interface{}, cpSpecTree map[string]interface{}, path util.Path) error {
	// translate enablement and namespace
	err := t.setEnablementAndNamespacesFromValue(valueTree, cpSpecTree)
	if err != nil {
		return fmt.Errorf("error when translating enablement and namespace from value.yaml tree: %v", err)
	}
	// translate with api mapping
	err = t.translateTree(valueTree, cpSpecTree)
	if err != nil {
		return fmt.Errorf("error when translating value.yaml tree with global mapping: %v", err)
	}

	// translate with k8s mapping
	err = t.initK8SMapping(valueTree)
	if err != nil {
		return fmt.Errorf("error when initiating k8s mapping: %v", err)
	}
	err = t.translateK8sTree(valueTree, cpSpecTree)
	if err != nil {
		return fmt.Errorf("error when translating value.yaml tree with kubernetes mapping: %v", err)
	}

	// translate remaining untranslated paths into component values
	err = t.translateRemainingPaths(valueTree, cpSpecTree, nil)
	if err != nil {
		return fmt.Errorf("error when translating remaining path: %v", err)
	}
	return nil
}

// setEnablementAndNamespaces translates the enablement and namespace value of each component in the baseYAML values
// tree, based on feature/component inheritance relationship.
func (t *ReverseTranslator) setEnablementAndNamespacesFromValue(valueSpec map[string]interface{}, root map[string]interface{}) error {
	for cnv, cni := range t.ValuesToComponentName {
		enabled, err := name.IsComponentEnabledFromValue(cnv, valueSpec)
		if err != nil {
			return err
		}
		featureName := name.ComponentNameToFeatureName[cni]
		tmpl := componentEnablementPattern
		ceVal, err := renderFeatureComponentPathTemplate(tmpl, featureName, cni)
		if err != nil {
			return err
		}
		outCP := util.ToYAMLPath(ceVal)
		// set component enablement
		if err := tpath.WriteNode(root, outCP, enabled); err != nil {
			return err
		}
		// set feature enablement
		feVal := featureName + ".Enabled"
		outFP := util.ToYAMLPath(string(feVal))
		curEnabled, found, _ := name.GetFromTreePath(root, outFP)
		if !found {
			if err := tpath.WriteNode(root, outFP, enabled); err != nil {
				return err
			}
		} else if curEnabled == false && enabled {
			if err := tpath.WriteNode(root, outFP, enabled); err != nil {
				return err
			}
		}
	}

	// set namespace
	for vp, nsList := range t.NamespaceMapping {
		namespace, err := name.NamespaceFromValue(vp, valueSpec)
		if err != nil {
			return err
		}
		for _, ns := range nsList {
			if err := tpath.WriteNode(root, util.ToYAMLPath(ns), namespace); err != nil {
				return err
			}
		}
	}
	return nil
}

// translateHPASpec translates HPA related configurations from helm values.yaml tree.
func translateHPASpec(inPath string, outPath string, value interface{}, valueTree map[string]interface{}, cpSpecTree map[string]interface{}) error {
	asEnabled, ok := value.(bool)
	if !ok {
		return fmt.Errorf("expect autoscaleEnabled node type to be bool but got: %T", value)
	}
	if !asEnabled {
		return nil
	}
	newP := util.PathFromString(inPath)[0]
	minPath := newP + ".autoscaleMin"
	asMin, found, err := name.GetFromTreePath(valueTree, util.ToYAMLPath(minPath))
	if found && err == nil {
		if err := tpath.WriteNode(cpSpecTree, util.ToYAMLPath(outPath+".minReplicas"), asMin); err != nil {
			return err
		}
	}
	if _, err := tpath.DeleteFromTree(valueTree, util.ToYAMLPath(minPath), util.ToYAMLPath(minPath)); err != nil {
		return err
	}
	maxPath := newP + ".autoscaleMax"
	asMax, found, err := name.GetFromTreePath(valueTree, util.ToYAMLPath(maxPath))
	if found && err == nil {
		log.Infof("path has value in helm Value.yaml tree, mapping to output path %s", outPath)
		if err := tpath.WriteNode(cpSpecTree, util.ToYAMLPath(outPath+".maxReplicas"), asMax); err != nil {
			return err
		}
	}
	if _, err := tpath.DeleteFromTree(valueTree, util.ToYAMLPath(maxPath), util.ToYAMLPath(maxPath)); err != nil {
		return err
	}
	return nil
}

// translateEnv translates env value from helm values.yaml tree.
func translateEnv(outPath string, value interface{}, cpSpecTree map[string]interface{}) error {
	envMap, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Errorf("expect env node type to be map[string]interface{} but got: %T", value)
	}
	outEnv := make([]map[string]interface{}, len(envMap))
	cnt := 0
	for k, v := range envMap {
		outEnv[cnt] = make(map[string]interface{})
		outEnv[cnt]["name"] = k
		outEnv[cnt]["value"] = v
		cnt++
	}
	log.Infof("path has value in helm Value.yaml tree, mapping to output path %s", outPath)
	if err := tpath.WriteNode(cpSpecTree, util.ToYAMLPath(outPath), outEnv); err != nil {
		return err
	}
	return nil
}

// translateAffinity translates Affinity related configurations from helm values.yaml tree
func translateAffinity(outPath string, value interface{}, cpSpecTree map[string]interface{}) error {
	affinityNode, ok := value.([]interface{})
	if !ok {
		return fmt.Errorf("expect affinity node type to be []interface{} but got: %T", affinityNode)
	}
	log.Infof("path has value in helm Value.yaml tree, mapping to output path %s", affinityNode)
	if err := tpath.WriteNode(cpSpecTree, util.ToYAMLPath(outPath), affinityNode); err != nil {
		return err
	}
	return nil
}

// translateK8sTree is internal method for translating K8s configurations from value.yaml tree.
func (t *ReverseTranslator) translateK8sTree(valueTree map[string]interface{},
	cpSpecTree map[string]interface{}) error {
	for inPath, v := range t.KubernetesMapping {
		log.Infof("Checking for k8s path %s in helm Value.yaml tree", inPath)
		m, found, err := name.GetFromTreePath(valueTree, util.ToYAMLPath(inPath))
		if err != nil {
			return err
		}
		if !found {
			log.Infof("path %s not found in helm Value.yaml tree, skip mapping.", inPath)
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

		path := util.PathFromString(inPath)
		k8sSettingName := ""
		if len(path) != 0 {
			k8sSettingName = path[len(path)-1]
		}

		switch k8sSettingName {
		case "autoscaleEnabled":
			err := translateHPASpec(inPath, v.outPath, m, valueTree, cpSpecTree)
			if err != nil {
				return fmt.Errorf("error in translating K8s HPA spec: %s", err)
			}

		case "env":
			err := translateEnv(v.outPath, m, cpSpecTree)
			if err != nil {
				return fmt.Errorf("error in translating k8s Env: %s", err)
			}

		case "podAntiAffinityLabelSelector", "podAntiAffinityTermLabelSelector":
			err := translateAffinity(v.outPath, m, cpSpecTree)
			if err != nil {
				return fmt.Errorf("error in translating k8s Affinity: %s", err)
			}

		default:
			output := util.ToYAMLPath(v.outPath)
			log.Infof("path has value in helm Value.yaml tree, mapping to output path %s", output)

			if err := tpath.WriteNode(cpSpecTree, output, m); err != nil {
				return err
			}
		}

		if _, err := tpath.DeleteFromTree(valueTree, util.ToYAMLPath(inPath), util.ToYAMLPath(inPath)); err != nil {
			return err
		}
	}
	return nil
}

// translateRemainingPaths translates remaining paths that are not availing in existing mappings.
func (t *ReverseTranslator) translateRemainingPaths(valueTree map[string]interface{},
	cpSpecTree map[string]interface{}, path util.Path) error {
	for key, val := range valueTree {
		newPath := append(path, key)
		// value set to nil means no translation needed or being translated already.
		if val == nil {
			continue
		}
		switch node := val.(type) {
		case map[string]interface{}:
			err := t.translateRemainingPaths(node, cpSpecTree, newPath)
			if err != nil {
				return err
			}
		case []interface{}:
			errs := util.Errors{}
			for _, newNode := range node {
				newMap, ok := newNode.(map[string]interface{})
				if !ok {
					return fmt.Errorf("fail to convert node to map[string] interface")
				}
				errs = util.AppendErr(errs, t.translateRemainingPaths(newMap, cpSpecTree, newPath))
			}
			if errs != nil {
				return errs
			}
		// remaining leaf need to be put into common.values
		default:
			err := t.translateToCommonValues(cpSpecTree, newPath, val)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// translateToCommonValues translates paths to common.values
func (t *ReverseTranslator) translateToCommonValues(root map[string]interface{}, path util.Path, value interface{}) error {
	if len(path) < 2 {
		log.Warn("path is empty or only contains root component node when translating to common.values")
		return nil
	}
	// path starts with ValueComponent name or global
	cn := path[0]
	// get rid of component name at the root
	vp := path[1:]
	newCN := t.ValuesToComponentName[cn]
	if newCN == "" {
		// some components path len is 2, e.g. mixer.policy
		if len(path) > 1 {
			cn = path[:2].String()
			newCN = t.ValuesToComponentName[cn]
			vp = path[2:]
		}
		if newCN == "" {
			return nil
		}
	}
	// enabled node value should not be added to common.values path
	if path[len(path)-1] == "enabled" {
		return nil
	}
	fn := name.ComponentNameToFeatureName[newCN]
	outPath, err := renderCommonComponentValues(componentValuesPattern, string(newCN), string(fn))
	if err != nil {
		return err
	}
	return tpath.WriteNode(root, append(util.ToYAMLPath(outPath), vp...), value)
}

// translateTree is internal method for translating value.yaml tree
func (t *ReverseTranslator) translateTree(valueTree map[string]interface{},
	cpSpecTree map[string]interface{}) error {
	for inPath, v := range t.APIMapping {
		log.Infof("Checking for path %s in helm Value.yaml tree", inPath)
		m, found, err := name.GetFromTreePath(valueTree, util.ToYAMLPath(inPath))
		if err != nil {
			return err
		}
		if !found {
			log.Infof("path %s not found in helm Value.yaml tree, skip mapping.", inPath)
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

		path := util.ToYAMLPath(v.outPath)
		log.Infof("path has value in helm Value.yaml tree, mapping to output path %s", path)

		if err := tpath.WriteNode(cpSpecTree, path, m); err != nil {
			return err
		}

		if _, err := tpath.DeleteFromTree(valueTree, util.ToYAMLPath(inPath), util.ToYAMLPath(inPath)); err != nil {
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
	return renderTemplate(tmpl, temp{componentName})
}

// renderCommonComponentValues renders a template of the form "{{.FeatureName}}.Components.{{.ComponentName}}.Common.Values" with
// the supplied parameters.
func renderCommonComponentValues(tmpl string, componentName string, featureName string) (string, error) {
	type temp struct {
		FeatureName   string
		ComponentName string
	}
	return renderTemplate(tmpl, temp{FeatureName: featureName, ComponentName: componentName})
}
