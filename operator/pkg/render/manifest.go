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

package render

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/version"

	"istio.io/istio/manifests"
	"istio.io/istio/operator/pkg/apis"
	"istio.io/istio/operator/pkg/apis/validation"
	"istio.io/istio/operator/pkg/component"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/values"
	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/kube"
	pkgversion "istio.io/istio/pkg/version"
)

// GenerateManifest produces fully rendered Kubernetes objects from rendering Helm charts.
// Inputs can be files and --set strings.
// Client is option; if it is provided, cluster-specific settings can be auto-detected.
// Logger is also option; if it is provided warning messages may be logged.
func GenerateManifest(files []string, setFlags []string, force bool, client kube.Client, logger clog.Logger) ([]manifest.ManifestSet, values.Map, error) {
	// First, compute our final configuration input. This will be in the form of an IstioOperator, but as an unstructured values.Map.
	// This allows safe access to get/fetch values dynamically, and avoids issues are typing and whether we should emit empty fields.
	merged, err := MergeInputs(files, setFlags, client)
	if err != nil {
		return nil, nil, fmt.Errorf("merge inputs: %v", err)
	}
	// Validate the config. This can emit warnings to the logger. If force is set, errors will be logged as warnings but not returned.
	if err := validateIstioOperator(merged, client, logger, force); err != nil {
		return nil, nil, err
	}
	// After validation, apply any unvalidatedValues they may have set.
	if unvalidatedValues, _ := merged.GetPathMap("spec.unvalidatedValues"); unvalidatedValues != nil {
		merged.MergeFrom(values.MakeMap(unvalidatedValues, "spec", "values"))
	}
	var kubernetesVersion *version.Info
	if client != nil {
		v, err := client.GetKubernetesVersion()
		if err != nil {
			return nil, nil, fmt.Errorf("fail to get Kubernetes version: %v", err)
		}
		kubernetesVersion = v
	}

	// Render each component
	allManifests := map[component.Name]manifest.ManifestSet{}
	var chartWarnings util.Errors
	for _, comp := range component.AllComponents {
		specs, err := comp.Get(merged)
		if err != nil {
			return nil, nil, fmt.Errorf("get component %v: %v", comp.UserFacingName, err)
		}
		for _, spec := range specs {
			// Each component may get a different view of the values; modify them as needed (with a copy)
			compVals := applyComponentValuesToHelmValues(comp, spec, merged)
			// Render the chart
			rendered, warnings, err := helm.Render("istio", spec.Namespace, comp.HelmSubdir, compVals, kubernetesVersion)
			if err != nil {
				return nil, nil, fmt.Errorf("helm render: %v", err)
			}
			chartWarnings = util.AppendErrs(chartWarnings, warnings)
			// IstioOperator has a variety of processing steps that are done *after* Helm, such as patching. Apply any of these steps.
			finalized, err := postProcess(comp, spec, rendered, compVals)
			if err != nil {
				return nil, nil, fmt.Errorf("post processing: %v", err)
			}
			manifests, found := allManifests[comp.UserFacingName]
			if found {
				manifests.Manifests = append(manifests.Manifests, finalized...)
				allManifests[comp.UserFacingName] = manifests
			} else {
				allManifests[comp.UserFacingName] = manifest.ManifestSet{
					Component: comp.UserFacingName,
					Manifests: finalized,
				}
			}
		}
	}

	// Log any warnings we got from the charts
	if logger != nil {
		for _, w := range chartWarnings {
			logger.LogAndErrorf("%s %v", "❗", w)
		}
	}

	values := make([]manifest.ManifestSet, 0, len(allManifests))

	for _, v := range allManifests {
		values = append(values, v)
	}

	return values, merged, nil
}

type MigrationResult struct {
	Components []ComponentMigration
}

type ComponentMigration struct {
	Component      component.Component
	Values         values.Map
	Manifest       []manifest.Manifest
	SkippedPatches []string
	Directory      string
	ComponentSpec  apis.GatewayComponentSpec
}

