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

	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"
	v11 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	yaml2 "sigs.k8s.io/yaml"

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
	MeshConfig *meshConfig            `json:"meshConfig" patchStrategy:"merge"`
	Components *istioComponentSetSpec `json:"components" patchStrategy:"merge"`
	Values     *values                `json:"values" patchStrategy:"merge"`
}

type istioComponentSetSpec struct {
	Base            *baseComponentSpec `json:"base" patchStrategy:"merge"`
	Pilot           *componentSpec     `json:"pilot" patchStrategy:"merge"`
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
	Label map[string]string                 `json:"label" patchStrategy:"merge"`
	K8S   *v1alpha1.KubernetesResourcesSpec `json:"k8s" patchStrategy:"merge"`
}

type values struct {
	Cni                    *v1alpha12.CNIConfig             `json:"cni" patchStrategy:"merge"`
	Gateways               *gatewaysConfig                  `json:"gateways" patchStrategy:"merge"`
	Global                 *v1alpha12.GlobalConfig          `json:"global" patchStrategy:"merge"`
	Pilot                  *v1alpha12.PilotConfig           `json:"pilot" patchStrategy:"merge"`
	Telemetry              *telemetryConfig                 `json:"telemetry" patchStrategy:"merge"`
	SidecarInjectorWebhook *v1alpha12.SidecarInjectorConfig `json:"sidecarInjectorWebhook" patchStrategy:"merge"`
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
	Env                              map[string]any                        `json:"env" patchStrategy:"merge"`
	Labels                           map[string]string                     `json:"labels" patchStrategy:"merge"`
	CPU                              *v1alpha12.CPUTargetUtilizationConfig `json:"cpu" patchStrategy:"replace"`
	LoadBalancerSourceRanges         []string                              `json:"loadBalancerSourceRanges" patchStrategy:"replace"`
	NodeSelector                     map[string]any                        `json:"nodeSelector" patchStrategy:"merge"`
	PodAntiAffinityLabelSelector     []map[string]any                      `json:"podAntiAffinityLabelSelector" patchStrategy:"replace"`
	PodAntiAffinityTermLabelSelector []map[string]any                      `json:"podAntiAffinityTermLabelSelector" patchStrategy:"replace"`
	PodAnnotations                   map[string]any                        `json:"podAnnotations" patchStrategy:"merge"`
	MeshExpansionPorts               []*v1alpha12.PortsConfig              `json:"meshExpansionPorts" patchStrategy:"merge" patchMergeKey:"name"`
	Ports                            []*v1alpha12.PortsConfig              `json:"ports" patchStrategy:"merge" patchMergeKey:"name"`
	Resources                        *resources                            `json:"resources" patchStrategy:"merge"`
	SecretVolumes                    []*v1alpha12.SecretVolume             `json:"secretVolumes" patchStrategy:"merge" patchMergeKey:"name"`
	ServiceAnnotations               map[string]any                        `json:"serviceAnnotations" patchStrategy:"merge"`
	Tolerations                      []map[string]any                      `json:"tolerations" patchStrategy:"replace"`
	IngressPorts                     []map[string]any                      `json:"ingressPorts" patchStrategy:"replace"`
	AdditionalContainers             []map[string]any                      `json:"additionalContainers" patchStrategy:"replace"`
	ConfigVolumes                    []map[string]any                      `json:"configVolumes" patchStrategy:"replace"`
	Zvpn                             *v1alpha12.IngressGatewayZvpnConfig   `json:"zvpn" patchStrategy:"merge"`
}

type resources struct {
	Limits   map[string]string `json:"limits" patchStrategy:"merge"`
	Requests map[string]string `json:"requests" patchStrategy:"merge"`
}

