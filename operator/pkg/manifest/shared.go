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

package manifest

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/yaml"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/apis/istio"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1/validation"
	"istio.io/istio/operator/pkg/controlplane"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/validate"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
	pkgversion "istio.io/istio/pkg/version"
)

// installerScope is the scope for shared manifest package.
var installerScope = log.RegisterScope("installer", "installer")

// GenManifests generates a manifest map, keyed by the component name, from input file list and a YAML tree
// representation of path-values passed through the --set flag.
// If force is set, validation errors will not cause processing to abort but will result in warnings going to the
// supplied logger.
func GenManifests(inFilename []string, setFlags []string, force bool, filter []string,
	client kube.Client, l clog.Logger,
) (name.ManifestMap, *iopv1alpha1.IstioOperator, error) {
	mergedYAML, _, err := GenerateConfig(inFilename, setFlags, force, client, l)
	if err != nil {
		return nil, nil, err
	}
	mergedIOPS, err := unmarshalAndValidateIOP(mergedYAML, force, false, l)
	if err != nil {
		return nil, nil, err
	}

	t := translate.NewTranslator()
	var ver *version.Info
	if client != nil {
		ver, err = client.GetKubernetesVersion()
		if err != nil {
			return nil, nil, err
		}
	}
	cp, err := controlplane.NewIstioControlPlane(mergedIOPS.Spec, t, filter, ver)
	if err != nil {
		return nil, nil, err
	}
	if err := cp.Run(); err != nil {
		return nil, nil, err
	}

	manifests, errs := cp.RenderManifest()
	if errs != nil {
		return manifests, mergedIOPS, errs.ToError()
	}
	return manifests, mergedIOPS, nil
}

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
func GenerateConfig(inFilenames []string, setFlags []string, force bool, client kube.Client,
	l clog.Logger,
) (string, *iopv1alpha1.IstioOperator, error) {
	if err := validateSetFlags(setFlags); err != nil {
		return "", nil, err
	}

	fy, profile, err := ReadYamlProfile(inFilenames, setFlags, force, l)
	if err != nil {
		return "", nil, err
	}

	return OverlayYAMLStrings(profile, fy, setFlags, force, client, l)
}

func OverlayYAMLStrings(profile string, fy string,
	setFlags []string, force bool, client kube.Client, l clog.Logger,
) (string, *iopv1alpha1.IstioOperator, error) {
	iopsString, iops, err := GenIOPFromProfile(profile, fy, setFlags, force, false, client, l)
	if err != nil {
		return "", nil, err
	}

	errs, warning := validation.ValidateConfig(false, iops.Spec)
	if warning != "" {
		l.LogAndError(warning)
	}

	if errs.ToError() != nil {
		return "", nil, fmt.Errorf("generated config failed semantic validation: %v", errs)
	}
	return iopsString, iops, nil
}

