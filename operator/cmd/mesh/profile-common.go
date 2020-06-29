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
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
	"k8s.io/client-go/rest"

	"istio.io/api/operator/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1/validation"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/validate"
	"istio.io/istio/operator/version"
	pkgversion "istio.io/pkg/version"
)

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
func GenerateConfig(inFilenames []string, setFlags []string, force bool, kubeConfig *rest.Config,
	l clog.Logger) (string, *v1alpha1.IstioOperatorSpec, error) {
	if err := validateSetFlags(setFlags); err != nil {
		return "", nil, err
	}

	fy, profile, err := readYamlProfile(inFilenames, setFlags, force, l)
	if err != nil {
		return "", nil, err
	}

	iopsString, iops, err := genIOPSFromProfile(profile, fy, setFlags, force, kubeConfig, l)
	if err != nil {
		return "", nil, err
	}

	errs, warning := validation.ValidateConfig(false, iops.Values, iops)
	if warning != "" {
		l.LogAndError(warning)
	}

	if errs.ToError() != nil {
		return "", nil, fmt.Errorf("generated config failed semantic validation: %v", errs)
	}
	return iopsString, iops, nil
}

func readYamlProfile(inFilenames []string, setFlags []string, force bool, l clog.Logger) (string, string, error) {
	profile := name.DefaultProfileName
	// Get the overlay YAML from the list of files passed in. Also get the profile from the overlay files.
	fy, fp, err := parseYAMLFiles(inFilenames, force, l)
	if err != nil {
		return "", "", err
	}
	if fp != "" {
		profile = fp
	}
	// The profile coming from --set flag has the highest precedence.
	psf := getValueForSetFlag(setFlags, "profile")
	if psf != "" {
		profile = psf
	}
	return fy, profile, nil
}

// parseYAMLFiles parses the given slice of filenames containing YAML and merges them into a single IstioOperator
// format YAML strings. It returns the overlay YAML, the profile name and error result.
func parseYAMLFiles(inFilenames []string, force bool, l clog.Logger) (overlayYAML string, profile string, err error) {
	if inFilenames == nil {
		return "", "", nil
	}
	y, err := ReadLayeredYAMLs(inFilenames)
	if err != nil {
		return "", "", err
	}
	var fileOverlayIOP *iopv1alpha1.IstioOperator
	fileOverlayIOP, err = validate.UnmarshalIOP(y)
	if err != nil {
		return "", "", err
	}
	if err := validate.ValidIOP(fileOverlayIOP); err != nil {
		if !force {
			return "", "", fmt.Errorf("validation errors (use --force to override): \n%s", err)
		}
		l.LogAndErrorf("Validation errors (continuing because of --force):\n%s", err)
	}
	if fileOverlayIOP.Spec != nil && fileOverlayIOP.Spec.Profile != "" {
		if profile != "" && profile != fileOverlayIOP.Spec.Profile {
			return "", "", fmt.Errorf("different profiles cannot be overlaid")
		}
		profile = fileOverlayIOP.Spec.Profile
	}
	return y, profile, nil
}

// genIOPSFromProfile generates an IstioOperatorSpec from the given profile name or path, and overlay YAMLs from user
// files and the --set flag. If successful, it returns an IstioOperatorSpec string and struct.
func genIOPSFromProfile(profileOrPath, fileOverlayYAML string, setFlags []string, skipValidation bool,
	kubeConfig *rest.Config, l clog.Logger) (string, *v1alpha1.IstioOperatorSpec, error) {

	installPackagePath, err := getInstallPackagePath(fileOverlayYAML)
	if err != nil {
		return "", nil, err
	}
	if sfp := getValueForSetFlag(setFlags, "installPackagePath"); sfp != "" {
		// set flag installPackagePath has the highest precedence, if set.
		installPackagePath = sfp
	}

	// If installPackagePath is a URL, fetch and extract it and continue with the local filesystem path instead.
	installPackagePath, profileOrPath, err = rewriteURLToLocalInstallPath(installPackagePath, profileOrPath, skipValidation)
	if err != nil {
		return "", nil, err
	}

	// To generate the base profileOrPath for overlaying with user values, we need the installPackagePath where the profiles
	// can be found, and the selected profileOrPath. Both of these can come from either the user overlay file or --set flag.
	outYAML, err := helm.GetProfileYAML(installPackagePath, profileOrPath)
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
		installerScope.Infof("Applying Cluster specific settings: %v", kubeOverrides)
		outYAML, err = util.OverlayYAML(outYAML, kubeOverrides)
		if err != nil {
			return "", nil, err
		}
	}

	// Combine file and --set overlays and translate any K8s settings in values to IOP format. Users should not set
	// these but we have to support this path until it's deprecated.
	overlayYAML, err := overlaySetFlagValues(fileOverlayYAML, setFlags)
	if err != nil {
		return "", nil, err
	}
	mvs := version.OperatorBinaryVersion.MinorVersion
	t, err := translate.NewReverseTranslator(mvs)
	if err != nil {
		return "", nil, fmt.Errorf("error creating values.yaml translator: %s", err)
	}
	overlayYAML, err = t.TranslateK8SfromValueToIOP(overlayYAML)
	if err != nil {
		return "", nil, fmt.Errorf("could not overlay k8s settings from values to IOP: %s", err)
	}

	// Merge user file and --set flags.
	outYAML, err = util.OverlayYAML(outYAML, overlayYAML)
	if err != nil {
		return "", nil, fmt.Errorf("could not overlay user config over base: %s", err)
	}

	if err := name.ScanBundledAddonComponents(installPackagePath); err != nil {
		return "", nil, err
	}
	// If enablement came from user values overlay (file or --set), translate into addonComponents paths and overlay that.
	outYAML, err = translate.OverlayValuesEnablement(outYAML, overlayYAML, overlayYAML)
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
	isURL, err := util.IsHTTPURL(installPackagePath)
	if err != nil && !skipValidation {
		return "", "", err
	}
	if isURL {
		installPackagePath, err = fetchExtractInstallPackageHTTP(installPackagePath)
		if err != nil {
			return "", "", err
		}
		// Transform a profileOrPath like "default" or "demo" into a filesystem path like
		// /tmp/istio-install-packages/istio-1.5.1/manifests/profiles/default.yaml OR
		// /tmp/istio-install-packages/istio-1.5.1/install/kubernetes/operator/profiles/default.yaml (before 1.6).
		baseDir := filepath.Join(installPackagePath, helm.OperatorSubdirFilePath15)
		if _, err := os.Stat(baseDir); os.IsNotExist(err) {
			baseDir = filepath.Join(installPackagePath, helm.OperatorSubdirFilePath)
		}
		profileOrPath = filepath.Join(baseDir, "profiles", profileOrPath+".yaml")
		// Rewrite installPackagePath to the local file path for further processing.
		installPackagePath = baseDir
	}

	return installPackagePath, profileOrPath, nil
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

