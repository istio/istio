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
	"strings"

	"istio.io/istio/operator/version"

	"istio.io/istio/operator/pkg/translate"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	"istio.io/istio/operator/pkg/name"

	"istio.io/api/operator/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/validate"
	"istio.io/pkg/log"
	pkgversion "istio.io/pkg/version"
)

var scope = log.RegisterScope("installer", "installer", 0)

// GenerateConfig creates an IstioOperatorSpec from the following sources, overlaid sequentially:
// 1. Compiled in base, or optionally base from paths pointing to one or multiple ICP/IOP files at inFilenames.
// 2. Profile overlay, if non-default overlay is selected. This also comes either from compiled in or path specified in IOP contained in inFilenames.
// 3. User overlays stored in inFilenames.
// 4. setOverlayYAML, which comes from --set flag passed to manifest command.
//
// Note that the user overlay at inFilenames can optionally contain a file path to a set of profiles different from the
// ones that are compiled in. If it does, the starting point will be the base and profile YAMLs at that file path.
// Otherwise it will be the compiled in profile YAMLs.
// In step 3, the remaining fields in the same user overlay are applied on the resulting profile base.
// The force flag causes validation errors not to abort but only emit log/console warnings.
func GenerateConfig(inFilenames []string, setOverlayYAML string, force bool, kubeConfig *rest.Config, l *Logger) (string, *v1alpha1.IstioOperatorSpec, error) {
	profile := name.DefaultProfileName

	// Get the overlay YAML from the list of files passed in. Also get the profile from the overlay files.
	fy, fp, err := parseYAMLFiles(inFilenames, force, l)
	if err != nil {
		return "", nil, err
	}
	if fp != "" {
		profile = fp
	}

	// The profile coming from --set flag has the highest precedence.
	psf := profileFromSetOverlay(setOverlayYAML)
	if psf != "" {
		profile = psf
	}

	return genIOPSFromProfile(profile, fy, setOverlayYAML, force, kubeConfig, l)
}

// parseYAMLFiles parses the given slice of filenames containing YAML and merges them into a single IstioOperator
// format YAML strings. It returns the overlay YAML, the profile name and error result.
func parseYAMLFiles(inFilenames []string, force bool, l *Logger) (overlayYAML string, profile string, err error) {
	if inFilenames == nil {
		return "", "", nil
	}
	y, err := ReadLayeredYAMLs(inFilenames)
	if err != nil {
		return "", "", err
	}
	var fileOverlayIOP *iopv1alpha1.IstioOperator
	fileOverlayIOP, overlayYAML, err = translate.UnmarshalIOPOrICP(y)
	if err != nil {
		return "", "", err
	}
	if err := validate.ValidIOP(fileOverlayIOP); err != nil {
		if !force {
			return "", "", fmt.Errorf("validation errors (use --force to override): \n%s", err)
		}
		l.logAndErrorf("Validation errors (continuing because of --force):\n%s", err)
	}
	if fileOverlayIOP.Spec.Profile != "" {
		if profile != "" && profile != fileOverlayIOP.Spec.Profile {
			return "", "", fmt.Errorf("different profiles cannot be overlaid")
		}
		profile = fileOverlayIOP.Spec.Profile
	}
	return overlayYAML, profile, nil
}

// profileFromSetOverlay takes a YAML string and if it contains a key called "profile" in the root, it returns the key
// value.
func profileFromSetOverlay(yml string) string {
	s, err := tpath.GetConfigSubtree(yml, "spec.profile")
	if err != nil {
		return ""
	}
	s = strings.TrimSpace(s)
	if s == "{}" {
		return ""
	}
	return s
}