// GenIOPFromProfile generates an IstioOperator from the given profile name or path, and overlay YAMLs from user
// files and the --set flag. If successful, it returns an IstioOperator string and struct.
func GenIOPFromProfile(profileOrPath, fileOverlayYAML string, setFlags []string, skipValidation, allowUnknownField bool,
	client kube.Client, l clog.Logger,
) (string, *iopv1alpha1.IstioOperator, error) {
	installPackagePath, err := getInstallPackagePath(fileOverlayYAML)
	if err != nil {
		return "", nil, err
	}
	if sfp := GetValueForSetFlag(setFlags, "installPackagePath"); sfp != "" {
		// set flag installPackagePath has the highest precedence, if set.
		installPackagePath = sfp
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
	if client != nil {
		kubeOverrides, err := getClusterSpecificValues(client)
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
	t := translate.NewReverseTranslator()
	overlayYAML, err = t.TranslateK8SfromValueToIOP(overlayYAML)
	if err != nil {
		return "", nil, fmt.Errorf("could not overlay k8s settings from values to IOP: %s", err)
	}

	// Merge user file and --set flags.
	outYAML, err = util.OverlayIOP(outYAML, overlayYAML)
	if err != nil {
		return "", nil, fmt.Errorf("could not overlay user config over base: %s", err)
	}

	// If enablement came from user values overlay (file or --set), translate into addonComponents paths and overlay that.
	outYAML, err = translate.OverlayValuesEnablement(outYAML, overlayYAML, overlayYAML)
	if err != nil {
		return "", nil, err
	}

	// convertDefaultIOPMapValues converts default paths values into string, prevent errors when unmarshalling.
	outYAML, err = convertDefaultIOPMapValues(outYAML, setFlags)
	if err != nil {
		return "", nil, err
	}

	finalIOP, err := unmarshalAndValidateIOP(outYAML, skipValidation, allowUnknownField, l)
	if err != nil {
		return "", nil, err
	}

	// Validate Final IOP config against K8s cluster
	if client != nil {
		err = util.ValidateIOPCAConfig(client, finalIOP)
		if err != nil {
			return "", nil, err
		}
	}
	// InstallPackagePath may have been a URL, change to extracted to local file path.
	finalIOP.Spec.InstallPackagePath = installPackagePath
	if ns := GetValueForSetFlag(setFlags, "values.global.istioNamespace"); ns != "" {
		finalIOP.Namespace = ns
	}
	if finalIOP.Spec.Profile == "" {
		finalIOP.Spec.Profile = name.DefaultProfileName
	}
	return util.MustToYAMLGeneric(finalIOP), finalIOP, nil
}

// ReadYamlProfile gets the overlay yaml file from list of files and return profile value from file overlay and set overlay.
func ReadYamlProfile(inFilenames []string, setFlags []string, force bool, l clog.Logger) (string, string, error) {
	profile := name.DefaultProfileName
	// Get the overlay YAML from the list of files passed in. Also get the profile from the overlay files.
	fy, fp, err := ParseYAMLFiles(inFilenames, force, l)
	if err != nil {
		return "", "", err
	}
	if fp != "" {
		profile = fp
	}
	// The profile coming from --set flag has the highest precedence.
	psf := GetValueForSetFlag(setFlags, "profile")
	if psf != "" {
		profile = psf
	}
	return fy, profile, nil
}

// ParseYAMLFiles parses the given slice of filenames containing YAML and merges them into a single IstioOperator
// format YAML strings. It returns the overlay YAML, the profile name and error result.
func ParseYAMLFiles(inFilenames []string, force bool, l clog.Logger) (overlayYAML string, profile string, err error) {
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
		profile = fileOverlayIOP.Spec.Profile
	}
	return y, profile, nil
}

func ReadLayeredYAMLs(filenames []string) (string, error) {
	return readLayeredYAMLs(filenames, os.Stdin)
}

func readLayeredYAMLs(filenames []string, stdinReader io.Reader) (string, error) {
	var ly string
	var stdin bool
	for _, fn := range filenames {
		var b []byte
		var err error
		if fn == "-" {
			if stdin {
				continue
			}
			stdin = true
			b, err = io.ReadAll(stdinReader)
		} else {
			b, err = os.ReadFile(strings.TrimSpace(fn))
		}
		if err != nil {
			return "", err
		}
		multiple := false
		multiple, err = hasMultipleIOPs(string(b))
		if err != nil {
			return "", err
		}
		if multiple {
			return "", fmt.Errorf("input file %s contains multiple IstioOperator CRs, only one per file is supported", fn)
		}
		ly, err = util.OverlayIOP(ly, string(b))
		if err != nil {
			return "", err
		}
	}
	return ly, nil
}

func hasMultipleIOPs(s string) (bool, error) {
	objs, err := object.ParseK8sObjectsFromYAMLManifest(s)
	if err != nil {
		return false, err
	}
	found := false
	for _, o := range objs {
		if o.Kind == name.IstioOperator {
			if found {
				return true, nil
			}
			found = true
		}
	}
	return false, nil
}

func GetProfile(iop *iopv1alpha1.IstioOperator) string {
	profile := "default"
	if iop != nil && iop.Spec != nil && iop.Spec.Profile != "" {
		profile = iop.Spec.Profile
	}
	return profile
}

func GetMergedIOP(userIOPStr, profile, manifestsPath, revision string, client kube.Client,
	logger clog.Logger,
) (*iopv1alpha1.IstioOperator, error) {
	extraFlags := make([]string, 0)
	if manifestsPath != "" {
		extraFlags = append(extraFlags, fmt.Sprintf("installPackagePath=%s", manifestsPath))
	}
	if revision != "" {
		extraFlags = append(extraFlags, fmt.Sprintf("revision=%s", revision))
	}
	_, mergedIOP, err := OverlayYAMLStrings(profile, userIOPStr, extraFlags, false, client, logger)
	if err != nil {
		return nil, err
	}
	return mergedIOP, nil
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

func getClusterSpecificValues(client kube.Client) (string, error) {
	overlays := []string{}

	cni := getCNISettings(client)
	if cni != "" {
		overlays = append(overlays, cni)
	}
	return makeTreeFromSetList(overlays)
}

// getCNISettings gets auto-detected values based on the Kubernetes environment.
// Note: there are other settings as well; however, these are detected inline in the helm chart.
// This ensures helm users also get them.
func getCNISettings(client kube.Client) string {
	ver, err := client.GetKubernetesVersion()
	if err != nil {
		return ""
	}
	// https://istio.io/latest/docs/setup/additional-setup/cni/#hosted-kubernetes-settings
	// GKE requires deployment in kube-system namespace.
	if strings.Contains(ver.GitVersion, "-gke") {
		return "components.cni.namespace=kube-system"
	}
	// TODO: OpenShift
	return ""
}

// makeTreeFromSetList creates a YAML tree from a string slice containing key-value pairs in the format key=value.
func makeTreeFromSetList(setOverlay []string) (string, error) {
	if len(setOverlay) == 0 {
		return "", nil
	}
	tree := make(map[string]any)
	for _, kv := range setOverlay {
		kvv := strings.Split(kv, "=")
		if len(kvv) != 2 {
			return "", fmt.Errorf("bad argument %s: expect format key=value", kv)
		}
		k := kvv[0]
		v := util.ParseValue(kvv[1])
		if err := tpath.WriteNode(tree, util.PathFromString(k), v); err != nil {
			return "", err
		}
		// To make errors more user friendly, test the path and error out immediately if we cannot unmarshal.
		testTree, err := yaml.Marshal(tree)
		if err != nil {
			return "", err
		}
		iops := &v1alpha1.IstioOperatorSpec{}
		if err := util.UnmarshalWithJSONPB(string(testTree), iops, false); err != nil {
			return "", fmt.Errorf("bad path=value %s: %v", kv, err)
		}
	}
	out, err := yaml.Marshal(tree)
	if err != nil {
		return "", err
	}
	return tpath.AddSpecRoot(string(out))
}

// unmarshalAndValidateIOP unmarshals a string containing IstioOperator YAML, validates it, and returns a struct
// representation if successful. If force is set, validation errors are written to logger rather than causing an
// error.
func unmarshalAndValidateIOP(iopsYAML string, force, allowUnknownField bool, l clog.Logger) (*iopv1alpha1.IstioOperator, error) {
	iop, err := istio.UnmarshalIstioOperator(iopsYAML, allowUnknownField)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal merged YAML: %s\n\nYAML:\n%s", err, iopsYAML)
	}
	if errs := validate.CheckIstioOperatorSpec(iop.Spec, true); len(errs) != 0 && !force {
		l.LogAndError("Run the command with the --force flag if you want to ignore the validation error and proceed.")
		return iop, fmt.Errorf(errs.Error())
	}
	return iop, nil
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

// alwaysString represents types that should always be decoded as strings
// TODO: this could be automatically derived from the value_types.proto?
var alwaysString = sets.New("values.compatibilityVersion", "compatibilityVersion")

// overlaySetFlagValues overlays each of the setFlags on top of the passed in IOP YAML string.
func overlaySetFlagValues(iopYAML string, setFlags []string) (string, error) {
	iop := make(map[string]any)
	if err := yaml.Unmarshal([]byte(iopYAML), &iop); err != nil {
		return "", err
	}
	// Unmarshal returns nil for empty manifests but we need something to insert into.
	if iop == nil {
		iop = make(map[string]any)
	}

	for _, sf := range setFlags {
		p, v := getPV(sf)
		p = strings.TrimPrefix(p, "spec.")
		inc, _, err := tpath.GetPathContext(iop, util.PathFromString("spec."+p), true)
		if err != nil {
			return "", err
		}
		// input value type is always string, transform it to correct type before setting.
		var val any = v
		if !alwaysString.Contains(p) {
			val = util.ParseValue(v)
		}
		if err := tpath.WritePathContext(inc, val, false); err != nil {
			return "", err
		}
	}

	out, err := yaml.Marshal(iop)
	if err != nil {
		return "", err
	}

	return string(out), nil
}

var defaultSetFlagConvertPaths = []string{
	"meshConfig.defaultConfig.proxyMetadata",
}

// convertDefaultIOPMapValues converts default map[string]string values into string.
func convertDefaultIOPMapValues(outYAML string, setFlags []string) (string, error) {
	return convertIOPMapValues(outYAML, setFlags, defaultSetFlagConvertPaths)
}

// convertIOPMapValues converts certain paths of map[string]string values into string.
func convertIOPMapValues(outYAML string, setFlags []string, convertPaths []string) (string, error) {
	for _, setFlagConvertPath := range convertPaths {
		if containParentPath(setFlags, setFlagConvertPath) {
			var (
				converter              = map[string]interface{}{}
				convertedProxyMetadata = map[string]string{}
				subPaths               = strings.Split(setFlagConvertPath, ".")
			)

			if err := yaml.Unmarshal([]byte(outYAML), &converter); err != nil {
				return outYAML, err
			}
			originMap, ok := converter["spec"].(map[string]any)
			if !ok {
				return outYAML, nil
			}

			for index, subPath := range subPaths {
				if _, ok := originMap[subPath].(map[string]any); !ok {
					return outYAML, fmt.Errorf("can not convert subPath %s in setFlag path %s",
						subPath, setFlagConvertPath)
				}

				if index == len(subPaths)-1 {
					for key, value := range originMap[subPath].(map[string]any) {
						if reflect.TypeOf(value).Kind() == reflect.Int {
							convertedProxyMetadata[key] = strconv.FormatInt(value.(int64), 10)
						}
						if reflect.TypeOf(value).Kind() == reflect.Bool {
							convertedProxyMetadata[key] = strconv.FormatBool(value.(bool))
						}
						if reflect.TypeOf(value).Kind() == reflect.Float64 {
							convertedProxyMetadata[key] = fmt.Sprint(value)
						}
						if reflect.TypeOf(value).Kind() == reflect.String {
							convertedProxyMetadata[key] = value.(string)
						}
					}
					originMap[subPath] = convertedProxyMetadata
				} else {
					originMap = originMap[subPath].(map[string]any)
				}
			}

			convertedYaml, err := yaml.Marshal(converter)
			if err != nil {
				return outYAML, err
			}
			return string(convertedYaml), nil
		}
	}

	return outYAML, nil
}

// containParentPath checks if setFlags contain parent path.
func containParentPath(setFlags []string, parentPath string) bool {
	ret := false
	for _, sf := range setFlags {
		p, _ := getPV(sf)
		if strings.Contains(p, parentPath) {
			ret = true
			break
		}
	}
	return ret
}

// GetValueForSetFlag parses the passed set flags which have format key=value and if any set the given path,
// returns the corresponding value, otherwise returns the empty string. setFlags must have valid format.
func GetValueForSetFlag(setFlags []string, path string) string {
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
