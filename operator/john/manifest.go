package john

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"

	"istio.io/istio/manifests"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/validate"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	pkgversion "istio.io/istio/pkg/version"
)

type ManifestSet struct {
	Component string
	Manifests []Manifest
	// TODO: notes, warnings, etc?
}

func GenerateManifest(files []string, setFlags []string, force bool, filter []string, client kube.Client) ([]ManifestSet, error) {
	merged, err := MergeInputs(files, setFlags, client)
	if err != nil {
		return nil, err
	}
	iop, err := IstioOperatorFromJSON(merged.JSON(), force)
	_ = iop
	if err != nil {
		return nil, err
	}

	var allManifests []ManifestSet
	for _, comp := range Components {
		specs, err := comp.Get(merged)
		if err != nil {
			return nil, err
		}
		for _, spec := range specs {

			manifests, err := Render(spec, comp, merged)
			if err != nil {
				return nil, err
			}
			manifests, err = postProcess(comp, spec, manifests)
			if err != nil {
				return nil, err
			}
			allManifests = append(allManifests, ManifestSet{
				Component: comp.Name,
				Manifests: manifests,
			})
		}
	}
	// TODO: istioNamespace -> IOP.namespace
	// TODO: set components based on profile
	// TODO: ValuesEnablementPathMap? This enables the ingress or egress
	return allManifests, nil
}

