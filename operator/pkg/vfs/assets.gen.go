// Code generated for package vfs by go-bindata DO NOT EDIT. (@generated)
// sources:
// examples/customresource/istio_v1alpha1_istiooperator_cr.yaml
// examples/multicluster/values-istio-multicluster-gateways.yaml
// examples/multicluster/values-istio-multicluster-primary.yaml
// examples/user-gateway/ingress-gateway-only.yaml
// examples/vm/values-istio-meshexpansion-gateways.yaml
// examples/vm/values-istio-meshexpansion.yaml
// translateConfig/names-1.5.yaml
// translateConfig/names-1.6.yaml
// translateConfig/names-1.7.yaml
// translateConfig/reverseTranslateConfig-1.4.yaml
// translateConfig/reverseTranslateConfig-1.5.yaml
// translateConfig/reverseTranslateConfig-1.6.yaml
// translateConfig/reverseTranslateConfig-1.7.yaml
// translateConfig/translateConfig-1.3.yaml
// translateConfig/translateConfig-1.4.yaml
// translateConfig/translateConfig-1.5.yaml
// translateConfig/translateConfig-1.6.yaml
// translateConfig/translateConfig-1.7.yaml
// versions.yaml
package vfs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)
type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name return file name
func (fi bindataFileInfo) Name() string {
	return fi.name
}

// Size return file size
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}

// Mode return file mode
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}

// Mode return file modify time
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir return file whether a directory
func (fi bindataFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir != 0
}

// Sys return file is sys mode
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var _examplesCustomresourceIstio_v1alpha1_istiooperator_crYaml = []byte(`---
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  namespace: istio-system
  name: example-istiocontrolplane
spec:
  profile: demo
...
`)

func examplesCustomresourceIstio_v1alpha1_istiooperator_crYamlBytes() ([]byte, error) {
	return _examplesCustomresourceIstio_v1alpha1_istiooperator_crYaml, nil
}

func examplesCustomresourceIstio_v1alpha1_istiooperator_crYaml() (*asset, error) {
	bytes, err := examplesCustomresourceIstio_v1alpha1_istiooperator_crYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/customresource/istio_v1alpha1_istiooperator_cr.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _examplesMulticlusterValuesIstioMulticlusterGatewaysYaml = []byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  addonComponents:
    istiocoredns:
      enabled: true

  components:
    egressGateways:
      - name: istio-egressgateway
        enabled: true

  values:
    global:
      # Provides dns resolution for global services
      podDNSSearchNamespaces:
        - global
        - "{{ valueOrDefault .DeploymentMeta.Namespace \"default\" }}.global"

      multiCluster:
        enabled: true

      controlPlaneSecurityEnabled: true

    gateways:
      istio-egressgateway:
        env:
          # Needed to route traffic via egress gateway if desired.
          ISTIO_META_REQUESTED_NETWORK_VIEW: "external"
`)

func examplesMulticlusterValuesIstioMulticlusterGatewaysYamlBytes() ([]byte, error) {
	return _examplesMulticlusterValuesIstioMulticlusterGatewaysYaml, nil
}

func examplesMulticlusterValuesIstioMulticlusterGatewaysYaml() (*asset, error) {
	bytes, err := examplesMulticlusterValuesIstioMulticlusterGatewaysYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/multicluster/values-istio-multicluster-gateways.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _examplesMulticlusterValuesIstioMulticlusterPrimaryYaml = []byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  # https://istio.io/docs/setup/install/multicluster/shared/#main-cluster
  values:
    global:
      multiCluster:
        # unique cluster name, must be a DNS label name
        clusterName: main0
      network: network1

      # Mesh network configuration. This is optional and may be omitted if
      # all clusters are on the same network.
      meshNetworks:
        network1:
          endpoints:
          # Always use Kubernetes as the registry name for the main cluster in the mesh network configuration
          - fromRegistry: Kubernetes
          gateways:
          - registry_service_name: istio-ingressgateway.istio-system.svc.cluster.local
            port: 443

        network2:
          endpoints:
          - fromRegistry: remote0
          gateways:
          - registry_service_name: istio-ingressgateway.istio-system.svc.cluster.local
            port: 443

      # Use the existing istio-ingressgateway.
      meshExpansion:
        enabled: true
`)

func examplesMulticlusterValuesIstioMulticlusterPrimaryYamlBytes() ([]byte, error) {
	return _examplesMulticlusterValuesIstioMulticlusterPrimaryYaml, nil
}

func examplesMulticlusterValuesIstioMulticlusterPrimaryYaml() (*asset, error) {
	bytes, err := examplesMulticlusterValuesIstioMulticlusterPrimaryYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/multicluster/values-istio-multicluster-primary.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _examplesUserGatewayIngressGatewayOnlyYaml = []byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  profile: empty
  gateways:
    enabled: true
    components:
      namespace: my-namespace
      ingressGateway:
        enabled: true
`)

func examplesUserGatewayIngressGatewayOnlyYamlBytes() ([]byte, error) {
	return _examplesUserGatewayIngressGatewayOnlyYaml, nil
}

func examplesUserGatewayIngressGatewayOnlyYaml() (*asset, error) {
	bytes, err := examplesUserGatewayIngressGatewayOnlyYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/user-gateway/ingress-gateway-only.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _examplesVmValuesIstioMeshexpansionGatewaysYaml = []byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  values:
    global:
      multiCluster:
        enabled: true

      meshExpansion:
        enabled: true

      controlPlaneSecurityEnabled: true

    # Provides dns resolution for service entries of form
    # name.namespace.global
    istiocoredns:
      enabled: true
`)

func examplesVmValuesIstioMeshexpansionGatewaysYamlBytes() ([]byte, error) {
	return _examplesVmValuesIstioMeshexpansionGatewaysYaml, nil
}