// genIOPSFromProfile generates an IstioOperatorSpec from the given profile name or path, and overlay YAMLs from user
// files and the --set flag. If successful, it returns an IstioOperatorSpec string and struct.
func genIOPSFromProfile(profileOrPath, fileOverlayYAML, setOverlayYAML string, skipValidation bool,
	kubeConfig *rest.Config, l *Logger) (string, *v1alpha1.IstioOperatorSpec, error) {
	userOverlayYAML, err := util.OverlayYAML(fileOverlayYAML, setOverlayYAML)
	if err != nil {
		return "", nil, fmt.Errorf("could not merge file and --set YAMLs: %s", err)
	}
	installPackagePath, err := getInstallPackagePath(userOverlayYAML)
	if err != nil {
		return "", nil, err
	}

	// If installPackagePath is a URL, fetch and extract it and continue with the local filesystem path instead.
	installPackagePath, profileOrPath, err = rewriteURLToLocalInstallPath(installPackagePath, profileOrPath, skipValidation)
	if err != nil {
		return "", nil, err
	}

	// To generate the base profileOrPath for overlaying with user values, we need the installPackagePath where the profiles
	// can be found, and the selected profileOrPath. Both of these can come from either the user overlay file or --set flag.
	outYAML, err := getProfileYAML(profileOrPath)
	if err != nil {
		return "", nil, err
	}

	// Hub and tag are only known at build time and must be passed in here during runtime from build stamps.
	outYAML, err = overlayHubAndTag(outYAML)
	if err != nil {
		return "", nil, err
	}

	// Merge k8s specific values.
	if kubeConfig != nil {
		kubeOverrides, err := getClusterSpecificValues(kubeConfig, skipValidation, l)
		if err != nil {
			return "", nil, err
		}
		scope.Infof("Applying Cluster specific settings: %v", kubeOverrides)
		outYAML, err = util.OverlayYAML(outYAML, kubeOverrides)
		if err != nil {
			return "", nil, err
		}
	}

	// Merge user file and --set overlays.
	outYAML, err = util.OverlayYAML(outYAML, userOverlayYAML)
	if err != nil {
		return "", nil, fmt.Errorf("could not overlay user config over base: %s", err)
	}

	// If enablement came from user values overlay (file or --set), translate into addonComponents paths and overlay that.
	outYAML, err = overlayValuesEnablement(outYAML, fileOverlayYAML, setOverlayYAML)
	if err != nil {
		return "", nil, err
	}

	// Grab just the IstioOperatorSpec subtree.
	outYAML, err = tpath.GetSpecSubtree(outYAML)
	if err != nil {
		return "", nil, err
	}

	finalIOPS, err := unmarshalAndValidateIOPS(outYAML, skipValidation, l)
	if err != nil {
		return "", nil, err
	}
	// InstallPackagePath may have been a URL, change to extracted to local file path.
	finalIOPS.InstallPackagePath = installPackagePath
	return util.ToYAMLWithJSONPB(finalIOPS), finalIOPS, nil
}

// rewriteURLToLocalInstallPath checks installPackagePath and if it is a URL, it tries to download and extract the
// Istio release tar at the URL to a local file path. If successful, it returns the resulting local paths to the
// installation charts and profile file.
// If installPackagePath is not a URL, it returns installPackagePath and profileOrPath unmodified.
func rewriteURLToLocalInstallPath(installPackagePath, profileOrPath string, skipValidation bool) (string, string, error) {
	var err error
	if util.IsHTTPURL(installPackagePath) {
		if !skipValidation {
			_, ver, err := helm.URLToDirname(installPackagePath)
			if err != nil {
				return "", "", err
			}
			if ver.Minor != version.OperatorBinaryVersion.Minor {
				return "", "", fmt.Errorf("chart minor version %s doesn't match istioctl version %s, use --force to override", ver, version.OperatorCodeBaseVersion)
			}
		}

		installPackagePath, err = fetchExtractInstallPackageHTTP(installPackagePath)
		if err != nil {
			return "", "", err
		}
		// Transform a profileOrPath like "default" or "demo" into a filesystem path like
		// /tmp/istio-install-packages/istio-1.5.1/install/kubernetes/operator/profiles/default.yaml.
		profileOrPath = filepath.Join(installPackagePath, helm.OperatorSubdirFilePath, "profiles", profileOrPath+".yaml")
		// Rewrite installPackagePath to the local file path for further processing.
		installPackagePath = filepath.Join(installPackagePath, helm.OperatorSubdirFilePath, "charts")
	}

	return installPackagePath, profileOrPath, nil
}