func postProcess(comp Component, spec ComponentSpec, manifests []Manifest) ([]Manifest, error) {
	if spec.Kubernetes == nil {
		return manifests, nil
	}
	type Patch struct {
		Kind, Name string
		Patch      string
	}
	rn := comp.ResourceName
	rt := comp.ResourceType
	patches := map[string]Patch{
		"affinity":            {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"affinity":%s}}}}`},
		"env":                 {Kind: rt, Name: rn, Patch: fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":%q, "env": %%s}]}}}}`, comp.ContainerName)},
		"hpaSpec":             {Kind: "HorizontalPodAutoscaler", Name: rn, Patch: `{"spec":%s}`},
		"imagePullPolicy":     {Kind: rt, Name: rn, Patch: fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":%q, "imagePullPolicy": %%s}]}}}}`, comp.ContainerName)},
		"nodeSelector":        {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"nodeSelector":%s}}}}`},
		"podDisruptionBudget": {Kind: "PodDisruptionBudget", Name: rn, Patch: `{"spec":%s}`},
		"podAnnotations":      {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"metadata":{"annotations":%s}}}}`},
		"priorityClassName":   {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"priorityClassName":%s}}}}`},
		"readinessProbe":      {Kind: rt, Name: rn, Patch: fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":%q, "readinessProbe": %%s}]}}}}`, comp.ContainerName)},
		"replicaCount":        {Kind: rt, Name: rn, Patch: `{"spec":{"replicas":%s}}`},
		"resources":           {Kind: rt, Name: rn, Patch: fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":%q, "resources": %%s}]}}}}`, comp.ContainerName)},
		"strategy":            {Kind: rt, Name: rn, Patch: `{"spec":{"strategy":%s}}`},
		"tolerations":         {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"tolerations":%s}}}}`},
		"serviceAnnotations":  {Kind: "Service", Name: rn, Patch: `{"metadata":{"annotations":%s}}`},
		"service":             {Kind: "Service", Name: rn, Patch: `{"spec"::%s}`},
		"securityContext":     {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"securityContext":%s}}}}`},
	}
	needPatching := map[int][]string{}
	for field, k := range patches {
		v, ok := spec.Raw.GetPath("k8s." + field)
		if !ok {
			log.Errorf("howardjohn: %v", field)
			continue
		}
		inner, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		patch := fmt.Sprintf(k.Patch, inner)
		// Find which manifests need the patch
		for idx, m := range manifests {
			if k.Kind == m.GetKind() && k.Name == m.GetName() {
				needPatching[idx] = append(needPatching[idx], patch)
			}
		}
	}
	for idx, patches := range needPatching {
		m := manifests[idx]
		baseJSON, err := yaml.YAMLToJSON([]byte(m.Content))
		if err != nil {
			return nil, err
		}
		typed, err := scheme.Scheme.New(m.GroupVersionKind())
		if err != nil {
			return nil, err
		}

		for _, patch := range patches {
			newBytes, err := strategicpatch.StrategicMergePatch(baseJSON, []byte(patch), typed)
			if err != nil {
				return nil, err
			}
			baseJSON = newBytes
		}
		us := &unstructured.Unstructured{}
		if err := json.Unmarshal(baseJSON, us); err != nil {
			return nil, err
		}
		yml, err := yaml.Marshal(us)
		if err != nil {
			return nil, err
		}
		// Rebuild our manifest
		manifests[idx] = Manifest{
			Unstructured: us,
			Content:      string(yml),
		}
	}
	return manifests, nil
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
func MergeInputs(filenames []string, flags []string, client kube.Client) (Map, error) {
	// We want our precedence order to be: base < profile < auto detected settings < files (in order) < --set flags (in order).
	// The tricky bit is we don't know where to read the profile from until we read the files/--set flags.
	// To handle this, we will build up these first, then apply it on top of the base once we know what base to use.
	// Initial base values
	userConfigBase, err := MapFromJson([]byte(`{
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
		m, err := MapFromYaml(b)
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

	installPackagePath := TryGetPathAs[string](userConfigBase, "spec.installPackagePath")
	profile := TryGetPathAs[string](userConfigBase, "spec.profile")

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

func translateIstioOperatorToHelm(base Map) (Map, error) {
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
			nm := MakeMap(v, "spec", "values", "meshConfig")
			base.MergeFrom(nm)
		} else {
			if err := base.SetSpecPaths(fmt.Sprintf("values.%s=%v", out, v)); err != nil {
				return nil, err
			}
		}
	}
	return base, nil
}

func readProfile(path string, profile string) (Map, error) {
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
	return MapFromYaml(pb)
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

func IstioOperatorFromJSON(iopString string, force bool) (*v1alpha1.IstioOperator, error) {
	iop := &v1alpha1.IstioOperator{}
	if err := json.Unmarshal([]byte(iopString), iop); err != nil {
		return nil, err
	}
	if errs := validate.CheckIstioOperatorSpec(iop.Spec); len(errs) != 0 && !force {
		// l.LogAndError("Run the command with the --force flag if you want to ignore the validation error and proceed.")
		return iop, fmt.Errorf(errs.Error())
	}
	return iop, nil
}

type Component struct {
	Name    string
	Default bool
	Multi   bool
	// ResourceType maps a ComponentName to the type of the rendered k8s resource.
	ResourceType string
	// ResourceName maps a ComponentName to the name of the rendered k8s resource.
	ResourceName string
	// ContainerName maps a ComponentName to the name of the container in a Deployment.
	ContainerName string
	// HelmSubdir is a mapping between a component name and the subdirectory of the component Chart.
	HelmSubdir string
	// ToHelmValuesTreeRoot is the tree root in values YAML files for the component.
	ToHelmValuesTreeRoot string
	// SkipReverseTranslate defines whether reverse translate of this component need to be skipped.
	SkipReverseTranslate bool
	// FlattenValues, if true, means the component expects values not prefixed with ToHelmValuesTreeRoot
	// For example `.name=foo` instead of `.component.name=foo`.
	FlattenValues     bool
	AltEnablementPath string
}

func (c Component) Get(merged Map) ([]ComponentSpec, error) {
	defaultNamespace := TryGetPathAs[string](merged, "metadata.namespace")
	var defaultResponse []ComponentSpec
	def := c.Default
	if c.AltEnablementPath != "" {
		if TryGetPathAs[bool](merged, c.AltEnablementPath) {
			def = true
		}
	}
	if def {
		defaultResponse = []ComponentSpec{{Namespace: defaultNamespace}}
	}

	buildSpec := func(m Map) (ComponentSpec, error) {
		spec, err := ConvertMap[ComponentSpec](m)
		if err != nil {
			return ComponentSpec{}, fmt.Errorf("fail to convert %v: %v", c.Name, err)
		}
		if spec.Namespace == "" {
			spec.Namespace = defaultNamespace
		}
		if spec.Namespace == "" {
			spec.Namespace = "istio-system"
		}
		spec.Raw = m
		return spec, nil
	}
	// List of components
	if c.Multi {
		s, ok := merged.GetPath("spec.components." + c.Name)
		if !ok {
			return defaultResponse, nil
		}
		specs := []ComponentSpec{}
		for _, cur := range s.([]any) {
			m, _ := asMap(cur)
			spec, err := buildSpec(m)
			if err != nil {
				return nil, err
			}
			if spec.Enabled.GetValueOrTrue() {
				specs = append(specs, spec)
			}
		}
		return specs, nil
	}
	// Single component
	s, ok := merged.GetPathMap("spec.components." + c.Name)
	if !ok {
		return defaultResponse, nil
	}
	spec, err := buildSpec(s)
	if err != nil {
		return nil, err
	}
	if !spec.Enabled.GetValueOrTrue() {
		return nil, nil
	}
	return []ComponentSpec{spec}, nil
}

var Components = []Component{
	{
		Name:                 "base",
		Default:              true,
		HelmSubdir:           "base",
		ToHelmValuesTreeRoot: "global",
		SkipReverseTranslate: true,
	},
	{
		Name:                 "pilot",
		Default:              true,
		ResourceType:         "Deployment",
		ResourceName:         "istiod",
		ContainerName:        "discovery",
		HelmSubdir:           "istio-control/istio-discovery",
		ToHelmValuesTreeRoot: "pilot",
	},
	{
		Name:                 "ingressGateways",
		Multi:                true,
		Default:              true,
		ResourceType:         "Deployment",
		ResourceName:         "istio-ingressgateway",
		ContainerName:        "istio-proxy",
		HelmSubdir:           "gateways/istio-ingress",
		ToHelmValuesTreeRoot: "gateways.istio-ingressgateway",
		AltEnablementPath:    "spec.values.gateways.istio-ingressgateway.enabled",
	},
	{
		Name:                 "egressGateways",
		Multi:                true,
		ResourceType:         "Deployment",
		ResourceName:         "istio-egressgateway",
		ContainerName:        "istio-proxy",
		HelmSubdir:           "gateways/istio-egress",
		ToHelmValuesTreeRoot: "gateways.istio-egressgateway",
		AltEnablementPath:    "spec.values.gateways.istio-egressgateway.enabled",
	},
	{
		Name:                 "cni",
		ResourceType:         "DaemonSet",
		ResourceName:         "istio-cni-node",
		ContainerName:        "install-cni",
		HelmSubdir:           "istio-cni",
		ToHelmValuesTreeRoot: "cni",
	},
	{
		Name:                 "istiodRemote",
		HelmSubdir:           "istiod-remote",
		ToHelmValuesTreeRoot: "global",
		SkipReverseTranslate: true,
	},
	{
		Name:                 "ztunnel",
		ResourceType:         "DaemonSet",
		ResourceName:         "ztunnel",
		HelmSubdir:           "ztunnel",
		ToHelmValuesTreeRoot: "ztunnel",
		ContainerName:        "istio-proxy",
		FlattenValues:        true,
	},
}
