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

package mesh

import (
	"fmt"
	"io/ioutil"

	"istio.io/operator/pkg/apis/istio/v1alpha2"

	"istio.io/operator/pkg/manifest"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"

	"istio.io/operator/pkg/component/component"
	"istio.io/operator/pkg/helm"
	"istio.io/operator/pkg/tpath"
	"istio.io/operator/pkg/translate"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/validate"
	"istio.io/operator/pkg/version"
)

type profileDumpArgs struct {
	// inFilename is the path to the input IstioControlPlane CR.
	inFilename string
	// If set, display the translated Helm values rather than IstioControlPlaneSpec.
	helmValues bool
	// configPath sets the root node for the subtree to display the config for.
	configPath string
	// set is a string with element format "path=value" where path is an IstioControlPlane path and the value is a
	// value to set the node at that path to.
	set []string
}

func addProfileDumpFlags(cmd *cobra.Command, args *profileDumpArgs) {
	cmd.PersistentFlags().StringVarP(&args.inFilename, "filename", "f", "", filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.configPath, "config-path", "p", "",
		"The path the root of the configuration subtree to dump e.g. trafficManagement.components.pilot. By default, dump whole tree. ")
	cmd.PersistentFlags().BoolVarP(&args.helmValues, "helm-values", "", false,
		"If set, dumps the Helm values that IstioControlPlaceSpec is translated to before manifests are rendered.")
	cmd.PersistentFlags().StringSliceVarP(&args.set, "set", "s", nil, setFlagHelpStr)
}

func profileDumpCmd(rootArgs *rootArgs, pdArgs *profileDumpArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "dump",
		Short: "Dumps an Istio configuration profile.",
		Long:  "The dump subcommand is used to dump the values in an Istio configuration profile.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			profileDump(rootArgs, pdArgs)
		}}

}

func profileDump(args *rootArgs, pdArgs *profileDumpArgs) {
	checkLogsOrExit(args)

	writer, err := getWriter("")
	if err != nil {
		logAndFatalf(args, err.Error())
	}
	defer func() {
		if err := writer.Close(); err != nil {
			logAndFatalf(args, "Did not close output successfully: %v", err)
		}
	}()

	overlayFromSet, err := makeTreeFromSetList(pdArgs.set)
	if err != nil {
		logAndFatalf(args, err.Error())
	}

	y, err := genProfile(pdArgs.helmValues, pdArgs.inFilename, overlayFromSet, pdArgs.configPath)
	if err != nil {
		logAndFatalf(args, err.Error())
	}

	if _, err := writer.WriteString(y); err != nil {
		logAndFatalf(args, "Could not write values; %s", err)
	}
}

func genProfile(helmValues bool, inFilename, setOverlayYAML, configPath string) (string, error) {
	overlayCRYAML := ""
	if inFilename != "" {
		b, err := ioutil.ReadFile(inFilename)
		if err != nil {
			return "", fmt.Errorf("could not read values file %s: %s", inFilename, err)
		}
		overlayCRYAML = string(b)
	}

	overlayICPS, overlayYAML, err := unmarshalAndValidateICP(overlayCRYAML)
	if err != nil {
		return "", err
	}

	// Now read the base profile specified in the user spec.
	fname, err := helm.FilenameFromProfile(overlayICPS.Profile)
	if err != nil {
		return "", fmt.Errorf("could not get filename from profile: %s", err)
	}
	// This contains the IstioControlPlane CR.
	baseCRYAML, err := helm.ReadValuesYAML(overlayICPS.Profile)
	if err != nil {
		return "", fmt.Errorf("could not read the profile values for %s: %s", fname, err)
	}

	_, baseYAML, err := unmarshalAndValidateICP(baseCRYAML)
	if err != nil {
		return "", err
	}

	// Merge base and overlay.
	mergedYAML, err := helm.OverlayYAML(baseYAML, overlayYAML)
	if err != nil {
		return "", fmt.Errorf("could not overlay user config over base: %s", err)
	}
	if _, err := unmarshalAndValidateICPS(mergedYAML); err != nil {
		return "", err
	}

	// Merge the tree build from --set option on top of that.
	finalYAML, err := helm.OverlayYAML(mergedYAML, setOverlayYAML)
	if err != nil {
		return "", fmt.Errorf("could not overlay --set values over merged: %s", err)
	}

	finalICPS, err := unmarshalAndValidateICPS(finalYAML)
	if err != nil {
		return "", err
	}

	t, err := translate.NewTranslator(version.NewMinorVersion(1, 2))
	if err != nil {
		return "", err
	}

	if helmValues {
		finalYAML, err = component.TranslateHelmValues(finalICPS, t, "")
		if err != nil {
			return "", err
		}
	}

	finalYAML, err = getConfigSubtree(finalYAML, configPath)
	if err != nil {
		return "", err
	}

	return finalYAML, err
}

func unmarshalAndValidateICP(crYAML string) (*v1alpha2.IstioControlPlaneSpec, string, error) {
	// TODO: add GVK handling as appropriate.
	if crYAML == "" {
		return &v1alpha2.IstioControlPlaneSpec{}, "", nil
	}
	icps, _, err := manifest.ParseK8SYAMLToIstioControlPlaneSpec(crYAML)
	if err != nil {
		return nil, "", fmt.Errorf("could not unmarshal the overlay file: %s\n\nOriginal YAML:\n%s", err, crYAML)
	}
	if errs := validate.CheckIstioControlPlaneSpec(icps, false); len(errs) != 0 {
		return nil, "", fmt.Errorf("input file failed validation with the following errors: %s\n\nOriginal YAML:\n%s", errs, crYAML)
	}
	icpsYAML, err := util.MarshalWithJSONPB(icps)
	if err != nil {
		return nil, "", fmt.Errorf("could not marshal: %s", err)
	}
	return icps, icpsYAML, nil
}

func unmarshalAndValidateICPS(icpsYAML string) (*v1alpha2.IstioControlPlaneSpec, error) {
	icps := &v1alpha2.IstioControlPlaneSpec{}
	if err := util.UnmarshalWithJSONPB(icpsYAML, icps); err != nil {
		return nil, fmt.Errorf("could not unmarshal the merged YAML: %s\n\nYAML:\n%s", err, icpsYAML)
	}
	if errs := validate.CheckIstioControlPlaneSpec(icps, true); len(errs) != 0 {
		return nil, fmt.Errorf(errs.Error())
	}
	return icps, nil
}

func getConfigSubtree(manifest, path string) (string, error) {
	root := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(manifest), &root); err != nil {
		return "", err
	}

	nc, _, err := tpath.GetPathContext(root, util.PathFromString(path))
	if err != nil {
		return "", err
	}
	out, err := yaml.Marshal(nc.Node)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