func examplesVmValuesIstioMeshexpansionGatewaysYaml() (*asset, error) {
	bytes, err := examplesVmValuesIstioMeshexpansionGatewaysYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/vm/values-istio-meshexpansion-gateways.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _examplesVmValuesIstioMeshexpansionYaml = []byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  values:
    global:
      meshExpansion:
        enabled: true

      controlPlaneSecurityEnabled: true
`)

func examplesVmValuesIstioMeshexpansionYamlBytes() ([]byte, error) {
	return _examplesVmValuesIstioMeshexpansionYaml, nil
}

func examplesVmValuesIstioMeshexpansionYaml() (*asset, error) {
	bytes, err := examplesVmValuesIstioMeshexpansionYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "examples/vm/values-istio-meshexpansion.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigNames15Yaml = []byte(`DeprecatedComponentNames:
  - "Injector"
  - "CertManager"
  - "NodeAgent"
  - "SidecarInjector"

`)

func translateconfigNames15YamlBytes() ([]byte, error) {
	return _translateconfigNames15Yaml, nil
}

func translateconfigNames15Yaml() (*asset, error) {
	bytes, err := translateconfigNames15YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/names-1.5.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigNames16Yaml = []byte(`DeprecatedComponentNames:
  - "Injector"
  - "CertManager"
  - "NodeAgent"
  - "SidecarInjector"
`)

func translateconfigNames16YamlBytes() ([]byte, error) {
	return _translateconfigNames16Yaml, nil
}

func translateconfigNames16Yaml() (*asset, error) {
	bytes, err := translateconfigNames16YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/names-1.6.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigNames17Yaml = []byte(`DeprecatedComponentNames:
  - "Injector"
  - "CertManager"
  - "NodeAgent"
  - "SidecarInjector"`)

func translateconfigNames17YamlBytes() ([]byte, error) {
	return _translateconfigNames17Yaml, nil
}

func translateconfigNames17Yaml() (*asset, error) {
	bytes, err := translateconfigNames17YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/names-1.7.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigReversetranslateconfig14Yaml = []byte(`kubernetesPatternMapping:
  "{{.ValueComponentName}}.env":                   "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.Env"
  "{{.ValueComponentName}}.autoscaleEnabled":      "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.HpaSpec"
  "{{.ValueComponentName}}.imagePullPolicy":       "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.ImagePullPolicy"
  "{{.ValueComponentName}}.nodeSelector":          "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.NodeSelector"
  "{{.ValueComponentName}}.tolerations":           "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.Tolerations"
  "{{.ValueComponentName}}.podDisruptionBudget":   "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.PodDisruptionBudget"
  "{{.ValueComponentName}}.podAnnotations":        "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.PodAnnotations"
  "{{.ValueComponentName}}.priorityClassName":     "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.PriorityClassName"
  "{{.ValueComponentName}}.readinessProbe":        "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.ReadinessProbe"
  "{{.ValueComponentName}}.replicaCount":          "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.ReplicaCount"
  "{{.ValueComponentName}}.resources":             "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.Resources"
  "{{.ValueComponentName}}.rollingMaxSurge":       "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.Strategy"
  "{{.ValueComponentName}}.rollingMaxUnavailable": "{{.FeatureName}}.Components.{{.ComponentName}}.K8s.Strategy"`)

func translateconfigReversetranslateconfig14YamlBytes() ([]byte, error) {
	return _translateconfigReversetranslateconfig14Yaml, nil
}

func translateconfigReversetranslateconfig14Yaml() (*asset, error) {
	bytes, err := translateconfigReversetranslateconfig14YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/reverseTranslateConfig-1.4.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigReversetranslateconfig15Yaml = []byte(`kubernetesPatternMapping:
  "{{.ValueComponentName}}.env":                   "Components.{{.ComponentName}}.K8s.Env"
  "{{.ValueComponentName}}.autoscaleEnabled":      "Components.{{.ComponentName}}.K8s.HpaSpec"
  "{{.ValueComponentName}}.imagePullPolicy":       "Components.{{.ComponentName}}.K8s.ImagePullPolicy"
  "{{.ValueComponentName}}.nodeSelector":          "Components.{{.ComponentName}}.K8s.NodeSelector"
  "{{.ValueComponentName}}.tolerations":           "Components.{{.ComponentName}}.K8s.Tolerations"
  "{{.ValueComponentName}}.podDisruptionBudget":   "Components.{{.ComponentName}}.K8s.PodDisruptionBudget"
  "{{.ValueComponentName}}.podAnnotations":        "Components.{{.ComponentName}}.K8s.PodAnnotations"
  "{{.ValueComponentName}}.priorityClassName":     "Components.{{.ComponentName}}.K8s.PriorityClassName"
  "{{.ValueComponentName}}.readinessProbe":        "Components.{{.ComponentName}}.K8s.ReadinessProbe"
  "{{.ValueComponentName}}.replicaCount":          "Components.{{.ComponentName}}.K8s.ReplicaCount"
  "{{.ValueComponentName}}.resources":             "Components.{{.ComponentName}}.K8s.Resources"
  "{{.ValueComponentName}}.rollingMaxSurge":       "Components.{{.ComponentName}}.K8s.Strategy"
  "{{.ValueComponentName}}.rollingMaxUnavailable": "Components.{{.ComponentName}}.K8s.Strategy"
  "{{.ValueComponentName}}.serviceAnnotations":    "Components.{{.ComponentName}}.K8s.ServiceAnnotations"`)

func translateconfigReversetranslateconfig15YamlBytes() ([]byte, error) {
	return _translateconfigReversetranslateconfig15Yaml, nil
}

func translateconfigReversetranslateconfig15Yaml() (*asset, error) {
	bytes, err := translateconfigReversetranslateconfig15YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/reverseTranslateConfig-1.5.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigReversetranslateconfig16Yaml = []byte(`kubernetesPatternMapping:
  "{{.ValueComponentName}}.env":                   "Components.{{.ComponentName}}.K8s.Env"
  "{{.ValueComponentName}}.autoscaleEnabled":      "Components.{{.ComponentName}}.K8s.HpaSpec"
  "{{.ValueComponentName}}.imagePullPolicy":       "Components.{{.ComponentName}}.K8s.ImagePullPolicy"
  "{{.ValueComponentName}}.nodeSelector":          "Components.{{.ComponentName}}.K8s.NodeSelector"
  "{{.ValueComponentName}}.tolerations":           "Components.{{.ComponentName}}.K8s.Tolerations"
  "{{.ValueComponentName}}.podDisruptionBudget":   "Components.{{.ComponentName}}.K8s.PodDisruptionBudget"
  "{{.ValueComponentName}}.podAnnotations":        "Components.{{.ComponentName}}.K8s.PodAnnotations"
  "{{.ValueComponentName}}.priorityClassName":     "Components.{{.ComponentName}}.K8s.PriorityClassName"
  "{{.ValueComponentName}}.readinessProbe":        "Components.{{.ComponentName}}.K8s.ReadinessProbe"
  "{{.ValueComponentName}}.replicaCount":          "Components.{{.ComponentName}}.K8s.ReplicaCount"
  "{{.ValueComponentName}}.resources":             "Components.{{.ComponentName}}.K8s.Resources"
  "{{.ValueComponentName}}.rollingMaxSurge":       "Components.{{.ComponentName}}.K8s.Strategy"
  "{{.ValueComponentName}}.rollingMaxUnavailable": "Components.{{.ComponentName}}.K8s.Strategy"
  "{{.ValueComponentName}}.serviceAnnotations":    "Components.{{.ComponentName}}.K8s.ServiceAnnotations"
`)

func translateconfigReversetranslateconfig16YamlBytes() ([]byte, error) {
	return _translateconfigReversetranslateconfig16Yaml, nil
}

func translateconfigReversetranslateconfig16Yaml() (*asset, error) {
	bytes, err := translateconfigReversetranslateconfig16YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/reverseTranslateConfig-1.6.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigReversetranslateconfig17Yaml = []byte(`kubernetesPatternMapping:
  "{{.ValueComponentName}}.env":                   "Components.{{.ComponentName}}.K8s.Env"
  "{{.ValueComponentName}}.autoscaleEnabled":      "Components.{{.ComponentName}}.K8s.HpaSpec"
  "{{.ValueComponentName}}.imagePullPolicy":       "Components.{{.ComponentName}}.K8s.ImagePullPolicy"
  "{{.ValueComponentName}}.nodeSelector":          "Components.{{.ComponentName}}.K8s.NodeSelector"
  "{{.ValueComponentName}}.tolerations":           "Components.{{.ComponentName}}.K8s.Tolerations"
  "{{.ValueComponentName}}.podDisruptionBudget":   "Components.{{.ComponentName}}.K8s.PodDisruptionBudget"
  "{{.ValueComponentName}}.podAnnotations":        "Components.{{.ComponentName}}.K8s.PodAnnotations"
  "{{.ValueComponentName}}.priorityClassName":     "Components.{{.ComponentName}}.K8s.PriorityClassName"
  "{{.ValueComponentName}}.readinessProbe":        "Components.{{.ComponentName}}.K8s.ReadinessProbe"
  "{{.ValueComponentName}}.replicaCount":          "Components.{{.ComponentName}}.K8s.ReplicaCount"
  "{{.ValueComponentName}}.resources":             "Components.{{.ComponentName}}.K8s.Resources"
  "{{.ValueComponentName}}.rollingMaxSurge":       "Components.{{.ComponentName}}.K8s.Strategy"
  "{{.ValueComponentName}}.rollingMaxUnavailable": "Components.{{.ComponentName}}.K8s.Strategy"
  "{{.ValueComponentName}}.serviceAnnotations":    "Components.{{.ComponentName}}.K8s.ServiceAnnotations"`)

func translateconfigReversetranslateconfig17YamlBytes() ([]byte, error) {
	return _translateconfigReversetranslateconfig17Yaml, nil
}

func translateconfigReversetranslateconfig17Yaml() (*asset, error) {
	bytes, err := translateconfigReversetranslateconfig17YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/reverseTranslateConfig-1.7.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigTranslateconfig13Yaml = []byte(`apiMapping:
  Hub:
    outPath: "global.hub"
  Tag:
    outPath: "global.tag"
  K8SDefaults:
    outPath: "global.resources"
  DefaultNamespace:
    outPath: "global.istioNamespace"
  Values.Proxy:
    outPath: "global.proxy"
  ConfigManagement.Components.Namespace:
    outPath: "global.configNamespace"
  Policy.Components.Namespace:
    outPath: "global.policyNamespace"
  Telemetry.Components.Namespace:
    outPath: "global.telemetryNamespace"
  Security.Components.Namespace:
    outPath: "global.securityNamespace"
kubernetesMapping:
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Affinity":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.affinity"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Env":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].env"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.HpaSpec":
    outPath: "[HorizontalPodAutoscaler:{{.ResourceName}}].spec"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.ImagePullPolicy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].imagePullPolicy"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.NodeSelector":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.nodeSelector"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.PodDisruptionBudget":
    outPath: "[PodDisruptionBudget:{{.ResourceName}}].spec"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.PodAnnotations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.metadata.annotations"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.PriorityClassName":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.priorityClassName."
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.ReadinessProbe":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].readinessProbe"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.ReplicaCount":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.replicas"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Resources":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].resources"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Strategy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.strategy"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Tolerations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.tolerations"
toFeature:
    crds:               Base
    Pilot:              TrafficManagement
    Galley:             ConfigManagement
    Injector:           AutoInjection
    Policy:             Policy
    Telemetry:          Telemetry
    Citadel:            Security
    CertManager:        Security
    NodeAgent:          Security
    IngressGateway:     Gateways
    EgressGateway:      Gateways
    Cni:                Cni
    Grafana:            ThirdParty
    Prometheus:         ThirdParty
    Tracing:            ThirdParty
    PrometheusOperator: ThirdParty
    Kiali:              ThirdParty
