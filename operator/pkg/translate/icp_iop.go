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
	"io/ioutil"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/validate"

	"gopkg.in/yaml.v2"

	icpv1alpha2 "istio.io/istio/operator/pkg/apis/istio/v1alpha2"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/version"
	"istio.io/istio/operator/pkg/vfs"
	binversion "istio.io/istio/operator/version"
)

const (
	IstioOperatorTreeString = `
apiVersion: operator.istio.io/v1alpha1
kind: IstioOperator
`
	iCPIOPTranslationsFilename = "translate-ICP-IOP-"
)

// ICPtoIOPTranslations returns the translations for the given binary version.
func ICPtoIOPTranslations(ver version.Version) (map[string]string, error) {
	b, err := vfs.ReadFile("translateConfig/" + iCPIOPTranslationsFilename + ver.MinorVersion.String() + ".yaml")
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	if err := yaml.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadICPtoIOPTranslations reads a file at filePath with key:value pairs in the format expected by ICPToIOP.
func ReadICPtoIOPTranslations(filePath string) (map[string]string, error) {
	b, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	if err := yaml.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ICPToIOPVer takes an IstioControlPlane YAML string and the target version as input,
// then translates it into an IstioOperator YAML string.
func ICPToIOPVer(icp string, ver version.Version) (string, error) {
	translations, err := ICPtoIOPTranslations(ver)
	if err != nil {
		return "", fmt.Errorf("could not read translate config for version %s: %s", ver, err)
	}
	return ICPToIOP(icp, translations)
}

// UnmarshalIOPOrICP takes a YAML string in either IstioControlPlane or IstioOperator formats and
// returns an IstioOperatorSpec struct and string if it is successful, performing any required translation.
func UnmarshalIOPOrICP(yml string) (*v1alpha1.IstioOperator, string, error) {
	translated, tyml := ICPToIOPYAML(yml)
	if translated {
		yml = tyml
	}
	iop, err := validate.UnmarshalIOP(yml)
	return iop, yml, err
}

// ICPToIOPYAML attempts to translate the passed in ICP YAML string to an IOP YAML strings. It returns true
// if the translation was successful.
func ICPToIOPYAML(icpYAML string) (bool, string) {
	out, err := ICPToIOPVer(icpYAML, binversion.OperatorBinaryVersion)
	if err != nil {
		return false, ""
	}
	return true, out
}

// ICPToIOP takes an IstioControlPlane YAML string and a map of translations with key:value format
// souce-path:destination-path (where paths are expressed in pkg/tpath format) and returns an IstioOperator string.
func ICPToIOP(icp string, translations map[string]string) (string, error) {
	if err := checkIstioControlPlane(icp); err != nil {
		return "", err
	}
	icps, err := getSpecSubtree(icp)
	if err != nil {
		return "", err
	}

	// Prefill the output tree with gateways if they are set to ensure we have the correct list types created.
	outTree, err := gatewaysOverlay(icps)
	if err != nil {
		return "", err
	}

	translated, err := YAMLTree(icps, outTree, translations)
	if err != nil {
		return "", err
	}

	out, err := tpath.AddSpecRoot(translated)
	if err != nil {
		return "", err
	}
	return util.OverlayYAML(IstioOperatorTreeString, out)
}

// getSpecSubtree takes a YAML tree with the root node spec and returns the subtree under this root node.
func getSpecSubtree(tree string) (string, error) {
	icpTree := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(tree), &icpTree); err != nil {
		return "", err
	}
	spec := icpTree["spec"]
	if spec == nil {
		return "", fmt.Errorf("spec field not found in input string: \n%s", tree)
	}
	outTree, err := yaml.Marshal(spec)
	if err != nil {
		return "", err
	}
	return string(outTree), nil
}

// gatewaysOverlay takes a source YAML tree and creates empty output gateways list entries for ingress and egress
// gateways if these are present in the source tree.
// Gateways must be created in the tree separately because they are a list type. Inserting into the tree dynamically
// would result in gateway paths being map types.
func gatewaysOverlay(icps string) (string, error) {
	componentsHeaderStr := `
components:
`
	gatewayStr := map[string]string{
		"ingress": `
  ingressGateways:
  - name: istio-ingressgateway
`,
		"egress": `
  egressGateways:
  - name: istio-egressgateway
`,
	}

	icpsT, err := unmarshalTree(icps)
	if err != nil {
		return "", err
	}

	out := ""
	componentsHeaderSet := false
	for _, gt := range []string{"ingress", "egress"} {
		if _, found, _ := tpath.GetFromTreePath(icpsT, util.PathFromString(fmt.Sprintf("gateways.components.%sGateway", gt))); found {
			if !componentsHeaderSet {
				componentsHeaderSet = true
				out += componentsHeaderStr
			}
			out += gatewayStr[gt]
		}
	}
	return out, nil
}

// checkIstioControlPlane reports whether icpStr contains an IstioControlPlane with correct object headers.
func checkIstioControlPlane(icpStr string) error {
	// First, check if this is even an IstioControlPlane kind.
	icp := &icpv1alpha2.IstioControlPlane{}
	if err := util.UnmarshalWithJSONPB(icpStr, icp, false); err != nil {
		return fmt.Errorf("not a valid IstioControlPlane")
	}
	o, err := object.ParseYAMLToK8sObject([]byte(icpStr))
	if err != nil {
		return err
	}

	if o.Kind != icpv1alpha2.IstioControlPlaneKindStr {
		return fmt.Errorf("incorrect Kind, got %s, want %s", icp.TypeMeta.Kind, icpv1alpha2.IstioControlPlaneKindStr)
	}
	return nil
}

func unmarshalTree(tree string) (map[string]interface{}, error) {
	out := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(tree), &out); err != nil {
		return nil, err
	}
	return out, nil
}