// Migrate helps perform a migration from Istioctl to Helm.
// This basically runs the same steps as GenerateManifest but without any post processing.
func Migrate(files []string, setFlags []string, client kube.Client) (MigrationResult, error) {
	res := MigrationResult{}
	// First, compute our final configuration input. This will be in the form of an IstioOperator, but as an unstructured values.Map.
	// This allows safe access to get/fetch values dynamically, and avoids issues are typing and whether we should emit empty fields.
	merged, err := MergeInputs(files, setFlags, client)
	if err != nil {
		return res, fmt.Errorf("merge inputs: %v", err)
	}
	// After validation, apply any unvalidatedValues they may have set.
	if unvalidatedValues, _ := merged.GetPathMap("spec.unvalidatedValues"); unvalidatedValues != nil {
		merged.MergeFrom(values.MakeMap(unvalidatedValues, "spec", "values"))
	}
	var kubernetesVersion *version.Info
	if client != nil {
		v, err := client.GetKubernetesVersion()
		if err != nil {
			return res, fmt.Errorf("fail to get Kubernetes version: %v", err)
		}
		kubernetesVersion = v
	}

	// Render each component
	for _, comp := range component.AllComponents {
		specs, err := comp.Get(merged)
		if err != nil {
			return res, fmt.Errorf("get component %v: %v", comp.UserFacingName, err)
		}
		for _, spec := range specs {
			// Each component may get a different view of the values; modify them as needed (with a copy)
			compVals := applyComponentValuesToHelmValues(comp, spec, merged)
			// Render the chart
			rendered, _, err := helm.Render("istio", spec.Namespace, comp.HelmSubdir, compVals, kubernetesVersion)
			if err != nil {
				return res, fmt.Errorf("helm render: %v", err)
			}
			res.Components = append(res.Components, ComponentMigration{
				Values:         compVals,
				Manifest:       rendered,
				Component:      comp,
				ComponentSpec:  spec,
				SkippedPatches: nil,
			})
		}
	}

	return res, nil
}

// applyComponentValuesToHelmValues translates a generic values set into a component-specific one
func applyComponentValuesToHelmValues(comp component.Component, spec apis.GatewayComponentSpec, merged values.Map) values.Map {
	root := comp.ToHelmValuesTreeRoot

	// Gateways allow providing 'name' and 'label' overrides.
	if comp.IsGateway() {
		merged = merged.DeepClone()
		_ = merged.SetPath(fmt.Sprintf("spec.values.%s.name", root), spec.Name)
		_ = merged.SetPath(fmt.Sprintf("spec.values.%s.labels", root), spec.Label)
		if spec.Kubernetes != nil && spec.Kubernetes.Service != nil && len(spec.Kubernetes.Service.Ports) > 0 {
			b, _ := json.Marshal(spec.Kubernetes.Service.Ports)
			var ports []map[string]any
			_ = json.Unmarshal(b, &ports)
			_ = merged.SetPath(fmt.Sprintf("spec.values.%s.ports", root), ports)
		}
	}
	// No changes needed, skip early to avoid copy
	if !comp.FlattenValues && spec.Hub == "" && spec.Tag == nil && spec.Label == nil {
		return merged
	}

	// Copy to ensure we are not mutating the shared state
	merged = merged.DeepClone()
	if spec.Hub != "" {
		_ = merged.SetSpecPaths(fmt.Sprintf("values.%s.hub=%s", root, spec.Hub))
	}
	if spec.Tag != nil {
		_ = merged.SetSpecPaths(fmt.Sprintf("values.%s.tag=%v", root, spec.Tag))
	}
	// IstioOperator presents users a single set of values to apply to all charts. This works well when the chart expects all
	// values to be prefixed (like `pilot.resources=...`).
	// Other charts do not, and expect direct settings (like `resources=...`).
	// To avoid these direct settings being confusing/conflicting with other charts, IstioOperator nests them in the user-facing configuration,
	// then translates them to the flattened version.
	if comp.FlattenValues {
		cv, f := merged.GetPathMap("spec.values." + root)
		if f {
			vals, _ := merged.GetPathMap("spec.values")
			nv := values.Map{
				"global": vals["global"],
			}
			for k, v := range vals {
				_, isMap := v.(map[string]any)
				if !isMap {
					nv[k] = v
				}
			}
			for k, v := range cv {
				nv[k] = v
			}
			merged["spec"].(map[string]any)["values"] = nv
		}
	}
	return merged
}