globalNamespaces:
  Pilot:      "istioNamespace"
  Galley:     "configNamespace"
  Telemetry:  "telemetryNamespace"
  Policy:     "policyNamespace"
  Prometheus: "prometheusNamespace"
  Citadel:    "securityNamespace"
featureMaps:
  Base:
    alwaysEnabled: true
    Components:
      - crds
  TrafficManagement:
    Components:
      - Pilot
  Policy:
    Components:
      - Policy
  Telemetry:
    Components:
      - Telemetry
  Security:
    Components:
      - Citadel
      - CertManager
      - NodeAgent
  ConfigManagement:
    Components:
      - Galley
  AutoInjection:
    Components:
      - Injector
  Gateways:
    Components:
      - IngressGateway
      - EgressGateway
  Cni:
    Components:
      - Cni
  ThirdParty:
    Components:
      - Grafana
      - Prometheus
      - Tracing
      - PrometheusOperator
      - Kiali

componentMaps:
  crds:
    ToHelmValuesTreeRoot: "global"
    HelmSubdir:           "crds"
    AlwaysEnabled:        true
  Pilot:
    ResourceType:         "Deployment"
    ResourceName:         "istio-pilot"
    ContainerName:        "discovery"
    HelmSubdir:           "istio-control/istio-discovery"
    ToHelmValuesTreeRoot: "pilot"
  Galley:
    ResourceType:         "Deployment"
    ResourceName:         "istio-galley"
    ContainerName:        "galley"
    HelmSubdir:           "istio-control/istio-config"
    ToHelmValuesTreeRoot: "galley"
  Injector:
    ResourceType:         "Deployment"
    ResourceName:         "istio-sidecar-injector"
    ContainerName:        "sidecar-injector-webhook"
    HelmSubdir:           "istio-control/istio-autoinject"
    ToHelmValuesTreeRoot: "sidecarInjectorWebhook"
  Policy:
    ResourceType:         "Deployment"
    ResourceName:         "istio-policy"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-policy"
    ToHelmValuesTreeRoot: "mixer.policy"
  Telemetry:
    ResourceType:        "Deployment"
    ResourceName:         "istio-telemetry"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-telemetry/mixer-telemetry"
    ToHelmValuesTreeRoot: "mixer.telemetry"
  Citadel:
    ResourceType:        "Deployment"
    ResourceName:         "istio-citadel"
    ContainerName:        "citadel"
    HelmSubdir:           "security/citadel"
    ToHelmValuesTreeRoot: "security"
  NodeAgent:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-nodeagent"
    ContainerName:        "nodeagent"
    HelmSubdir:           "security/nodeagent"
    ToHelmValuesTreeRoot: "nodeagent"
  CertManager:
    ResourceType:        "Deployment"
    ResourceName:         "certmanager"
    ContainerName:        "certmanager"
    HelmSubdir:           "security/certmanager"
    ToHelmValuesTreeRoot: "certmanager"
  IngressGateway:
    ResourceType:         "Deployment"
    ResourceName:         "istio-ingressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-ingress"
    ToHelmValuesTreeRoot: "gateways.istio-ingressgateway"
  EgressGateway:
    ResourceType:         "Deployment"
    ResourceName:         "istio-egressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-egress"
    ToHelmValuesTreeRoot: "gateways.istio-egressgateway"
  Cni:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-cni-node"
    ContainerName:        "install-cni"
    HelmSubdir:           "istio-cni"
    ToHelmValuesTreeRoot: "cni"
  Tracing:
    ResourceType:         "Deployment"
    ResourceName:         "istio-tracing"
    ContainerName:        "jaeger"
    HelmSubdir:           "istio-telemetry/tracing"
    ToHelmValuesTreeRoot: "tracing.jaeger"
  PrometheusOperator:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheusOperator"
    ToHelmValuesTreeRoot: "prometheus"
  Kiali:
    ResourceType:         "Deployment"
    ResourceName:         "kiali"
    ContainerName:        "kiali"
    HelmSubdir:           "istio-telemetry/kiali"
    ToHelmValuesTreeRoot: "kiali"
  Grafana:
    ResourceType:        "Deployment"
    ResourceName:         "grafana"
    ContainerName:        "grafana"
    HelmSubdir:           "istio-telemetry/grafana"
    ToHelmValuesTreeRoot: "grafana"
  Prometheus:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheus"
    ToHelmValuesTreeRoot: "prometheus"
