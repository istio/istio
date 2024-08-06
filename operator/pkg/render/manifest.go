package render

import (
	"fmt"
	"io"
	"os"
	"strings"

	"istio.io/istio/manifests"
	"istio.io/istio/operator/pkg/apis"
	"istio.io/istio/operator/pkg/component"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/values"
	"istio.io/istio/pkg/kube"
	pkgversion "istio.io/istio/pkg/version"
)

func GenerateManifest(files []string, setFlags []string, force bool, filter []string, client kube.Client) ([]manifest.ManifestSet, values.Map, error) {
	merged, err := MergeInputs(files, setFlags, client)
	if err != nil {
		return nil, nil, fmt.Errorf("merge inputs: %v", err)
	}
	iop, err := IstioOperatorFromJSON(merged.JSON(), force)
	_ = iop
	if err != nil {
		return nil, nil, err
	}

	var allManifests []manifest.ManifestSet
	for _, comp := range component.AllComponents {
		specs, err := comp.Get(merged)
		if err != nil {
			return nil, nil, fmt.Errorf("get component %v: %v", comp.Name, err)
		}
		for _, spec := range specs {
			values := applyComponentValuesToHelmValues(comp, spec, merged)
			manifests, err := helm.Render(spec.Namespace, comp.HelmSubdir, values)
			if err != nil {
				return nil, nil, fmt.Errorf("helm render: %v", err)
			}
			manifests, err = postProcess(comp, spec, manifests)
			if err != nil {
				return nil, nil, fmt.Errorf("post processing: %v", err)
			}
			allManifests = append(allManifests, manifest.ManifestSet{
				Component: comp.Name,
				Manifests: manifests,
			})
		}
	}
	// TODO: istioNamespace -> IOP.namespace
	// TODO: set components based on profile
	// TODO: ValuesEnablementPathMap? This enables the ingress or egress
	return allManifests, merged, nil
}

func applyComponentValuesToHelmValues(comp component.Component, spec apis.GatewayComponentSpec, merged values.Map) values.Map {
	root := comp.ToHelmValuesTreeRoot
	if comp.Name == "ingressGateways" || comp.Name == "egressGateways" {
		merged = merged.DeepClone()
		merged.SetPath(fmt.Sprintf("spec.values.%s.name", root), spec.Name)
		merged.SetPath(fmt.Sprintf("spec.values.%s.labels", root), spec.Label)
		// TODO: labels, ports
	}
	if !comp.FlattenValues && spec.Hub == "" && spec.Tag == nil && spec.Label == nil {
		return merged
	}
	merged = merged.DeepClone()
	if spec.Hub != "" {
		merged.SetSpecPaths(fmt.Sprintf("values.%s.hub=%s", root, spec.Hub))
	}
	if spec.Tag != "" {
		merged.SetSpecPaths(fmt.Sprintf("values.%s.tag=%v", root, spec.Tag))
	}
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
	userConfigBase, err := values.MapFromJson([]byte(`{
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

	installPackagePath := values.TryGetPathAs[string](userConfigBase, "spec.installPackagePath")
	profile := values.TryGetPathAs[string](userConfigBase, "spec.profile")

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

	// Canonicalize some of the values, translating things like spec.hub to spec.values.global.hub for helm compatibility
	return translateIstioOperatorToHelm(base)
}

func translateIstioOperatorToHelm(base values.Map) (values.Map, error) {
	translations := map[string]string{
		"spec.hub":                  "global.hub",
		"spec.tag":                  "global.tag",
		"spec.revision":             "revision",
		"spec.meshConfig":           "meshConfig",
		"spec.compatibilityVersion": "compatibilityVersion",
		// TODO: istioNamespace?
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
	return base, nil
}

func readProfile(path string, profile string) (values.Map, error) {
	if profile == "" {
		profile = "default"
	}
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
	if strings.Contains(ver.GitVersion, "-gke") {
		return []string{"components.cni.namespace=kube-system"}
	}
	return nil
}

func IstioOperatorFromJSON(iopString string, force bool) (*apis.IstioOperator, error) {
	iop := &apis.IstioOperator{}
	// TODO: consolidate the two validation packagess
	//if err := json.Unmarshal([]byte(iopString), iop); err != nil {
	//	return nil, err
	//}
	//if errs := validate.CheckIstioOperatorSpec(iop.Spec); len(errs) != 0 && !force {
	//	l.LogAndError("Run the command with the --force flag if you want to ignore the validation error and proceed.")
	//return iop, fmt.Errorf(errs.Error())
	//}
	return iop, nil
}