// hubTagOverlay returns settings to override the default hub/tag, if the binary is compiled with specific versions
func hubTagOverlay() []string {
	hub := pkgversion.DockerInfo.Hub
	tag := pkgversion.DockerInfo.Tag
	if hub != "unknown" && tag != "unknown" {
		return []string{"hub=" + hub, "tag=" + tag}
	}
	return nil
}

// MergeInputs merges the various configuration inputs into one single IstioOperator.
func MergeInputs(filenames []string, flags []string, client kube.Client) (values.Map, error) {
	// We want our precedence order to be: base < profile < auto detected settings < files (in order) < --set flags (in order).
	// The tricky bit is we don't know where to read the profile from until we read the files/--set flags.
	// To handle this, we will build up these first, then apply it on top of the base once we know what base to use.
	// Initial base values
	userConfigBase, err := values.MapFromJSON([]byte(`{
  "apiVersion": "install.istio.io/v1alpha1",
  "kind": "IstioOperator",
  "metadata": {},
  "spec": {}
}`))
	if err != nil {
		return nil, err
	}

	// Apply all passed in files
	for i, fn := range filenames {
		var b []byte
		var err error
		if fn == "-" {
			if i != len(filenames)-1 {
				return nil, fmt.Errorf("stdin is only allowed as the last filename")
			}
			b, err = io.ReadAll(os.Stdin)
		} else {
			b, err = os.ReadFile(strings.TrimSpace(fn))
		}
		if err != nil {
			return nil, err
		}
		if err := checkNoMultipleIOPs(string(b)); err != nil {
			return nil, err
		}
		m, err := values.MapFromYaml(b)
		if err != nil {
			return nil, err
		}
		// Special hack to allow an empty spec to work. Should this be more generic?
		if m["spec"] == nil {
			delete(m, "spec")
		}
		userConfigBase.MergeFrom(m)
	}

	// Apply any --set flags
	if err := userConfigBase.SetSpecPaths(flags...); err != nil {
		return nil, err
	}

	installPackagePath := userConfigBase.GetPathString("spec.installPackagePath")
	profile := userConfigBase.GetPathString("spec.profile")
	userValues, _ := userConfigBase.GetPathMap("spec.values")

	// Now we have the base
	base, err := readProfile(installPackagePath, profile)
	if err != nil {
		return nil, err
	}

	// Overlay detected settings
	if err := base.SetSpecPaths(clusterSpecificSettings(client)...); err != nil {
		return nil, err
	}
	// Insert compiled in hub/tag
	if err := base.SetSpecPaths(hubTagOverlay()...); err != nil {
		return nil, err
	}

	// Merge the user values on top
	base.MergeFrom(userConfigBase)

	// Canonical-ize some of the values, translating things like `spec.hub` to `spec.values.global.hub` for helm compatibility
	base, err = translateIstioOperatorToHelm(base)
	if err != nil {
		return nil, err
	}

	// User values may override things from translateIstioOperatorToHelm.
	// For instance, I may set `values.istio_cni.enabled=true` without enabling the CNI component; translateIstioOperatorToHelm would set this to
	// nil.
	// So apply the user values on top as the last step
	if userValues != nil {
		base.MergeFrom(values.Map{"spec": values.Map{"values": userValues}})
	}
	return base, nil
}

func checkNoMultipleIOPs(s string) error {
	mfs, err := manifest.ParseMultiple(s)
	if err != nil {
		return fmt.Errorf("unable to parse file: %v", err)
	}
	if len(mfs) > 1 {
		return fmt.Errorf("contains multiple IstioOperator CRs, only one per file is supported")
	}
	return nil
}