`)

func translateconfigTranslateconfig13YamlBytes() ([]byte, error) {
	return _translateconfigTranslateconfig13Yaml, nil
}

func translateconfigTranslateconfig13Yaml() (*asset, error) {
	bytes, err := translateconfigTranslateconfig13YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/translateConfig-1.3.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigTranslateconfig14Yaml = []byte(`apiMapping:
  Hub:
    outPath: "global.hub"
  Tag:
    outPath: "global.tag"
  K8SDefaults:
    outPath: "global.resources"
  DefaultNamespace:
    outPath: "global.istioNamespace"
  ConfigManagement.Components.Namespace:
    outPath: "global.configNamespace"
  Policy.Components.Namespace:
    outPath: "global.policyNamespace"
  Telemetry.Components.Namespace:
    outPath: "global.telemetryNamespace"
  Security.Components.Namespace:
    outPath: "global.securityNamespace"
kubernetesMapping:
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Affinity":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.affinity"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Env":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].env"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.HpaSpec":
    outPath: "[HorizontalPodAutoscaler:{{.ResourceName}}].spec"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.ImagePullPolicy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].imagePullPolicy"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.NodeSelector":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.nodeSelector"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.PodDisruptionBudget":
    outPath: "[PodDisruptionBudget:{{.ResourceName}}].spec"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.PodAnnotations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.metadata.annotations"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.PriorityClassName":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.priorityClassName."
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.ReadinessProbe":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].readinessProbe"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.ReplicaCount":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.replicas"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Resources":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].resources"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Strategy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.strategy"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Tolerations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.tolerations"
  "{{.FeatureName}}.Components.{{.ComponentName}}.K8S.Service":
    outPath: "[Service:{{.ResourceName}}].spec"
toFeature:
    Base:               Base
    Pilot:              TrafficManagement
    Galley:             ConfigManagement
    Injector:           AutoInjection
    Policy:             Policy
    Telemetry:          Telemetry
    Citadel:            Security
    CertManager:        Security
    NodeAgent:          Security
    IngressGateway:     Gateways
    EgressGateway:      Gateways
    Cni:                Cni
    CoreDNS:            CoreDNS
    Grafana:            ThirdParty
    Prometheus:         ThirdParty
    Tracing:            ThirdParty
    PrometheusOperator: ThirdParty
    Kiali:              ThirdParty
globalNamespaces:
  Pilot:      "istioNamespace"
  Galley:     "configNamespace"
  Telemetry:  "telemetryNamespace"
  Policy:     "policyNamespace"
  Prometheus: "prometheusNamespace"
  Citadel:    "securityNamespace"
featureMaps:
  Base:
    Components:
      - Base
  TrafficManagement:
    Components:
      - Pilot
  Policy:
    Components:
      - Policy
  Telemetry:
    Components:
      - Telemetry
  Security:
    Components:
      - Citadel
      - CertManager
      - NodeAgent
  ConfigManagement:
    Components:
      - Galley
  AutoInjection:
    Components:
      - Injector
  Gateways:
    Components:
      - IngressGateway
      - EgressGateway
  Cni:
    Components:
      - Cni
  CoreDNS:
    Components:
      - CoreDNS
  ThirdParty:
    Components:
      - Grafana
      - Prometheus
      - Tracing
      - PrometheusOperator
      - Kiali

componentMaps:
  Base:
    ToHelmValuesTreeRoot: "global"
    HelmSubdir:           "base"
  Pilot:
    ResourceType:         "Deployment"
    ResourceName:         "istio-pilot"
    ContainerName:        "discovery"
    HelmSubdir:           "istio-control/istio-discovery"
    ToHelmValuesTreeRoot: "pilot"
  Galley:
    ResourceType:         "Deployment"
    ResourceName:         "istio-galley"
    ContainerName:        "galley"
    HelmSubdir:           "istio-control/istio-config"
    ToHelmValuesTreeRoot: "galley"
  Injector:
    ResourceType:         "Deployment"
    ResourceName:         "istio-sidecar-injector"
    ContainerName:        "sidecar-injector-webhook"
    HelmSubdir:           "istio-control/istio-autoinject"
    ToHelmValuesTreeRoot: "sidecarInjectorWebhook"
  Policy:
    ResourceType:         "Deployment"
    ResourceName:         "istio-policy"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-policy"
    ToHelmValuesTreeRoot: "mixer.policy"
  Telemetry:
    ResourceType:        "Deployment"
    ResourceName:         "istio-telemetry"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-telemetry/mixer-telemetry"
    ToHelmValuesTreeRoot: "mixer.telemetry"
  Citadel:
    ResourceType:        "Deployment"
    ResourceName:         "istio-citadel"
    ContainerName:        "citadel"
    HelmSubdir:           "security/citadel"
    ToHelmValuesTreeRoot: "security"
  NodeAgent:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-nodeagent"
    ContainerName:        "nodeagent"
    HelmSubdir:           "security/nodeagent"
    ToHelmValuesTreeRoot: "nodeagent"
  CertManager:
    ResourceType:        "Deployment"
    ResourceName:         "certmanager"
    ContainerName:        "certmanager"
    HelmSubdir:           "security/certmanager"
    ToHelmValuesTreeRoot: "certmanager"
  IngressGateway:
    ResourceType:         "Deployment"
    ResourceName:         "istio-ingressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-ingress"
    ToHelmValuesTreeRoot: "gateways.istio-ingressgateway"
  EgressGateway:
    ResourceType:         "Deployment"
    ResourceName:         "istio-egressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-egress"
    ToHelmValuesTreeRoot: "gateways.istio-egressgateway"
  Cni:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-cni-node"
    ContainerName:        "install-cni"
    HelmSubdir:           "istio-cni"
    ToHelmValuesTreeRoot: "cni"
  CoreDNS:
    ResourceType:         "Deployment"
    ResourceName:         "istiocoredns"
    ContainerName:        "coredns"
    HelmSubdir:           "istiocoredns"
    ToHelmValuesTreeRoot: "istiocoredns"
  Tracing:
    ResourceType:         "Deployment"
    ResourceName:         "istio-tracing"
    ContainerName:        "jaeger"
    HelmSubdir:           "istio-telemetry/tracing"
    ToHelmValuesTreeRoot: "tracing.jaeger"
  PrometheusOperator:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheusOperator"
    ToHelmValuesTreeRoot: "prometheus"
  Kiali:
    ResourceType:         "Deployment"
    ResourceName:         "kiali"
    ContainerName:        "kiali"
    HelmSubdir:           "istio-telemetry/kiali"
    ToHelmValuesTreeRoot: "kiali"
  Grafana:
    ResourceType:        "Deployment"
    ResourceName:         "grafana"
    ContainerName:        "grafana"
    HelmSubdir:           "istio-telemetry/grafana"
    ToHelmValuesTreeRoot: "grafana"
  Prometheus:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheus"
    ToHelmValuesTreeRoot: "prometheus"