// getProfileYAML returns the YAML for the given profile name, using the given profileOrPath string, which may be either
// a profile label or a file path.
func getProfileYAML(profileOrPath string) (string, error) {
	// This contains the IstioOperator CR.
	baseCRYAML, err := helm.ReadProfileYAML(profileOrPath)
	if err != nil {
		return "", err
	}

	if !helm.IsDefaultProfile(profileOrPath) {
		// Profile definitions are relative to the default profileOrPath, so read that first.
		dfn, err := helm.DefaultFilenameForProfile(profileOrPath)
		if err != nil {
			return "", err
		}
		defaultYAML, err := helm.ReadProfileYAML(dfn)
		if err != nil {
			return "", err
		}
		baseCRYAML, err = util.OverlayYAML(defaultYAML, baseCRYAML)
		if err != nil {
			return "", err
		}
	}

	return baseCRYAML, nil
}

// Due to the fact that base profile is compiled in before a tag can be created, we must allow an additional
// override from variables that are set during release build time.
func overlayHubAndTag(yml string) (string, error) {
	hub := pkgversion.DockerInfo.Hub
	tag := pkgversion.DockerInfo.Tag
	out := yml
	if hub != "unknown" && tag != "unknown" {
		buildHubTagOverlayYAML, err := helm.GenerateHubTagOverlay(hub, tag)
		if err != nil {
			return "", err
		}
		out, err = util.OverlayYAML(yml, buildHubTagOverlayYAML)
		if err != nil {
			return "", err
		}
	}
	return out, nil
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

	return makeTreeFromSetList(overlays)

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

// overlayValuesEnablement overlays any enablement in values path from the user file overlay or set flag overlay.
// The overlay is translated from values to the corresponding addonComponents enablement paths.
func overlayValuesEnablement(baseYAML, fileOverlayYAML, setOverlayYAML string) (string, error) {
	overlayYAML, err := util.OverlayYAML(fileOverlayYAML, setOverlayYAML)
	if err != nil {
		return "", fmt.Errorf("could not overlay user config over base: %s", err)
	}

	return translate.YAMLTree(overlayYAML, baseYAML, name.ValuesEnablementPathMap)
}

// unmarshalAndValidateIOPS unmarshals a string containing IstioOperator YAML, validates it, and returns a struct
// representation if successful. If force is set, validation errors are written to logger rather than causing an
// error.
func unmarshalAndValidateIOPS(iopsYAML string, force bool, l *Logger) (*v1alpha1.IstioOperatorSpec, error) {
	iops := &v1alpha1.IstioOperatorSpec{}
	if err := util.UnmarshalWithJSONPB(iopsYAML, iops, false); err != nil {
		return nil, fmt.Errorf("could not unmarshal the merged YAML: %s\n\nYAML:\n%s", err, iopsYAML)
	}
	if errs := validate.CheckIstioOperatorSpec(iops, true); len(errs) != 0 {
		if !force {
			l.logAndError("Run the command with the --force flag if you want to ignore the validation error and proceed.")
			return nil, fmt.Errorf(errs.Error())
		}
	}
	return iops, nil
}

// getInstallPackagePath returns the installPackagePath in the given IstioOperator YAML string.
func getInstallPackagePath(iopYAML string) (string, error) {
	iop, err := validate.UnmarshalIOP(iopYAML)
	if err != nil {
		return "", err
	}
	if iop.Spec == nil {
		return "", nil
	}
	return iop.Spec.InstallPackagePath, nil
}
