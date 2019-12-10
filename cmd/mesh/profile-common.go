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

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/helm"
	"istio.io/operator/pkg/manifest"
	"istio.io/operator/pkg/tpath"
	"istio.io/operator/pkg/translate"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/validate"
	version2 "istio.io/operator/version"
	"istio.io/pkg/version"
)

// getICPS creates an IstioControlPlaneSpec from the following sources, overlaid sequentially:
// 1. Compiled in base, or optionally base from path pointed to in ICP stored at inFilename.
// 2. Profile overlay, if non-default overlay is selected. This also comes either from compiled in or path specified in ICP contained in inFilename.
// 3. User overlay stored in inFilename.
// 4. setOverlayYAML, which comes from --set flag passed to manifest command.
//
// Note that the user overlay at inFilename can optionally contain a file path to a set of profiles different from the
// ones that are compiled in. If it does, the starting point will be the base and profile YAMLs at that file path.
// Otherwise it will be the compiled in profile YAMLs.
// In step 3, the remaining fields in the same user overlay are applied on the resulting profile base.
func genICPS(inFilename, profile, setOverlayYAML string, force bool, l *Logger) (string, *v1alpha2.IstioControlPlaneSpec, error) {
	overlayYAML := ""
	var overlayICPS *v1alpha2.IstioControlPlaneSpec
	set := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(setOverlayYAML), &set)
	if err != nil {
		return "", nil, fmt.Errorf("could not Unmarshal overlay Set%s: %s", setOverlayYAML, err)
	}
	if inFilename != "" {
		b, err := ioutil.ReadFile(inFilename)
		if err != nil {
			return "", nil, fmt.Errorf("could not read values from file %s: %s", inFilename, err)
		}
		overlayICPS, overlayYAML, err = unmarshalAndValidateICP(string(b), force)
		if err != nil {
			return "", nil, err
		}
		profile = overlayICPS.Profile
	}
	if setProfile, ok := set["profile"]; ok {
		profile = setProfile.(string)
	}

	// This contains the IstioControlPlane CR.
	baseCRYAML, err := helm.ReadProfileYAML(profile)
	if err != nil {
		return "", nil, fmt.Errorf("could not read the profile values for %s: %s", profile, err)
	}

	if !helm.IsDefaultProfile(profile) {
		// Profile definitions are relative to the default profile, so read that first.
		dfn, err := helm.DefaultFilenameForProfile(profile)
		if err != nil {
			return "", nil, err
		}
		defaultYAML, err := helm.ReadProfileYAML(dfn)
		if err != nil {
			return "", nil, fmt.Errorf("could not read the default profile values for %s: %s", dfn, err)
		}
		baseCRYAML, err = util.OverlayYAML(defaultYAML, baseCRYAML)
		if err != nil {
			return "", nil, fmt.Errorf("could not overlay the profile over the default %s: %s", profile, err)
		}
	}

	_, baseYAML, err := unmarshalAndValidateICP(baseCRYAML, force)
	if err != nil {
		return "", nil, err
	}

	// Due to the fact that base profile is compiled in before a tag can be created, we must allow an additional
	// override from variables that are set during release build time.
	hub := version.DockerInfo.Hub
	tag := version.DockerInfo.Tag
	if hub != "unknown" && tag != "unknown" {
		buildHubTagOverlayYAML, err := helm.GenerateHubTagOverlay(hub, tag)
		if err != nil {
			return "", nil, err
		}
		baseYAML, err = util.OverlayYAML(baseYAML, buildHubTagOverlayYAML)
		if err != nil {
			return "", nil, err
		}
	}

	// Merge base and overlay.
	mergedYAML, err := util.OverlayYAML(baseYAML, overlayYAML)
	if err != nil {
		return "", nil, fmt.Errorf("could not overlay user config over base: %s", err)
	}
	if _, err := unmarshalAndValidateICPS(mergedYAML, force, l); err != nil {
		return "", nil, err
	}

	// Merge the tree build from --set option on top of that.
	finalYAML, err := util.OverlayYAML(mergedYAML, setOverlayYAML)
	if err != nil {
		return "", nil, fmt.Errorf("could not overlay --set values over merged: %s", err)
	}

	finalICPS, err := unmarshalAndValidateICPS(finalYAML, force, l)
	if err != nil {
		return "", nil, err
	}
	return finalYAML, finalICPS, nil
}

func genProfile(helmValues bool, inFilename, profile, setOverlayYAML, configPath string, force bool, l *Logger) (string, error) {
	finalYAML, finalICPS, err := genICPS(inFilename, profile, setOverlayYAML, force, l)
	if err != nil {
		return "", err
	}

	t, err := translate.NewTranslator(version2.OperatorBinaryVersion.MinorVersion)
	if err != nil {
		return "", err
	}

	if helmValues {
		finalYAML, err = t.TranslateHelmValues(finalICPS, "")
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

func unmarshalAndValidateICP(crYAML string, force bool) (*v1alpha2.IstioControlPlaneSpec, string, error) {
	// TODO: add GVK handling as appropriate.
	if crYAML == "" {
		return &v1alpha2.IstioControlPlaneSpec{}, "", nil
	}
	icps, _, err := manifest.ParseK8SYAMLToIstioControlPlaneSpec(crYAML)
	if err != nil {
		return nil, "", fmt.Errorf("could not unmarshal the overlay file: %s\n\nOriginal YAML:\n%s", err, crYAML)
	}
	if errs := validate.CheckIstioControlPlaneSpec(icps, false); len(errs) != 0 {
		if !force {
			return nil, "", fmt.Errorf("input file failed validation with the following errors: %s\n\nOriginal YAML:\n%s", errs, crYAML)
		}
	}
	icpsYAML, err := util.MarshalWithJSONPB(icps)
	if err != nil {
		return nil, "", fmt.Errorf("could not marshal: %s", err)
	}
	return icps, icpsYAML, nil
}

func unmarshalAndValidateICPS(icpsYAML string, force bool, l *Logger) (*v1alpha2.IstioControlPlaneSpec, error) {
	icps := &v1alpha2.IstioControlPlaneSpec{}
	if err := util.UnmarshalWithJSONPB(icpsYAML, icps); err != nil {
		return nil, fmt.Errorf("could not unmarshal the merged YAML: %s\n\nYAML:\n%s", err, icpsYAML)
	}
	if errs := validate.CheckIstioControlPlaneSpec(icps, true); len(errs) != 0 {
		if !force {
			l.logAndError("Run the command with the --force flag if you want to ignore the validation error and proceed.")
			return nil, fmt.Errorf(errs.Error())
		}
		l.logAndError("Proceeding despite the following validation errors: \n", errs.Error())
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