`)

func translateconfigTranslateconfig14YamlBytes() ([]byte, error) {
	return _translateconfigTranslateconfig14Yaml, nil
}

func translateconfigTranslateconfig14Yaml() (*asset, error) {
	bytes, err := translateconfigTranslateconfig14YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/translateConfig-1.4.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigTranslateconfig15Yaml = []byte(`apiMapping:
  Hub:
    outPath: "global.hub"
  Tag:
    outPath: "global.tag"
  K8SDefaults:
    outPath: "global.resources"
  MeshConfig.rootNamespace:
    outPath: "global.istioNamespace"
  Revision:
    outPath: "revision"
kubernetesMapping:
  "Components.{{.ComponentName}}.K8S.Affinity":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.affinity"
  "Components.{{.ComponentName}}.K8S.Env":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].env"
  "Components.{{.ComponentName}}.K8S.HpaSpec":
    outPath: "[HorizontalPodAutoscaler:{{.ResourceName}}].spec"
  "Components.{{.ComponentName}}.K8S.ImagePullPolicy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].imagePullPolicy"
  "Components.{{.ComponentName}}.K8S.NodeSelector":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.nodeSelector"
  "Components.{{.ComponentName}}.K8S.PodDisruptionBudget":
    outPath: "[PodDisruptionBudget:{{.ResourceName}}].spec"
  "Components.{{.ComponentName}}.K8S.PodAnnotations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.metadata.annotations"
  "Components.{{.ComponentName}}.K8S.PriorityClassName":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.priorityClassName."
  "Components.{{.ComponentName}}.K8S.ReadinessProbe":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].readinessProbe"
  "Components.{{.ComponentName}}.K8S.ReplicaCount":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.replicas"
  "Components.{{.ComponentName}}.K8S.Resources":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].resources"
  "Components.{{.ComponentName}}.K8S.Strategy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.strategy"
  "Components.{{.ComponentName}}.K8S.Tolerations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.tolerations"
  "Components.{{.ComponentName}}.K8S.ServiceAnnotations":
    outPath: "[Service:{{.ResourceName}}].metadata.annotations"
  "Components.{{.ComponentName}}.K8S.Service":
    outPath: "[Service:{{.ResourceName}}].spec"
globalNamespaces:
  Pilot:      "istioNamespace"
  Galley:     "configNamespace"
  Telemetry:  "telemetryNamespace"
  Policy:     "policyNamespace"
  Prometheus: "prometheusNamespace"
  Citadel:    "securityNamespace"

componentMaps:
  Base:
    ToHelmValuesTreeRoot: "global"
    HelmSubdir:           "base"
    SkipReverseTranslate: true
  Pilot:
    ResourceType:         "Deployment"
    ResourceName:         "istiod"
    ContainerName:        "discovery"
    HelmSubdir:           "istio-control/istio-discovery"
    ToHelmValuesTreeRoot: "pilot"
  Galley:
    ResourceType:         "Deployment"
    ResourceName:         "istio-galley"
    ContainerName:        "galley"
    HelmSubdir:           "istio-control/istio-config"
    ToHelmValuesTreeRoot: "galley"
  SidecarInjector:
    ResourceType:         "Deployment"
    ResourceName:         "istio-sidecar-injector"
    ContainerName:        "sidecar-injector-webhook"
    HelmSubdir:           "istio-control/istio-autoinject"
    ToHelmValuesTreeRoot: "sidecarInjectorWebhook"
    SkipReverseTranslate: true
  Policy:
    ResourceType:         "Deployment"
    ResourceName:         "istio-policy"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-policy"
    ToHelmValuesTreeRoot: "mixer.policy"
  Telemetry:
    ResourceType:        "Deployment"
    ResourceName:         "istio-telemetry"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-telemetry/mixer-telemetry"
    ToHelmValuesTreeRoot: "mixer.telemetry"
  Citadel:
    ResourceType:        "Deployment"
    ResourceName:         "istio-citadel"
    ContainerName:        "citadel"
    HelmSubdir:           "security/citadel"
    ToHelmValuesTreeRoot: "security"
  NodeAgent:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-nodeagent"
    ContainerName:        "nodeagent"
    HelmSubdir:           "security/nodeagent"
    ToHelmValuesTreeRoot: "nodeagent"
    SkipReverseTranslate: true
  CertManager:
    ResourceType:        "Deployment"
    ResourceName:         "certmanager"
    ContainerName:        "certmanager"
    HelmSubdir:           "security/certmanager"
    ToHelmValuesTreeRoot: "certmanager"
    SkipReverseTranslate: true
  IngressGateways:
    ResourceType:         "Deployment"
    ResourceName:         "istio-ingressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-ingress"
    ToHelmValuesTreeRoot: "gateways.istio-ingressgateway"
  EgressGateways:
    ResourceType:         "Deployment"
    ResourceName:         "istio-egressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-egress"
    ToHelmValuesTreeRoot: "gateways.istio-egressgateway"
  Cni:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-cni-node"
    ContainerName:        "install-cni"
    HelmSubdir:           "istio-cni"
    ToHelmValuesTreeRoot: "cni"
  Istiocoredns:
    ResourceType:         "Deployment"
    ResourceName:         "istiocoredns"
    ContainerName:        "coredns"
    HelmSubdir:           "istiocoredns"
    ToHelmValuesTreeRoot: "istiocoredns"
  Tracing:
    ResourceType:         "Deployment"
    ResourceName:         "istio-tracing"
    ContainerName:        "jaeger"
    HelmSubdir:           "istio-telemetry/tracing"
    ToHelmValuesTreeRoot: "tracing.jaeger"
  PrometheusOperator:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheusOperator"
    ToHelmValuesTreeRoot: "prometheus"
    SkipReverseTranslate: true
  Kiali:
    ResourceType:         "Deployment"
    ResourceName:         "kiali"
    ContainerName:        "kiali"
    HelmSubdir:           "istio-telemetry/kiali"
    ToHelmValuesTreeRoot: "kiali"
  Grafana:
    ResourceType:        "Deployment"
    ResourceName:         "grafana"
    ContainerName:        "grafana"
    HelmSubdir:           "istio-telemetry/grafana"
    ToHelmValuesTreeRoot: "grafana"
  Prometheus:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheus"
    ToHelmValuesTreeRoot: "prometheus"
