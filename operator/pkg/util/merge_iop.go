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

package util

import (
	"fmt"
	"strings"

	yaml2 "github.com/ghodss/yaml"
	protobuf "github.com/gogo/protobuf/types"
	v11 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"

	v1alpha13 "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/api/operator/v1alpha1"
	v1alpha12 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
)

// Partially mirrored from istio/api and operator/pkg/api (for values).
// Struct tags are required to use k8s strategic merge library. It would be possible
// to add these to source protos but because the values field is defined as
// map[string]interface{} here (and similar for MeshConfig in v1alpha1.Values)
// that alone would not be sufficient.
// Only non-scalar types require tags, therefore most fields are omitted here.
type iopMergeStructType struct {
	v11.ObjectMeta `json:"metadata" patchStrategy:"merge"`
	Spec           istioOperatorSpec `json:"spec" patchStrategy:"merge"`
}

type istioOperatorSpec struct {
	MeshConfig      *meshConfig            `json:"meshConfig" patchStrategy:"merge"`
	Components      *istioComponentSetSpec `json:"components" patchStrategy:"merge"`
	AddonComponents *externalComponentSpec `json:"addonComponents" patchStrategy:"merge"`
	Values          *values                `json:"values" patchStrategy:"merge"`
}

type istioComponentSetSpec struct {
	Base            *baseComponentSpec `json:"base" patchStrategy:"merge"`
	Pilot           *componentSpec     `json:"pilot" patchStrategy:"merge"`
	Policy          *componentSpec     `json:"policy" patchStrategy:"merge"`
	Telemetry       *componentSpec     `json:"telemetry" patchStrategy:"merge"`
	Cni             *componentSpec     `json:"cni" patchStrategy:"merge"`
	IstiodRemote    *componentSpec     `json:"istiodRemote" patchStrategy:"merge"`
	IngressGateways []*gatewaySpec     `json:"ingressGateways" patchStrategy:"merge" patchMergeKey:"name"`
	EgressGateways  []*gatewaySpec     `json:"egressGateways" patchStrategy:"merge" patchMergeKey:"name"`
}

type baseComponentSpec struct {
	K8S *v1alpha1.KubernetesResourcesSpec `json:"k8s" patchStrategy:"merge"`
}

type componentSpec struct {
	K8S *v1alpha1.KubernetesResourcesSpec `json:"k8s" patchStrategy:"merge"`
}

type gatewaySpec struct {
	K8S *v1alpha1.KubernetesResourcesSpec `json:"k8s" patchStrategy:"merge"`
}

type externalComponentSpec struct {
	IstioCoreDNS *componentSpec `json:"istiocoredns" patchStrategy:"merge"`
}

type values struct {
	Cni                    *v1alpha12.CNIConfig             `json:"cni" patchStrategy:"merge"`
	Istiocoredns           *v1alpha12.CoreDNSConfig         `json:"istiocoredns" patchStrategy:"merge"`
	Gateways               *gatewaysConfig                  `json:"gateways" patchStrategy:"merge"`
	Global                 *v1alpha12.GlobalConfig          `json:"global" patchStrategy:"merge"`
	Pilot                  *v1alpha12.PilotConfig           `json:"pilot" patchStrategy:"merge"`
	Telemetry              *telemetryConfig                 `json:"telemetry" patchStrategy:"merge"`
	SidecarInjectorWebhook *v1alpha12.SidecarInjectorConfig `json:"sidecarInjectorWebhook" patchStrategy:"merge"`
	ClusterResources       *protobuf.BoolValue              `json:"clusterResources" patchStrategy:"merge"`
	IstioCni               *v1alpha12.CNIConfig             `json:"istio_cni" patchStrategy:"merge"`
	MeshConfig             *meshConfig                      `json:"meshConfig" patchStrategy:"merge"`
	Base                   *v1alpha12.BaseConfig            `json:"base" patchStrategy:"merge"`
	IstiodRemote           *v1alpha12.IstiodRemoteConfig    `json:"istiodRemote" patchStrategy:"merge"`
}

type gatewaysConfig struct {
	IstioEgressgateway  *egressGatewayConfig  `json:"istio-egressgateway" patchStrategy:"merge"`
	IstioIngressgateway *ingressGatewayConfig `json:"istio-ingressgateway" patchStrategy:"merge"`
}

