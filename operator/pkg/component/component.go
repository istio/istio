package component

import (
	"fmt"

	"istio.io/istio/operator/john"
	"istio.io/istio/operator/pkg/values"
)

type Component struct {
	Name    Name
	Default bool
	Multi   bool
	// ResourceType maps a Name to the type of the rendered k8s resource.
	ResourceType string
	// ResourceName maps a Name to the name of the rendered k8s resource.
	ResourceName string
	// ContainerName maps a Name to the name of the container in a Deployment.
	ContainerName string
	// HelmSubdir is a mapping between a component name and the subdirectory of the component Chart.
	HelmSubdir string
	// ToHelmValuesTreeRoot is the tree root in values YAML files for the component.
	ToHelmValuesTreeRoot string
	// FlattenValues, if true, means the component expects values not prefixed with ToHelmValuesTreeRoot
	// For example `.name=foo` instead of `.component.name=foo`.
	FlattenValues bool
	// AltEnablementPath is the alternative path, from values, to enable the component
	AltEnablementPath string
}

func (c Component) Get(merged values.Map) ([]john.ComponentSpec, error) {
	defaultNamespace := values.TryGetPathAs[string](merged, "metadata.namespace")
	var defaultResponse []john.ComponentSpec
	def := c.Default
	if c.AltEnablementPath != "" {
		if values.TryGetPathAs[bool](merged, c.AltEnablementPath) {
			def = true
		}
	}
	if def {
		defaultResponse = []john.ComponentSpec{{Namespace: defaultNamespace}}
	}

	buildSpec := func(m values.Map) (john.ComponentSpec, error) {
		spec, err := values.ConvertMap[john.ComponentSpec](m)
		if err != nil {
			return john.ComponentSpec{}, fmt.Errorf("fail to convert %v: %v", c.Name, err)
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
		s, ok := merged.GetPath("spec.components." + string(c.Name))
		if !ok {
			return defaultResponse, nil
		}
		specs := []john.ComponentSpec{}
		for _, cur := range s.([]any) {
			m, _ := values.AsMap(cur)
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
	s, ok := merged.GetPathMap("spec.components." + string(c.Name))
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
	return []john.ComponentSpec{spec}, nil
}

// Name is a component name string, typed to constrain allowed values.
type Name string

const (
	// IstioComponent names corresponding to the IstioOperator proto component names. Must be the same, since these
	// are used for struct traversal.
	BaseComponentName  Name = "base"
	PilotComponentName Name = "pilot"

	CNIComponentName     Name = "cni"
	ZtunnelComponentName Name = "ztunnel"

	// istiod remote component
	IstiodRemoteComponentName Name = "istiodRemote"

	IngressComponentName Name = "ingressGateways"
	EgressComponentName  Name = "egressGateways"
)

var AllComponents = []Component{
	{
		Name:                 BaseComponentName,
		Default:              true,
		HelmSubdir:           "base",
		ToHelmValuesTreeRoot: "global",
	},
	{
		Name:                 PilotComponentName,
		Default:              true,
		ResourceType:         "Deployment",
		ResourceName:         "istiod",
		ContainerName:        "discovery",
		HelmSubdir:           "istio-control/istio-discovery",
		ToHelmValuesTreeRoot: "pilot",
	},
	{
		Name:                 IngressComponentName,
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
		Name:                 EgressComponentName,
		Multi:                true,
		ResourceType:         "Deployment",
		ResourceName:         "istio-egressgateway",
		ContainerName:        "istio-proxy",
		HelmSubdir:           "gateways/istio-egress",
		ToHelmValuesTreeRoot: "gateways.istio-egressgateway",
		AltEnablementPath:    "spec.values.gateways.istio-egressgateway.enabled",
	},
	{
		Name:                 CNIComponentName,
		ResourceType:         "DaemonSet",
		ResourceName:         "istio-cni-node",
		ContainerName:        "install-cni",
		HelmSubdir:           "istio-cni",
		ToHelmValuesTreeRoot: "cni",
	},
	{
		Name:                 IstiodRemoteComponentName,
		HelmSubdir:           "istiod-remote",
		ToHelmValuesTreeRoot: "global",
	},
	{
		Name:                 ZtunnelComponentName,
		ResourceType:         "DaemonSet",
		ResourceName:         "ztunnel",
		HelmSubdir:           "ztunnel",
		ToHelmValuesTreeRoot: "ztunnel",
		ContainerName:        "istio-proxy",
		FlattenValues:        true,
	},
}

var (
	userFacingComponentNames = map[Name]string{
		BaseComponentName:         "Istio core",
		PilotComponentName:        "Istiod",
		CNIComponentName:          "CNI",
		ZtunnelComponentName:      "Ztunnel",
		IngressComponentName:      "Ingress gateways",
		EgressComponentName:       "Egress gateways",
		IstiodRemoteComponentName: "Istiod remote",
	}

	Icons = map[Name]string{
		BaseComponentName:    "⛵️",
		PilotComponentName:   "🧠",
		CNIComponentName:     "🪢",
		ZtunnelComponentName: "🔒",
		IngressComponentName: "🛬",
		EgressComponentName:  "🛫",
	}
)

// UserFacingComponentName returns the name of the given component that should be displayed to the user in high
// level CLIs (like progress log).
func UserFacingComponentName(name Name) string {
	ret, ok := userFacingComponentNames[name]
	if !ok {
		return "Unknown"
	}
	return ret
}