type egressGatewayConfig struct {
	Env                              map[string]any            `json:"env" patchStrategy:"merge"`
	Labels                           map[string]string         `json:"labels" patchStrategy:"merge"`
	NodeSelector                     map[string]any            `json:"nodeSelector" patchStrategy:"merge"`
	PodAntiAffinityLabelSelector     []map[string]any          `json:"podAntiAffinityLabelSelector" patchStrategy:"replace"`
	PodAntiAffinityTermLabelSelector []map[string]any          `json:"podAntiAffinityTermLabelSelector" patchStrategy:"replace"`
	PodAnnotations                   map[string]any            `json:"podAnnotations" patchStrategy:"merge"`
	Ports                            []*v1alpha12.PortsConfig  `json:"ports" patchStrategy:"merge" patchMergeKey:"name"`
	Resources                        *resources                `json:"resources" patchStrategy:"merge"`
	SecretVolumes                    []*v1alpha12.SecretVolume `json:"secretVolumes" patchStrategy:"merge" patchMergeKey:"name"`
	Tolerations                      []map[string]any          `json:"tolerations" patchStrategy:"replace"`
	ConfigVolumes                    []map[string]any          `json:"configVolumes" patchStrategy:"replace"`
	AdditionalContainers             []map[string]any          `json:"additionalContainers" patchStrategy:"replace"`
	Zvpn                             *v1alpha12.ZeroVPNConfig  `json:"zvpn" patchStrategy:"replace"`
}

type meshConfig struct {
	ConnectTimeout                 *durationpb.Duration                                      `json:"connectTimeout" patchStrategy:"replace"`
	ProtocolDetectionTimeout       *durationpb.Duration                                      `json:"protocolDetectionTimeout" patchStrategy:"replace"`
	RdsRefreshDelay                *durationpb.Duration                                      `json:"rdsRefreshDelay" patchStrategy:"replace"`
	EnableAutoMtls                 *wrappers.BoolValue                                       `json:"enableAutoMtls" patchStrategy:"replace"`
	EnablePrometheusMerge          *wrappers.BoolValue                                       `json:"enablePrometheusMerge" patchStrategy:"replace"`
	OutboundTrafficPolicy          *v1alpha13.MeshConfig_OutboundTrafficPolicy               `json:"outboundTrafficPolicy" patchStrategy:"merge"`
	TCPKeepalive                   *v1alpha3.ConnectionPoolSettings_TCPSettings_TcpKeepalive `json:"tcpKeepalive" patchStrategy:"merge"`
	DefaultConfig                  *proxyConfig                                              `json:"defaultConfig" patchStrategy:"merge"`
	ConfigSources                  []*v1alpha13.ConfigSource                                 `json:"configSources" patchStrategy:"merge" patchMergeKey:"address"`
	TrustDomainAliases             []string                                                  `json:"trustDomainAliases" patchStrategy:"merge"`
	DefaultServiceExportTo         []string                                                  `json:"defaultServiceExportTo" patchStrategy:"merge"`
	DefaultVirtualServiceExportTo  []string                                                  `json:"defaultVirtualServiceExportTo" patchStrategy:"merge"`
	DefaultDestinationRuleExportTo []string                                                  `json:"defaultDestinationRuleExportTo" patchStrategy:"merge"`
	LocalityLbSetting              *v1alpha3.LocalityLoadBalancerSetting                     `json:"localityLbSetting" patchStrategy:"merge"`
	DNSRefreshRate                 *durationpb.Duration                                      `json:"dnsRefreshRate" patchStrategy:"replace"`
	Certificates                   []*v1alpha13.Certificate                                  `json:"certificates" patchStrategy:"merge" patchMergeKey:"secretName"`
	ThriftConfig                   *meshConfigThriftConfig                                   `json:"thriftConfig" patchStrategy:"merge"`
	ServiceSettings                []*meshConfigServiceSettings                              `json:"serviceSettings" patchStrategy:"replace"`
	DefaultProviders               *meshConfigDefaultProviders                               `json:"defaultProviders" patchStrategy:"merge"`
	ExtensionProviders             []*meshConfigExtensionProvider                            `json:"extensionProviders" patchStrategy:"merge" patchMergeKey:"name"`
}