func getClusterSpecificValues(config *rest.Config, force bool, l clog.Logger) (string, error) {
	overlays := []string{}

	jwt, err := getJwtTypeOverlay(config, l)
	if err != nil {
		if force {
			l.LogAndPrint(err)
		} else {
			return "", err
		}
	} else {
		overlays = append(overlays, jwt)
	}

	return makeTreeFromSetList(overlays)

}

func getJwtTypeOverlay(config *rest.Config, l clog.Logger) (string, error) {
	jwtPolicy, err := util.DetectSupportedJWTPolicy(config)
	if err != nil {
		return "", fmt.Errorf("failed to determine JWT policy support. Use the --force flag to ignore this: %v", err)
	}
	if jwtPolicy == util.FirstPartyJWT {
		// nolint: lll
		l.LogAndPrint("Detected that your cluster does not support third party JWT authentication. " +
			"Falling back to less secure first party JWT. See https://istio.io/docs/ops/best-practices/security/#configure-third-party-service-account-tokens for details.")
	}
	return "values.global.jwtPolicy=" + string(jwtPolicy), nil
}

// unmarshalAndValidateIOPS unmarshals a string containing IstioOperator YAML, validates it, and returns a struct
// representation if successful. If force is set, validation errors are written to logger rather than causing an
// error.
func unmarshalAndValidateIOPS(iopsYAML string, force bool, l clog.Logger) (*v1alpha1.IstioOperatorSpec, error) {
	iops := &v1alpha1.IstioOperatorSpec{}
	if err := util.UnmarshalWithJSONPB(iopsYAML, iops, false); err != nil {
		return nil, fmt.Errorf("could not unmarshal merged YAML: %s\n\nYAML:\n%s", err, iopsYAML)
	}
	if errs := validate.CheckIstioOperatorSpec(iops, true); len(errs) != 0 && !force {
		l.LogAndError("Run the command with the --force flag if you want to ignore the validation error and proceed.")
		return iops, fmt.Errorf(errs.Error())
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

// validateSetFlags validates that setFlags all have path=value format.
func validateSetFlags(setFlags []string) error {
	for _, sf := range setFlags {
		pv := strings.Split(sf, "=")
		if len(pv) != 2 {
			return fmt.Errorf("set flag %s has incorrect format, must be path=value", sf)
		}
	}
	return nil
}

// overlaySetFlagValues overlays each of the setFlags on top of the passed in IOP YAML string.
func overlaySetFlagValues(iopYAML string, setFlags []string) (string, error) {
	iop := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(iopYAML), &iop); err != nil {
		return "", err
	}
	// Unmarshal returns nil for empty manifests but we need something to insert into.
	if iop == nil {
		iop = make(map[string]interface{})
	}

	for _, sf := range setFlags {
		p, v := getPV(sf)
		p = strings.TrimPrefix(p, "spec.")
		inc, _, err := tpath.GetPathContext(iop, util.PathFromString("spec."+p), true)
		if err != nil {
			return "", err
		}
		// input value type is always string, transform it to correct type before setting.
		if err := tpath.WritePathContext(inc, util.ParseValue(v), false); err != nil {
			return "", err
		}
	}

	out, err := yaml.Marshal(iop)
	if err != nil {
		return "", err
	}

	return string(out), nil
}

// getValueForSetFlag parses the passed set flags which have format key=value and if any set the given path,
// returns the corresponding value, otherwise returns the empty string. setFlags must have valid format.
func getValueForSetFlag(setFlags []string, path string) string {
	ret := ""
	for _, sf := range setFlags {
		p, v := getPV(sf)
		if p == path {
			ret = v
		}
		// if set multiple times, return last set value
	}
	return ret
}

// getPV returns the path and value components for the given set flag string, which must be in path=value format.
func getPV(setFlag string) (path string, value string) {
	pv := strings.Split(setFlag, "=")
	if len(pv) != 2 {
		return setFlag, ""
	}
	path, value = strings.TrimSpace(pv[0]), strings.TrimSpace(pv[1])
	return
}