`)

func translateconfigTranslateconfig15YamlBytes() ([]byte, error) {
	return _translateconfigTranslateconfig15Yaml, nil
}

func translateconfigTranslateconfig15Yaml() (*asset, error) {
	bytes, err := translateconfigTranslateconfig15YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/translateConfig-1.5.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigTranslateconfig16Yaml = []byte(`apiMapping:
  Hub:
    outPath: "global.hub"
  Tag:
    outPath: "global.tag"
  K8SDefaults:
    outPath: "global.resources"
  Revision:
    outPath: "revision"
  MeshConfig:
    outPath: "meshConfig"
kubernetesMapping:
  "Components.{{.ComponentName}}.K8S.Affinity":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.affinity"
  "Components.{{.ComponentName}}.K8S.Env":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].env"
  "Components.{{.ComponentName}}.K8S.HpaSpec":
    outPath: "[HorizontalPodAutoscaler:{{.ResourceName}}].spec"
  "Components.{{.ComponentName}}.K8S.ImagePullPolicy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].imagePullPolicy"
  "Components.{{.ComponentName}}.K8S.NodeSelector":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.nodeSelector"
  "Components.{{.ComponentName}}.K8S.PodDisruptionBudget":
    outPath: "[PodDisruptionBudget:{{.ResourceName}}].spec"
  "Components.{{.ComponentName}}.K8S.PodAnnotations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.metadata.annotations"
  "Components.{{.ComponentName}}.K8S.PriorityClassName":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.priorityClassName."
  "Components.{{.ComponentName}}.K8S.ReadinessProbe":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].readinessProbe"
  "Components.{{.ComponentName}}.K8S.ReplicaCount":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.replicas"
  "Components.{{.ComponentName}}.K8S.Resources":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].resources"
  "Components.{{.ComponentName}}.K8S.Strategy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.strategy"
  "Components.{{.ComponentName}}.K8S.Tolerations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.tolerations"
  "Components.{{.ComponentName}}.K8S.ServiceAnnotations":
    outPath: "[Service:{{.ResourceName}}].metadata.annotations"
  "Components.{{.ComponentName}}.K8S.Service":
    outPath: "[Service:{{.ResourceName}}].spec"
globalNamespaces:
  Pilot:      "istioNamespace"
  Galley:     "configNamespace"
  Telemetry:  "telemetryNamespace"
  Policy:     "policyNamespace"
  Prometheus: "prometheusNamespace"
  Citadel:    "securityNamespace"

componentMaps:
  Base:
    ToHelmValuesTreeRoot: "global"
    HelmSubdir:           "base"
    SkipReverseTranslate: true
  Pilot:
    ResourceType:         "Deployment"
    ResourceName:         "istiod"
    ContainerName:        "discovery"
    HelmSubdir:           "istio-control/istio-discovery"
    ToHelmValuesTreeRoot: "pilot"
  Galley:
    ResourceType:         "Deployment"
    ResourceName:         "istio-galley"
    ContainerName:        "galley"
    HelmSubdir:           "istio-control/istio-config"
    ToHelmValuesTreeRoot: "galley"
  SidecarInjector:
    ResourceType:         "Deployment"
    ResourceName:         "istio-sidecar-injector"
    ContainerName:        "sidecar-injector-webhook"
    HelmSubdir:           "istio-control/istio-autoinject"
    ToHelmValuesTreeRoot: "sidecarInjectorWebhook"
    SkipReverseTranslate: true
  Policy:
    ResourceType:         "Deployment"
    ResourceName:         "istio-policy"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-policy"
    ToHelmValuesTreeRoot: "mixer.policy"
  Telemetry:
    ResourceType:        "Deployment"
    ResourceName:         "istio-telemetry"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-telemetry/mixer-telemetry"
    ToHelmValuesTreeRoot: "mixer.telemetry"
  Citadel:
    ResourceType:        "Deployment"
    ResourceName:         "istio-citadel"
    ContainerName:        "citadel"
    HelmSubdir:           "security/citadel"
    ToHelmValuesTreeRoot: "security"
  NodeAgent:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-nodeagent"
    ContainerName:        "nodeagent"
    HelmSubdir:           "security/nodeagent"
    ToHelmValuesTreeRoot: "nodeagent"
    SkipReverseTranslate: true
  IngressGateways:
    ResourceType:         "Deployment"
    ResourceName:         "istio-ingressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-ingress"
    ToHelmValuesTreeRoot: "gateways.istio-ingressgateway"
  EgressGateways:
    ResourceType:         "Deployment"
    ResourceName:         "istio-egressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-egress"
    ToHelmValuesTreeRoot: "gateways.istio-egressgateway"
  Cni:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-cni-node"
    ContainerName:        "install-cni"
    HelmSubdir:           "istio-cni"
    ToHelmValuesTreeRoot: "cni"
  Istiocoredns:
    ResourceType:         "Deployment"
    ResourceName:         "istiocoredns"
    ContainerName:        "coredns"
    HelmSubdir:           "istiocoredns"
    ToHelmValuesTreeRoot: "istiocoredns"
  Tracing:
    ResourceType:         "Deployment"
    ResourceName:         "istio-tracing"
    ContainerName:        "jaeger"
    HelmSubdir:           "istio-telemetry/tracing"
    ToHelmValuesTreeRoot: "tracing.jaeger"
  PrometheusOperator:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheusOperator"
    ToHelmValuesTreeRoot: "prometheus"
    SkipReverseTranslate: true
  Kiali:
    ResourceType:         "Deployment"
    ResourceName:         "kiali"
    ContainerName:        "kiali"
    HelmSubdir:           "istio-telemetry/kiali"
    ToHelmValuesTreeRoot: "kiali"
  Grafana:
    ResourceType:        "Deployment"
    ResourceName:         "grafana"
    ContainerName:        "grafana"
    HelmSubdir:           "istio-telemetry/grafana"
    ToHelmValuesTreeRoot: "grafana"
  Prometheus:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheus"
    ToHelmValuesTreeRoot: "prometheus"
`)

func translateconfigTranslateconfig16YamlBytes() ([]byte, error) {
	return _translateconfigTranslateconfig16Yaml, nil
}

func translateconfigTranslateconfig16Yaml() (*asset, error) {
	bytes, err := translateconfigTranslateconfig16YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/translateConfig-1.6.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _translateconfigTranslateconfig17Yaml = []byte(`apiMapping:
  Hub:
    outPath: "global.hub"
  Tag:
    outPath: "global.tag"
  K8SDefaults:
    outPath: "global.resources"
  Revision:
    outPath: "revision"
  MeshConfig:
    outPath: "meshConfig"
