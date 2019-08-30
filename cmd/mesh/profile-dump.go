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

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/component/component"
	"istio.io/operator/pkg/helm"
	"istio.io/operator/pkg/manifest"
	"istio.io/operator/pkg/tpath"
	"istio.io/operator/pkg/translate"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/validate"
	binversion "istio.io/operator/version"
)

type profileDumpArgs struct {
	// inFilename is the path to the input IstioControlPlane CR.
	inFilename string
	// If set, display the translated Helm values rather than IstioControlPlaneSpec.
	helmValues bool
	// configPath sets the root node for the subtree to display the config for.
	configPath string
}

func addProfileDumpFlags(cmd *cobra.Command, args *profileDumpArgs) {
	cmd.PersistentFlags().StringVarP(&args.inFilename, "filename", "f", "", filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.configPath, "config-path", "p", "",
		"The path the root of the configuration subtree to dump e.g. trafficManagement.components.pilot. By default, dump whole tree. ")
	cmd.PersistentFlags().BoolVarP(&args.helmValues, "helm-values", "", false,
		"If set, dumps the Helm values that IstioControlPlaceSpec is translated to before manifests are rendered.")
}

func profileDumpCmd(rootArgs *rootArgs, pdArgs *profileDumpArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "dump",
		Short: "Dumps an Istio configuration profile.",
		Long:  "The dump subcommand dumps the values in an Istio configuration profile.",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			l := newLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.OutOrStderr())
			profileDump(args, rootArgs, pdArgs, l)
		}}

}

func profileDump(args []string, rootArgs *rootArgs, pdArgs *profileDumpArgs, l *logger) {
	initLogsOrExit(rootArgs)

	if len(args) == 1 && pdArgs.inFilename != "" {
		l.logAndFatal("Cannot specify both profile name and filename flag.")
	}

	profile := ""
	if len(args) == 1 {
		profile = args[0]
	}
	y, err := genProfile(pdArgs.helmValues, pdArgs.inFilename, profile, "", pdArgs.configPath)
	if err != nil {
		l.logAndFatal(err.Error())
	}

	l.print(y + "\n")
}

func genProfile(helmValues bool, inFilename, profile, setOverlayYAML, configPath string) (string, error) {
	overlayYAML := ""
	var overlayICPS *v1alpha2.IstioControlPlaneSpec
	if inFilename != "" {
		b, err := ioutil.ReadFile(inFilename)
		if err != nil {
			return "", fmt.Errorf("could not read values file %s: %s", inFilename, err)
		}
		overlayICPS, overlayYAML, err = unmarshalAndValidateICP(string(b))
		if err != nil {
			return "", err
		}
		profile = overlayICPS.Profile
	}

	// This contains the IstioControlPlane CR.
	baseCRYAML, err := helm.ReadValuesYAML(profile)
	if err != nil {
		return "", fmt.Errorf("could not read the profile values for %s: %s", profile, err)
	}

	if !helm.IsDefaultProfile(profile) {
		// Profile definitions are relative to the default profile, so read that first.
		dfn, err := helm.DefaultFilenameForProfile(profile)
		if err != nil {
			return "", err
		}
		defaultYAML, err := helm.ReadValuesYAML(dfn)
		if err != nil {
			return "", fmt.Errorf("could not read the default profile values for %s: %s", dfn, err)
		}
		baseCRYAML, err = helm.OverlayYAML(defaultYAML, baseCRYAML)
		if err != nil {
			return "", fmt.Errorf("could not overlay the profile over the default %s: %s", profile, err)
		}
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

	t, err := translate.NewTranslator(binversion.OperatorBinaryVersion.MinorVersion)
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