// translateIstioOperatorToHelm converts top level IstioOperator configs into Helm values.
// Note: many other settings are done as post-processing steps, so they are not included here.
func translateIstioOperatorToHelm(base values.Map) (values.Map, error) {
	translations := map[string]string{
		"spec.hub":                  "global.hub",
		"spec.tag":                  "global.tag",
		"spec.revision":             "revision",
		"spec.meshConfig":           "meshConfig",
		"spec.compatibilityVersion": "compatibilityVersion",
	}
	for in, out := range translations {
		v, f := base.GetPath(in)
		if !f {
			continue
		}
		if _, ok := v.(map[string]any); ok {
			nm := values.MakeMap(v, "spec", "values", "meshConfig")
			base.MergeFrom(nm)
		} else {
			if err := base.SetSpecPaths(fmt.Sprintf("values.%s=%v", out, v)); err != nil {
				return nil, err
			}
		}
	}

	// Propagate component enablement to values. This is used for cross-chart dependencies.
	if err := base.SetPath("spec.values.pilot.enabled", base.GetPathBool("spec.components.pilot.enabled")); err != nil {
		return nil, err
	}
	if err := base.SetPath("spec.values.pilot.cni.enabled", base.GetPathBool("spec.components.cni.enabled")); err != nil {
		return nil, err
	}
	if n := base.GetPathString("spec.values.global.istioNamespace"); n != "" {
		if err := base.SetPath("metadata.namespace", n); err != nil {
			return nil, err
		}
	}
	return base, nil
}

// readProfile reads a profile, from given path.
func readProfile(path string, profile string) (values.Map, error) {
	if profile == "" {
		profile = "default"
	}
	// All profiles are based on applying on top of the 'default' profile
	base, err := readProfileInternal(path, "default")
	if err != nil {
		return nil, err
	}
	if profile == "default" {
		// If we requested the default profile, just return it
		return base, nil
	}
	// Otherwise, merge default with the requested profile
	top, err := readProfileInternal(path, profile)
	if err != nil {
		return nil, err
	}
	base.MergeFrom(top)
	return base, nil
}

func readProfileInternal(path string, profile string) (values.Map, error) {
	fs := manifests.BuiltinOrDir(path)
	f, err := fs.Open(fmt.Sprintf("profiles/%v.yaml", profile))
	if err != nil {
		return nil, fmt.Errorf("profile %q not found: %v", profile, err)
	}
	pb, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return values.MapFromYaml(pb)
}

var allowGKEAutoDetection = env.Register("AUTO_GKE_DETECTION", true, "If true, GKE will be detected automatically.").Get()

// clusterSpecificSettings computes any automatically detected settings from the cluster.
func clusterSpecificSettings(client kube.Client) []string {
	if client == nil {
		return nil
	}
	ver, err := client.GetKubernetesVersion()
	if err != nil {
		return nil
	}
	// https://istio.io/latest/docs/setup/additional-setup/cni/#hosted-kubernetes-settings
	// GKE requires deployment in kube-system namespace.
	if allowGKEAutoDetection && strings.Contains(ver.GitVersion, "-gke") {
		return []string{
			// This could be in istio-system with ResourceQuotas, but for backwards compatibility we move it to kube-system still
			"components.cni.namespace=kube-system",
			// Enable GKE profile, which ensures CNI Bin Dir is set appropriately
			"values.global.platform=gke",
			// Since we already put it in kube-system, don't bother deploying a resource quota
			"values.cni.resourceQuotas.enabled=false",
		}
	}
	return nil
}

// validateIstioOperator validates an IstioOperator, logging any warnings and reporting any errors.
func validateIstioOperator(iop values.Map, client kube.Client, logger clog.Logger, force bool) error {
	warnings, errs := validation.ParseAndValidateIstioOperator(iop, client)
	if err := errs.ToError(); err != nil {
		if force {
			if logger != nil {
				logger.PrintErr(fmt.Sprintf("spec invalid; continuing because of --force: %v", err))
			}
		} else {
			return err
		}
	}
	if logger != nil {
		for _, w := range warnings {
			logger.LogAndErrorf("%s %v", "❗", w)
		}
	}
	return nil
}