// Configuration for an ingress gateway.
type ingressGatewayConfig struct {
	CPU                *v1alpha12.CPUTargetUtilizationConfig `json:"cpu" patchStrategy:"merge"`
	MeshExpansionPorts []*v1alpha12.PortsConfig              `json:"meshExpansionPorts" patchStrategy:"merge"`
	Ports              []*v1alpha12.PortsConfig              `json:"ports" patchStrategy:"merge"`
	SecretVolumes      []*v1alpha12.SecretVolume             `json:"secretVolumes" patchStrategy:"merge"`
	Zvpn               *v1alpha12.IngressGatewayZvpnConfig   `json:"zvpn" patchStrategy:"merge"`
}

type egressGatewayConfig struct {
	Ports         []*v1alpha12.PortsConfig  `json:"ports" patchStrategy:"merge"`
	Resources     *v1alpha12.Resources      `json:"resources" patchStrategy:"merge"`
	SecretVolumes []*v1alpha12.SecretVolume `json:"secretVolumes" patchStrategy:"merge"`
	Zvpn          *v1alpha12.ZeroVPNConfig  `json:"zvpn" patchStrategy:"merge"`
}

type meshConfig struct {
	TCPKeepalive                   *v1alpha3.ConnectionPoolSettings_TCPSettings_TcpKeepalive `json:"tcpKeepalive" patchStrategy:"merge"`
	DefaultConfig                  *v1alpha13.ProxyConfig                                    `json:"defaultConfig" patchStrategy:"merge"`
	ConfigSources                  []*v1alpha13.ConfigSource                                 `json:"configSources" patchStrategy:"merge"`
	TrustDomainAliases             []string                                                  `json:"trustDomainAliases" patchStrategy:"merge"`
	DefaultServiceExportTo         []string                                                  `json:"defaultServiceExportTo" patchStrategy:"merge"`
	DefaultVirtualServiceExportTo  []string                                                  `json:"defaultVirtualServiceExportTo" patchStrategy:"merge"`
	DefaultDestinationRuleExportTo []string                                                  `json:"defaultDestinationRuleExportTo" patchStrategy:"merge"`
	LocalityLbSetting              *v1alpha3.LocalityLoadBalancerSetting                     `json:"localityLbSetting" patchStrategy:"merge"`
	Certificates                   []*v1alpha13.Certificate                                  `json:"certificates" patchStrategy:"merge"`
	ThriftConfig                   *v1alpha13.MeshConfig_ThriftConfig                        `json:"thriftConfig" patchStrategy:"merge"`
	ServiceSettings                []*v1alpha13.MeshConfig_ServiceSettings                   `json:"serviceSettings" patchStrategy:"merge"`
}

type telemetryConfig struct {
	V2 *telemetryV2Config `json:"v2" patchStrategy:"merge"`
}

type telemetryV2Config struct {
	MetadataExchange *v1alpha12.TelemetryV2MetadataExchangeConfig      `json:"metadataExchange" patchStrategy:"merge"`
	Prometheus       *v1alpha12.TelemetryV2PrometheusConfig            `json:"prometheus" patchStrategy:"merge"`
	Stackdriver      *v1alpha12.TelemetryV2StackDriverConfig           `json:"stackdriver" patchStrategy:"merge"`
	AccessLogPolicy  *v1alpha12.TelemetryV2AccessLogPolicyFilterConfig `json:"accessLogPolicy" patchStrategy:"merge"`
}

var (
	iopMergeStruct iopMergeStructType
)

// OverlayIOP overlays over base using JSON strategic merge.
func OverlayIOP(base, overlay string) (string, error) {
	if strings.TrimSpace(base) == "" {
		return overlay, nil
	}
	if strings.TrimSpace(overlay) == "" {
		return base, nil
	}
	bj, err := yaml2.YAMLToJSON([]byte(base))
	if err != nil {
		return "", fmt.Errorf("yamlToJSON error in base: %s\n%s", err, bj)
	}
	oj, err := yaml2.YAMLToJSON([]byte(overlay))
	if err != nil {
		return "", fmt.Errorf("yamlToJSON error in overlay: %s\n%s", err, oj)
	}
	if base == "" {
		bj = []byte("{}")
	}
	if overlay == "" {
		oj = []byte("{}")
	}

	merged, err := strategicpatch.StrategicMergePatch(bj, oj, &iopMergeStruct)
	if err != nil {
		return "", fmt.Errorf("json merge error (%s) for base object: \n%s\n override object: \n%s", err, bj, oj)
	}

	my, err := yaml2.JSONToYAML(merged)
	if err != nil {
		return "", fmt.Errorf("jsonToYAML error (%s) for merged object: \n%s", err, merged)
	}

	return string(my), nil
}