kubernetesMapping:
  "Components.{{.ComponentName}}.K8S.Affinity":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.affinity"
  "Components.{{.ComponentName}}.K8S.Env":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].env"
  "Components.{{.ComponentName}}.K8S.HpaSpec":
    outPath: "[HorizontalPodAutoscaler:{{.ResourceName}}].spec"
  "Components.{{.ComponentName}}.K8S.ImagePullPolicy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].imagePullPolicy"
  "Components.{{.ComponentName}}.K8S.NodeSelector":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.nodeSelector"
  "Components.{{.ComponentName}}.K8S.PodDisruptionBudget":
    outPath: "[PodDisruptionBudget:{{.ResourceName}}].spec"
  "Components.{{.ComponentName}}.K8S.PodAnnotations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.metadata.annotations"
  "Components.{{.ComponentName}}.K8S.PriorityClassName":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.priorityClassName."
  "Components.{{.ComponentName}}.K8S.ReadinessProbe":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].readinessProbe"
  "Components.{{.ComponentName}}.K8S.ReplicaCount":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.replicas"
  "Components.{{.ComponentName}}.K8S.Resources":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.containers.[name:{{.ContainerName}}].resources"
  "Components.{{.ComponentName}}.K8S.Strategy":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.strategy"
  "Components.{{.ComponentName}}.K8S.Tolerations":
    outPath: "[{{.ResourceType}}:{{.ResourceName}}].spec.template.spec.tolerations"
  "Components.{{.ComponentName}}.K8S.ServiceAnnotations":
    outPath: "[Service:{{.ResourceName}}].metadata.annotations"
  "Components.{{.ComponentName}}.K8S.Service":
    outPath: "[Service:{{.ResourceName}}].spec"
globalNamespaces:
  Pilot:      "istioNamespace"
  Galley:     "configNamespace"
  Telemetry:  "telemetryNamespace"
  Policy:     "policyNamespace"
  Prometheus: "prometheusNamespace"
  Citadel:    "securityNamespace"

componentMaps:
  Base:
    ToHelmValuesTreeRoot: "global"
    HelmSubdir:           "base"
    SkipReverseTranslate: true
  Pilot:
    ResourceType:         "Deployment"
    ResourceName:         "istiod"
    ContainerName:        "discovery"
    HelmSubdir:           "istio-control/istio-discovery"
    ToHelmValuesTreeRoot: "pilot"
  Galley:
    ResourceType:         "Deployment"
    ResourceName:         "istio-galley"
    ContainerName:        "galley"
    HelmSubdir:           "istio-control/istio-config"
    ToHelmValuesTreeRoot: "galley"
  SidecarInjector:
    ResourceType:         "Deployment"
    ResourceName:         "istio-sidecar-injector"
    ContainerName:        "sidecar-injector-webhook"
    HelmSubdir:           "istio-control/istio-autoinject"
    ToHelmValuesTreeRoot: "sidecarInjectorWebhook"
    SkipReverseTranslate: true
  Policy:
    ResourceType:         "Deployment"
    ResourceName:         "istio-policy"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-policy"
    ToHelmValuesTreeRoot: "mixer.policy"
  Telemetry:
    ResourceType:        "Deployment"
    ResourceName:         "istio-telemetry"
    ContainerName:        "mixer"
    HelmSubdir:           "istio-telemetry/mixer-telemetry"
    ToHelmValuesTreeRoot: "mixer.telemetry"
  Citadel:
    ResourceType:        "Deployment"
    ResourceName:         "istio-citadel"
    ContainerName:        "citadel"
    HelmSubdir:           "security/citadel"
    ToHelmValuesTreeRoot: "security"
  NodeAgent:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-nodeagent"
    ContainerName:        "nodeagent"
    HelmSubdir:           "security/nodeagent"
    ToHelmValuesTreeRoot: "nodeagent"
    SkipReverseTranslate: true
  IngressGateways:
    ResourceType:         "Deployment"
    ResourceName:         "istio-ingressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-ingress"
    ToHelmValuesTreeRoot: "gateways.istio-ingressgateway"
  EgressGateways:
    ResourceType:         "Deployment"
    ResourceName:         "istio-egressgateway"
    ContainerName:        "istio-proxy"
    HelmSubdir:           "gateways/istio-egress"
    ToHelmValuesTreeRoot: "gateways.istio-egressgateway"
  Cni:
    ResourceType:         "DaemonSet"
    ResourceName:         "istio-cni-node"
    ContainerName:        "install-cni"
    HelmSubdir:           "istio-cni"
    ToHelmValuesTreeRoot: "cni"
  Istiocoredns:
    ResourceType:         "Deployment"
    ResourceName:         "istiocoredns"
    ContainerName:        "coredns"
    HelmSubdir:           "istiocoredns"
    ToHelmValuesTreeRoot: "istiocoredns"
  Tracing:
    ResourceType:         "Deployment"
    ResourceName:         "istio-tracing"
    ContainerName:        "jaeger"
    HelmSubdir:           "istio-telemetry/tracing"
    ToHelmValuesTreeRoot: "tracing.jaeger"
  PrometheusOperator:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheusOperator"
    ToHelmValuesTreeRoot: "prometheus"
    SkipReverseTranslate: true
  Kiali:
    ResourceType:         "Deployment"
    ResourceName:         "kiali"
    ContainerName:        "kiali"
    HelmSubdir:           "istio-telemetry/kiali"
    ToHelmValuesTreeRoot: "kiali"
  Grafana:
    ResourceType:        "Deployment"
    ResourceName:         "grafana"
    ContainerName:        "grafana"
    HelmSubdir:           "istio-telemetry/grafana"
    ToHelmValuesTreeRoot: "grafana"
  Prometheus:
    ResourceType:         "Deployment"
    ResourceName:         "prometheus"
    ContainerName:        "prometheus"
    HelmSubdir:           "istio-telemetry/prometheus"
    ToHelmValuesTreeRoot: "prometheus"`)

func translateconfigTranslateconfig17YamlBytes() ([]byte, error) {
	return _translateconfigTranslateconfig17Yaml, nil
}

func translateconfigTranslateconfig17Yaml() (*asset, error) {
	bytes, err := translateconfigTranslateconfig17YamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "translateConfig/translateConfig-1.7.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _versionsYaml = []byte(`- operatorVersion: 1.3.0
  supportedIstioVersions: 1.3.0
  recommendedIstioVersions: 1.3.0
- operatorVersion: 1.3.1
  supportedIstioVersions: ">=1.3.0,<=1.3.1"
  recommendedIstioVersions: 1.3.1
- operatorVersion: 1.3.2
  supportedIstioVersions: ">=1.3.0,<=1.3.2"
  recommendedIstioVersions: 1.3.2
- operatorVersion: 1.3.3
  supportedIstioVersions: ">=1.3.0,<=1.3.3"
  recommendedIstioVersions: 1.3.3
- operatorVersion: 1.3.4
  supportedIstioVersions: ">=1.3.0,<=1.3.4"
  recommendedIstioVersions: 1.3.4
- operatorVersion: 1.3.5
  supportedIstioVersions: ">=1.3.0,<=1.3.5"
  recommendedIstioVersions: 1.3.5
- operatorVersion: 1.3.6
  supportedIstioVersions: ">=1.3.0,<=1.3.6"
  recommendedIstioVersions: 1.3.6
- operatorVersion: 1.3.7
  operatorVersionRange: ">=1.3.7,<1.4.0"
  supportedIstioVersions: ">=1.3.0,<1.4.0"
  recommendedIstioVersions: 1.3.7
- operatorVersion: 1.4.0
  supportedIstioVersions: ">=1.3.3, <1.6"
  recommendedIstioVersions: 1.4.0
