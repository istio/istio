package john

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/validate"
	"istio.io/istio/pkg/kube"
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
			manifests, err := Render(spec, comp, merged.DeepClone())
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

func hubTagOverlay() []string {
	hub := pkgversion.DockerInfo.Hub
	tag := pkgversion.DockerInfo.Tag
	if hub != "unknown" && tag != "unknown" {
		return []string{"hub=" + hub, "tag=" + tag}
	}
	return nil
}

func MergeInputs(filenames []string, flags []string, client kube.Client) (Map, error) {
	// Initial base values
	base, err := MapFromJson([]byte(`{
  "apiVersion": "install.istio.io/v1alpha1",
  "kind": "IstioOperator",
  "metadata": {
    "namespace": "istio-system"
  },
  "spec": {
    "hub": "gcr.io/istio-testing",
    "tag": "latest",
    "components": {
    },
    "values": {
      "defaultRevision": ""
    }
  }
}
`))
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
		base.MergeInto(m)
	}

	// Apply any --set flags
	if err := base.SetSpecPaths(flags...); err != nil {
		return nil, err
	}

	return base, nil
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
			specs = append(specs, spec)
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
