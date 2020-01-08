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
	"path/filepath"

	"github.com/ghodss/yaml"

	"istio.io/api/operator/v1alpha1"
	"istio.io/operator/pkg/helm"
	"istio.io/operator/pkg/manifest"
	"istio.io/operator/pkg/tpath"
	"istio.io/operator/pkg/translate"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/validate"
	version2 "istio.io/operator/version"
	"istio.io/pkg/version"
)

// getIOPS creates an IstioOperatorSpec from the following sources, overlaid sequentially:
// 1. Compiled in base, or optionally base from path pointed to in IOP stored at inFilename.
// 2. Profile overlay, if non-default overlay is selected. This also comes either from compiled in or path specified in IOP contained in inFilename.
// 3. User overlay stored in inFilename.
// 4. setOverlayYAML, which comes from --set flag passed to manifest command.
//
// Note that the user overlay at inFilename can optionally contain a file path to a set of profiles different from the
// ones that are compiled in. If it does, the starting point will be the base and profile YAMLs at that file path.
// Otherwise it will be the compiled in profile YAMLs.
// In step 3, the remaining fields in the same user overlay are applied on the resulting profile base.
func genIOPS(inFilename, profile, setOverlayYAML, ver string, force bool, l *Logger) (string, *v1alpha1.IstioOperatorSpec, error) {
	overlayYAML := ""
	var overlayIOPS *v1alpha1.IstioOperatorSpec
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
		overlayIOPS, overlayYAML, err = unmarshalAndValidateIOP(string(b), force)
		if err != nil {
			return "", nil, err
		}
		profile = overlayIOPS.Profile
	}
	if setProfile, ok := set["profile"]; ok {
		profile = setProfile.(string)
	}

	if ver != "" && !util.IsFilePath(profile) {
		pkgPath, err := fetchInstallPackage(helm.InstallURLFromVersion(ver))
		if err != nil {
			return "", nil, err
		}
		if helm.IsDefaultProfile(profile) {
			profile = filepath.Join(pkgPath, helm.ProfilesFilePath, helm.DefaultProfileFilename)
		} else {
			profile = filepath.Join(pkgPath, helm.ProfilesFilePath, profile+YAMLSuffix)
		}
	}

	// This contains the IstioOperator CR.
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

	_, baseYAML, err := unmarshalAndValidateIOP(baseCRYAML, force)
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
	if _, err := unmarshalAndValidateIOPS(mergedYAML, force, l); err != nil {
		return "", nil, err
	}

	// Merge the tree build from --set option on top of that.
	finalYAML, err := util.OverlayYAML(mergedYAML, setOverlayYAML)
	if err != nil {
		return "", nil, fmt.Errorf("could not overlay --set values over merged: %s", err)
	}

	finalIOPS, err := unmarshalAndValidateIOPS(finalYAML, force, l)
	if err != nil {
		return "", nil, err
	}
	return finalYAML, finalIOPS, nil
}

func genProfile(helmValues bool, inFilename, profile, setOverlayYAML, configPath string, force bool, l *Logger) (string, error) {
	finalYAML, finalIOPS, err := genIOPS(inFilename, profile, setOverlayYAML, "", force, l)
	if err != nil {
		return "", err
	}

	t, err := translate.NewTranslator(version2.OperatorBinaryVersion.MinorVersion)
	if err != nil {
		return "", err
	}

	if helmValues {
		finalYAML, err = t.TranslateHelmValues(finalIOPS, "")
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

func unmarshalAndValidateIOP(crYAML string, force bool) (*v1alpha1.IstioOperatorSpec, string, error) {
	// TODO: add GVK handling as appropriate.
	if crYAML == "" {
		return &v1alpha1.IstioOperatorSpec{}, "", nil
	}
	iops, _, err := manifest.ParseK8SYAMLToIstioOperatorSpec(crYAML)
	if err != nil {
		return nil, "", fmt.Errorf("could not unmarshal the overlay file: %s\n\nOriginal YAML:\n%s", err, crYAML)
	}
	if errs := validate.CheckIstioOperatorSpec(iops, false); len(errs) != 0 {
		if !force {
			return nil, "", fmt.Errorf("input file failed validation with the following errors: %s\n\nOriginal YAML:\n%s", errs, crYAML)
		}
	}
	iopsYAML, err := util.MarshalWithJSONPB(iops)
	if err != nil {
		return nil, "", fmt.Errorf("could not marshal: %s", err)
	}
	return iops, iopsYAML, nil
}

func unmarshalAndValidateIOPS(iopsYAML string, force bool, l *Logger) (*v1alpha1.IstioOperatorSpec, error) {
	iops := &v1alpha1.IstioOperatorSpec{}
	if err := util.UnmarshalWithJSONPB(iopsYAML, iops); err != nil {
		return nil, fmt.Errorf("could not unmarshal the merged YAML: %s\n\nYAML:\n%s", err, iopsYAML)
	}
	if errs := validate.CheckIstioOperatorSpec(iops, true); len(errs) != 0 {
		if !force {
			l.logAndError("Run the command with the --force flag if you want to ignore the validation error and proceed.")
			return nil, fmt.Errorf(errs.Error())
		}
		l.logAndError("Proceeding despite the following validation errors: \n", errs.Error())
	}
	return iops, nil
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