- operatorVersion: 1.4.1
  supportedIstioVersions: ">=1.3.3, <1.6"
  recommendedIstioVersions: 1.4.1
- operatorVersion: 1.4.2
  supportedIstioVersions: ">=1.3.3, <1.6"
  recommendedIstioVersions: 1.4.2
- operatorVersion: 1.4.3
  operatorVersionRange: ">=1.4.3,<1.5.0"
  supportedIstioVersions: ">=1.3.3, <1.6"
  recommendedIstioVersions: 1.4.3
- operatorVersion: 1.4.4
  operatorVersionRange: ">=1.4.4,<1.5.0"
  supportedIstioVersions: ">=1.3.3, <1.6"
  recommendedIstioVersions: 1.4.4
- operatorVersion: 1.5.0
  operatorVersionRange: ">=1.5.0,<1.6.0"
  supportedIstioVersions: ">=1.4.0, <1.6"
  recommendedIstioVersions: 1.5.0
  k8sClientVersionRange: ">=1.14"
  k8sServerVersionRange: ">=1.14"
- operatorVersion: 1.6.0
  operatorVersionRange: ">=1.6.0,<1.7.0"
  supportedIstioVersions: ">=1.5.0, <1.7"
  recommendedIstioVersions: 1.6.0
  k8sClientVersionRange: ">=1.14"
  k8sServerVersionRange: ">=1.14"
- operatorVersion: 1.7.0
  operatorVersionRange: ">=1.7.0,<1.8.0"
  supportedIstioVersions: ">=1.6.0, <1.8"
  recommendedIstioVersions: 1.7.0
  k8sClientVersionRange: ">=1.15"
  k8sServerVersionRange: ">=1.15"
`)

func versionsYamlBytes() ([]byte, error) {
	return _versionsYaml, nil
}

func versionsYaml() (*asset, error) {
	bytes, err := versionsYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "versions.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"examples/customresource/istio_v1alpha1_istiooperator_cr.yaml":  examplesCustomresourceIstio_v1alpha1_istiooperator_crYaml,
	"examples/multicluster/values-istio-multicluster-gateways.yaml": examplesMulticlusterValuesIstioMulticlusterGatewaysYaml,
	"examples/multicluster/values-istio-multicluster-primary.yaml":  examplesMulticlusterValuesIstioMulticlusterPrimaryYaml,
	"examples/user-gateway/ingress-gateway-only.yaml":               examplesUserGatewayIngressGatewayOnlyYaml,
	"examples/vm/values-istio-meshexpansion-gateways.yaml":          examplesVmValuesIstioMeshexpansionGatewaysYaml,
	"examples/vm/values-istio-meshexpansion.yaml":                   examplesVmValuesIstioMeshexpansionYaml,
	"translateConfig/names-1.5.yaml":                                translateconfigNames15Yaml,
	"translateConfig/names-1.6.yaml":                                translateconfigNames16Yaml,
	"translateConfig/names-1.7.yaml":                                translateconfigNames17Yaml,
	"translateConfig/reverseTranslateConfig-1.4.yaml":               translateconfigReversetranslateconfig14Yaml,
	"translateConfig/reverseTranslateConfig-1.5.yaml":               translateconfigReversetranslateconfig15Yaml,
	"translateConfig/reverseTranslateConfig-1.6.yaml":               translateconfigReversetranslateconfig16Yaml,
	"translateConfig/reverseTranslateConfig-1.7.yaml":               translateconfigReversetranslateconfig17Yaml,
	"translateConfig/translateConfig-1.3.yaml":                      translateconfigTranslateconfig13Yaml,
	"translateConfig/translateConfig-1.4.yaml":                      translateconfigTranslateconfig14Yaml,
	"translateConfig/translateConfig-1.5.yaml":                      translateconfigTranslateconfig15Yaml,
	"translateConfig/translateConfig-1.6.yaml":                      translateconfigTranslateconfig16Yaml,
	"translateConfig/translateConfig-1.7.yaml":                      translateconfigTranslateconfig17Yaml,
	"versions.yaml":                                                 versionsYaml,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"examples": &bintree{nil, map[string]*bintree{
		"customresource": &bintree{nil, map[string]*bintree{
			"istio_v1alpha1_istiooperator_cr.yaml": &bintree{examplesCustomresourceIstio_v1alpha1_istiooperator_crYaml, map[string]*bintree{}},
		}},
		"multicluster": &bintree{nil, map[string]*bintree{
			"values-istio-multicluster-gateways.yaml": &bintree{examplesMulticlusterValuesIstioMulticlusterGatewaysYaml, map[string]*bintree{}},
			"values-istio-multicluster-primary.yaml":  &bintree{examplesMulticlusterValuesIstioMulticlusterPrimaryYaml, map[string]*bintree{}},
		}},
		"user-gateway": &bintree{nil, map[string]*bintree{
			"ingress-gateway-only.yaml": &bintree{examplesUserGatewayIngressGatewayOnlyYaml, map[string]*bintree{}},
		}},
		"vm": &bintree{nil, map[string]*bintree{
			"values-istio-meshexpansion-gateways.yaml": &bintree{examplesVmValuesIstioMeshexpansionGatewaysYaml, map[string]*bintree{}},
			"values-istio-meshexpansion.yaml":          &bintree{examplesVmValuesIstioMeshexpansionYaml, map[string]*bintree{}},
		}},
	}},
	"translateConfig": &bintree{nil, map[string]*bintree{
		"names-1.5.yaml":                  &bintree{translateconfigNames15Yaml, map[string]*bintree{}},
		"names-1.6.yaml":                  &bintree{translateconfigNames16Yaml, map[string]*bintree{}},
		"names-1.7.yaml":                  &bintree{translateconfigNames17Yaml, map[string]*bintree{}},
		"reverseTranslateConfig-1.4.yaml": &bintree{translateconfigReversetranslateconfig14Yaml, map[string]*bintree{}},
		"reverseTranslateConfig-1.5.yaml": &bintree{translateconfigReversetranslateconfig15Yaml, map[string]*bintree{}},
		"reverseTranslateConfig-1.6.yaml": &bintree{translateconfigReversetranslateconfig16Yaml, map[string]*bintree{}},
		"reverseTranslateConfig-1.7.yaml": &bintree{translateconfigReversetranslateconfig17Yaml, map[string]*bintree{}},
		"translateConfig-1.3.yaml":        &bintree{translateconfigTranslateconfig13Yaml, map[string]*bintree{}},
		"translateConfig-1.4.yaml":        &bintree{translateconfigTranslateconfig14Yaml, map[string]*bintree{}},
		"translateConfig-1.5.yaml":        &bintree{translateconfigTranslateconfig15Yaml, map[string]*bintree{}},
		"translateConfig-1.6.yaml":        &bintree{translateconfigTranslateconfig16Yaml, map[string]*bintree{}},
		"translateConfig-1.7.yaml":        &bintree{translateconfigTranslateconfig17Yaml, map[string]*bintree{}},
	}},
	"versions.yaml": &bintree{versionsYaml, map[string]*bintree{}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}
