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

	"path/filepath"

	"github.com/ghodss/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	"istio.io/pkg/log"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/validate"
	binversion "istio.io/istio/operator/version"
	pkgversion "istio.io/pkg/version"
)

var scope = log.RegisterScope("installer", "installer", 0)

// getIOPS creates an IstioOperatorSpec from the following sources, overlaid sequentially:
// 1. Compiled in base, or optionally base from paths pointing to one or multiple ICP files at inFilename.
// 2. Profile overlay, if non-default overlay is selected. This also comes either from compiled in or path specified in IOP contained in inFilename.
// 3. User overlay stored in inFilename.
// 4. setOverlayYAML, which comes from --set flag passed to manifest command.
//
// Note that the user overlay at inFilename can optionally contain a file path to a set of profiles different from the
// ones that are compiled in. If it does, the starting point will be the base and profile YAMLs at that file path.
// Otherwise it will be the compiled in profile YAMLs.
// In step 3, the remaining fields in the same user overlay are applied on the resulting profile base.
func genIOPS(inFilename []string, profile, setOverlayYAML, ver string,
	force bool, kubeConfig *rest.Config, l *Logger) (string, *v1alpha1.IstioOperatorSpec, error) {
	overlayYAML := ""
	var overlayIOPS *v1alpha1.IstioOperatorSpec
	set := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(setOverlayYAML), &set)
	if err != nil {
		return "", nil, fmt.Errorf("could not Unmarshal overlay Set%s: %s", setOverlayYAML, err)
	}
	if inFilename != nil {
		inputYaml, err := ReadLayeredYAMLs(inFilename)
		if err != nil {
			return "", nil, fmt.Errorf("could not read values from file %s: %s", inFilename, err)
		}
		overlayIOPS, overlayYAML, err = unmarshalAndValidateIOP(inputYaml, force)
		if err != nil {
			iopYAML, translateErr := translate.ICPToIOPVer(inputYaml, binversion.OperatorBinaryVersion)
			if translateErr != nil {
				return "", nil, fmt.Errorf("could not unmarshal yaml or translate it to IOP: %s, %s\n\nOriginal YAML:\n%s",
					err, translateErr, inputYaml)
			}
			l.logAndPrintf("%s\n\nIstio Operator CR has been upgraded. "+
				"Your IstioControlPlane CR has been translated into IstioOperator CR above.\n"+
				"Please keep the new IstioOperator CR for your future install or upgrade.", iopYAML)
			overlayIOPS, overlayYAML, err = unmarshalAndValidateIOP(iopYAML, force)
			if err != nil {
				return "", nil, err
			}
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
		baseIopYAML, translateErr := translate.ICPToIOPVer(baseCRYAML, binversion.OperatorBinaryVersion)
		if translateErr != nil {
			return "", nil, fmt.Errorf("could not unmarshal or translate base yaml into IOP with profile %s at version %s: %s, %s",
				profile, binversion.OperatorBinaryVersion, err, translateErr)
		}
		_, overlayYAML, err = unmarshalAndValidateIOP(baseIopYAML, force)
		if err != nil {
			return "", nil, err
		}
	}

	// Due to the fact that base profile is compiled in before a tag can be created, we must allow an additional
	// override from variables that are set during release build time.
	hub := pkgversion.DockerInfo.Hub
	tag := pkgversion.DockerInfo.Tag
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

	if kubeConfig != nil {
		kubeOverrides, err := getClusterSpecificValues(kubeConfig, force, l)
		if err != nil {
			return "", nil, err
		}
		scope.Infof("Applying Cluster specific settings: %v", kubeOverrides)
		baseYAML, err = util.OverlayYAML(baseYAML, kubeOverrides)
		if err != nil {
			return "", nil, err
		}
	}
	overlayYAML, err = translate.OverlayYAMLTree(overlayYAML, overlayYAML, name.LegacyAddonComponentPathMap)
	if err != nil {
		return "", nil, fmt.Errorf("error translating addon components enablement from values of overlay files: %v", err)
	}
	// Merge base and overlay.
	mergedYAML, err := util.OverlayYAML(baseYAML, overlayYAML)
	if err != nil {
		return "", nil, fmt.Errorf("could not overlay user config over base: %s", err)
	}
	if _, err := unmarshalAndValidateIOPS(mergedYAML, force, l); err != nil {
		return "", nil, err
	}

	setOverlayYAML, err = translate.OverlayYAMLTree(setOverlayYAML, setOverlayYAML, name.LegacyAddonComponentPathMap)
	if err != nil {
		return "", nil, fmt.Errorf("error translating addon components enablement from values of set overlay: %v", err)
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

func getClusterSpecificValues(config *rest.Config, force bool, l *Logger) (string, error) {
	overlays := []string{}

	jwt, err := getJwtTypeOverlay(config, l)
	if err != nil {
		if force {
			l.logAndPrint(err)
		} else {
			return "", err
		}
	} else {
		overlays = append(overlays, jwt)
	}

	return MakeTreeFromSetList(overlays, false, l)

}

func getJwtTypeOverlay(config *rest.Config, l *Logger) (string, error) {
	d, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return "", fmt.Errorf("failed to determine JWT policy support. Use the --force flag to ignore this: %v", err)
	}
	_, s, err := d.ServerGroupsAndResources()
	if err != nil {
		return "", fmt.Errorf("failed to determine JWT policy support. Use the --force flag to ignore this: %v", err)
	}
	for _, res := range s {
		for _, api := range res.APIResources {
			// Appearance of this API indicates we do support third party jwt token
			if api.Name == "serviceaccounts/token" {
				return "values.global.jwtPolicy=third-party-jwt", nil
			}
		}
	}
	// TODO link to istio.io doc on how to secure this
	l.logAndPrint("Detected that your cluster does not support third party JWT authentication. Falling back to less secure first party JWT")
	return "values.global.jwtPolicy=first-party-jwt", nil
}

func genProfile(helmValues bool, inFilename []string, profile, setOverlayYAML,
	configPath string, force bool, kubeConfig *rest.Config, l *Logger) (string, error) {
	finalYAML, finalIOPS, err := genIOPS(inFilename, profile, setOverlayYAML, "", force, kubeConfig, l)
	if err != nil {
		return "", err
	}

	t, err := translate.NewTranslator(binversion.OperatorBinaryVersion.MinorVersion)
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