type (
	meshConfigDefaultProviders struct {
		AccessLogging []struct{} `json:"accessLogging"`
		Tracing       []struct{} `json:"tracing"`
		Metrics       []struct{} `json:"metrics"`
	}
	meshConfigExtensionProvider struct {
		Name               string   `json:"string"`
		EnvoyOtelAls       struct{} `json:"envoyOtelAls"`
		Prometheus         struct{} `json:"prometheus"`
		EnvoyFileAccessLog struct{} `json:"envoyFileAccessLog"`
		Stackdriver        struct{} `json:"stackdriver"`
		EnvoyExtAuthzHTTP  struct{} `json:"envoyExtAuthzHttp"`
		EnvoyExtAuthzGrpc  struct{} `json:"envoyExtAuthzGrpc"`
		Zipkin             struct{} `json:"zipkin"`
		Lightstep          struct{} `json:"lightstep"`
		Datadog            struct{} `json:"datadog"`
		Opencensus         struct{} `json:"opencensus"`
		Skywalking         struct{} `json:"skywalking"`
		EnvoyHTTPAls       struct{} `json:"envoyHttpAls"`
		EnvoyTCPAls        struct{} `json:"envoyTcpAls"`
		OpenTelemetry      struct{} `json:"opentelemetry"`
	}
	clusterName struct {
		ServiceCluster     *v1alpha13.ProxyConfig_ServiceCluster      `json:"serviceCluster,omitempty"`
		TracingServiceName *v1alpha13.ProxyConfig_TracingServiceName_ `json:"tracingServiceName,omitempty"`
	}
)

type meshConfigThriftConfig struct {
	RateLimitTimeout *durationpb.Duration `json:"rateLimitTimeout" patchStrategy:"replace"`
}

type proxyConfig struct {
	DrainDuration                  *durationpb.Duration                    `json:"drainDuration" patchStrategy:"replace"`
	ParentShutdownDuration         *durationpb.Duration                    `json:"parentShutdownDuration" patchStrategy:"replace"`
	DiscoveryRefreshDelay          *durationpb.Duration                    `json:"discoveryRefreshDelay" patchStrategy:"replace"`
	TerminationDrainDuration       *durationpb.Duration                    `json:"terminationDrainDuration" patchStrategy:"replace"`
	Concurrency                    *wrappers.Int32Value                    `json:"concurrency" patchStrategy:"replace"`
	ConfigSources                  []*v1alpha13.ConfigSource               `json:"configSources" patchStrategy:"replace"`
	ClusterName                    *clusterName                            `json:"clusterName" patchStrategy:"replace"`
	TrustDomainAliases             []string                                `json:"trustDomainAliases" patchStrategy:"replace"`
	DefaultServiceExportTo         []string                                `json:"defaultServiceExportTo" patchStrategy:"replace"`
	DefaultVirtualServiceExportTo  []string                                `json:"defaultVirtualServiceExportTo" patchStrategy:"replace"`
	DefaultDestinationRuleExportTo []string                                `json:"defaultDestinationRuleExportTo" patchStrategy:"replace"`
	LocalityLbSetting              *v1alpha3.LocalityLoadBalancerSetting   `json:"localityLbSetting" patchStrategy:"merge"`
	DNSRefreshRate                 *durationpb.Duration                    `json:"dnsRefreshRate" patchStrategy:"replace"`
	Certificates                   []*v1alpha13.Certificate                `json:"certificates" patchStrategy:"replace"`
	ThriftConfig                   *v1alpha13.MeshConfig_ThriftConfig      `json:"thriftConfig" patchStrategy:"merge"`
	ServiceSettings                []*v1alpha13.MeshConfig_ServiceSettings `json:"serviceSettings" patchStrategy:"replace"`
	Tracing                        *tracing                                `json:"tracing" patchStrategy:"replace"`
	Sds                            *v1alpha13.SDS                          `json:"sds" patchStrategy:"replace"`
	EnvoyAccessLogService          *v1alpha13.RemoteService                `json:"envoyAccessLogService" patchStrategy:"merge" patchMergeKey:"address"`
	EnvoyMetricsService            *v1alpha13.RemoteService                `json:"envoyMetricsService" patchStrategy:"merge" patchMergeKey:"address"`
	ProxyMetadata                  map[string]string                       `json:"proxyMetadata" patchStrategy:"merge"`
	ExtraStatTags                  []string                                `json:"extraStatTags" patchStrategy:"replace"`
	GatewayTopology                *v1alpha13.Topology                     `json:"gatewayTopology" patchStrategy:"replace"`
}

type tracing struct {
	TlSSettings *v1alpha3.ClientTLSSettings `json:"tlsSettings" patchStrategy:"merge"`
}

type meshConfigServiceSettings struct {
	Settings *v1alpha13.MeshConfig_ServiceSettings_Settings `json:"settings" patchStrategy:"merge"`
	Hosts    []string                                       `json:"hosts" patchStrategy:"merge"`
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

var iopMergeStruct iopMergeStructType

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
