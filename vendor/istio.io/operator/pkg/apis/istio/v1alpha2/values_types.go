// Copyright 2017 Istio Authors
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

package v1alpha2

// TODO: create remaining enum types.

import (
	"github.com/gogo/protobuf/jsonpb"
	corev1 "k8s.io/api/core/v1"
	intstr "k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/operator/pkg/util"
)

// Values is described in istio.io documentation.
type Values struct {
	CertManager     *CertManagerConfig     `json:"certmanager,omitempty"`
	CNI             *CNIConfig             `json:"istio_cni,omitempty"`
	CoreDNS         *CoreDNSConfig         `json:"istiocoredns,omitempty"`
	Galley          *GalleyConfig          `json:"galley,omitempty"`
	Gateways        *GatewaysConfig        `json:"gateways,omitempty"`
	Global          *GlobalConfig          `json:"global,omitempty"`
	Grafana         map[string]interface{} `json:"grafana,omitempty"`
	Mixer           *MixerConfig           `json:"mixer,omitempty"`
	NodeAgent       *NodeAgentConfig       `json:"nodeagent,omitempty"`
	Pilot           *PilotConfig           `json:"pilot,omitempty"`
	Prometheus      *PrometheusConfig      `json:"prometheus,omitempty"`
	Security        *SecurityConfig        `json:"security,omitempty"`
	SidecarInjector *SidecarInjectorConfig `json:"sidecarInjectorWebhook,omitempty"`
	Tracing         *TracingConfig         `json:"tracing,omitempty"`
}

// CertManagerConfig is described in istio.io documentation.
type CertManagerConfig struct {
	Enabled      *bool                  `json:"enabled,omitempty"`
	Hub          *string                `json:"hub,omitempty"`
	NodeSelector map[string]interface{} `json:"nodeSelector"`
	Resources    *ResourcesConfig       `json:"resources,omitempty"`
	Tag          *string                `json:"tag,omitempty"`
}

// CNIConfig is described in istio.io documentation.
type CNIConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// CoreDNSConfig is described in istio.io documentation.
type CoreDNSConfig struct {
	CoreDNSImage       *string                `json:"coreDNSImage,omitempty"`
	CoreDNSPluginImage *string                `json:"coreDNSPluginImage,omitempty"`
	Enabled            *bool                  `json:"enabled,omitempty"`
	NodeSelector       map[string]interface{} `json:"nodeSelector,omitempty"`
	ReplicaCount       *uint8                 `json:"replicaCount,omitempty"`
}

// GalleyConfig is described in istio.io documentation.
type GalleyConfig struct {
	Enabled                          *bool                    `json:"enabled,omitempty"`
	Image                            *string                  `json:"image,omitempty"`
	Mesh                             map[string]string        `json:"mesh,omitempty"`
	PodAntiAffinityLabelSelector     []map[string]interface{} `json:"podAntiAffinityLabelSelector"`
	PodAntiAffinityTermLabelSelector []map[string]interface{} `json:"podAntiAffinityTermLabelSelector"`
	ReplicaCount                     *uint8                   `json:"replicaCount,omitempty"`
	Resources                        *ResourcesConfig         `json:"resources,omitempty"`
}

// GatewaysConfig is described in istio.io documentation.
type GatewaysConfig struct {
	EgressGateway  *EgressGatewayConfig  `json:"istio-egressgateway,omitempty"`
	Enabled        *bool                 `json:"enabled,omitempty"`
	ILBGateway     *ILBGatewayConfig     `json:"istio-ilbgateway,omitempty"`
	IngressGateway *IngressGatewayConfig `json:"istio-ingressgateway,omitempty"`
}

// EgressGatewayConfig is described in istio.io documentation.
type EgressGatewayConfig struct {
	AutoscaleEnabled                 *bool                       `json:"autoscaleEnabled,omitempty"`
	AutoscaleMax                     *uint8                      `json:"autoscaleMax,omitempty"`
	AutoscaleMin                     *uint8                      `json:"autoscaleMin,omitempty"`
	ConnectTimeout                   *string                     `json:"connectTimeout,omitempty"`
	CPU                              *CPUTargetUtilizationConfig `json:"cpu,omitempty"`
	DrainDuration                    *string                     `json:"drainDuration,omitempty"`
	Enabled                          *bool                       `json:"enabled,omitempty"`
	Env                              map[string]string           `json:"env,omitempty"`
	Labels                           *GatewayLabelsConfig        `json:"labels,omitempty"`
	NodeSelector                     map[string]string           `json:"nodeSelector,omitempty"`
	PodAnnotations                   map[string]interface{}      `json:"podAnnotations,omitempty"`
	PodAntiAffinityLabelSelector     []map[string]interface{}    `json:"podAntiAffinityLabelSelector"`
	PodAntiAffinityTermLabelSelector []map[string]interface{}    `json:"podAntiAffinityTermLabelSelector"`
	Ports                            []*PortsConfig              `json:"ports,omitempty"`
	Resources                        *ResourcesConfig            `json:"resources,omitempty"`
	SecretVolumes                    []*SecretVolume             `json:"secretVolumes,omitempty"`
	ServiceAnnotations               map[string]interface{}      `json:"serviceAnnotations,omitempty"`
	Type                             corev1.ServiceType          `json:"type,omitempty"`
	ZeroVPN                          *ZeroVPNConfig              `json:"zvpn,omitempty"`
}

// ZeroVPNConfig is described in istio.io documentation.
type ZeroVPNConfig struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Suffix  *string `json:"suffix,omitempty"`
}

// ILBGatewayConfig is described in istio.io documentation.
type ILBGatewayConfig struct {
	AutoscaleEnabled   *bool                       `json:"autoscaleEnabled,omitempty"`
	AutoscaleMax       *uint8                      `json:"autoscaleMax,omitempty"`
	AutoscaleMin       *uint8                      `json:"autoscaleMin,omitempty"`
	Enabled            *bool                       `json:"enabled,omitempty"`
	CPU                *CPUTargetUtilizationConfig `json:"cpu,omitempty"`
	Labels             *GatewayLabelsConfig        `json:"labels,omitempty"`
	LoadBalancerIP     *string                     `json:"loadBalancerIP,omitempty"`
	NodeSelector       map[string]interface{}      `json:"nodeSelector,omitempty"`
	PodAnnotations     map[string]interface{}      `json:"podAnnotations,omitempty"`
	Ports              []*PortsConfig              `json:"ports,omitempty"`
	Resources          *ResourcesConfig            `json:"resources,omitempty"`
	SecretVolumes      []*SecretVolume             `json:"secretVolumes,omitempty"`
	ServiceAnnotations map[string]interface{}      `json:"serviceAnnotations,omitempty"`
	Type               corev1.ServiceType          `json:"type,omitempty"`
}

// IngressGatewayConfig is described in istio.io documentation.
type IngressGatewayConfig struct {
	AutoscaleEnabled                 *bool                       `json:"autoscaleEnabled,omitempty"`
	AutoscaleMax                     *uint8                      `json:"autoscaleMax,omitempty"`
	AutoscaleMin                     *uint8                      `json:"autoscaleMin,omitempty"`
	ConnectTimeout                   *string                     `json:"connectTimeout,omitempty"`
	CPU                              *CPUTargetUtilizationConfig `json:"cpu,omitempty"`
	CustomService                    *bool                       `json:"customService,omitempty"`
	Debug                            *string                     `json:"debug,omitempty"`
	Domain                           *string                     `json:"domain,omitempty"`
	DrainDuration                    *string                     `json:"drainDuration,omitempty"`
	Enabled                          *bool                       `json:"enabled,omitempty"`
	Env                              map[string]string           `json:"env,omitempty"`
	ExternalIPs                      []string                    `json:"externalIPs,omitempty"`
	K8sIngress                       *bool                       `json:"k8sIngress,omitempty"`
	K8sIngressHTTPS                  *bool                       `json:"k8sIngressHttps,omitempty"`
	Labels                           *GatewayLabelsConfig        `json:"labels,omitempty"`
	LoadBalancerIP                   *string                     `json:"loadBalancerIP,omitempty"`
	LoadBalancerSourceRanges         []string                    `json:"loadBalancerSourceRanges,omitempty"`
	MeshExpansionPorts               []*PortsConfig              `json:"meshExpansionPorts,omitempty"`
	NodeSelector                     map[string]interface{}      `json:"nodeSelector,omitempty"`
	PodAnnotations                   map[string]interface{}      `json:"podAnnotations,omitempty"`
	PodAntiAffinityLabelSelector     []map[string]interface{}    `json:"podAntiAffinityLabelSelector"`
	PodAntiAffinityTermLabelSelector []map[string]interface{}    `json:"podAntiAffinityTermLabelSelector"`
	Ports                            []*PortsConfig              `json:"ports,omitempty"`
	ReplicaCount                     *uint8                      `json:"replicaCount,omitempty"`
	Resources                        map[string]interface{}      `json:"resources,omitempty"`
	Sds                              *IngressGatewaySdsConfig    `json:"sds,omitempty"`
	SecretVolumes                    []*SecretVolume             `json:"secretVolumes,omitempty"`
	ServiceAnnotations               map[string]interface{}      `json:"serviceAnnotations,omitempty"`
	Type                             corev1.ServiceType          `json:"type,omitempty"`
	Zvpn                             *IngressGatewayZvpnConfig   `json:"zvpn,omitempty"`
}

// IngressGatewaySdsConfig is described in istio.io documentation.
type IngressGatewaySdsConfig struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Image   *string `json:"image,omitempty"`
}

// IngressGatewayZvpnConfig is described in istio.io documentation.
type IngressGatewayZvpnConfig struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Suffix  *string `json:"suffix,omitempty"`
}

// GlobalConfig is described in istio.io documentation.
type GlobalConfig struct {
	Arch                        *ArchConfig                       `json:"arch,omitempty"`
	ConfigNamespace             *string                           `json:"configNamespace,omitempty"`
	ConfigValidation            *bool                             `json:"configValidation,omitempty"`
	ControlPlaneSecurityEnabled *bool                             `json:"controlPlaneSecurityEnabled,omitempty"`
	DefaultNodeSelector         map[string]interface{}            `json:"defaultNodeSelector,omitempty"`
	DefaultPodDisruptionBudget  *DefaultPodDisruptionBudgetConfig `json:"defaultPodDisruptionBudget,omitempty"`
	DisablePolicyChecks         *bool                             `json:"disablePolicyChecks,omitempty"`
	DefaultResources            *DefaultResourcesConfig           `json:"defaultResources,omitempty"`
	EnableHelmTest              *bool                             `json:"enableHelmTest,omitempty"`
	EnableTracing               *bool                             `json:"enableTracing,omitempty"`
	Hub                         *string                           `json:"hub,omitempty"`
	ImagePullPolicy             corev1.PullPolicy                 `json:"imagePullPolicy,omitempty"`
	IstioNamespace              *string                           `json:"istioNamespace,omitempty"`
	KubernetesIngress           *KubernetesIngressConfig          `json:"k8sIngress,omitempty"`
	LocalityLbSetting           map[string]interface{}            `json:"localityLbSetting,omitempty"`
	Logging                     *GlobalLoggingConfig              `json:"logging,omitempty"`
	MeshExpansion               *MeshExpansionConfig              `json:"meshExpansion,omitempty"`
	MeshNetworks                map[string]interface{}            `json:"meshNetworks,omitempty"`
	MonitoringPort              *uint16                           `json:"monitoringPort,omitempty"`
	MTLS                        *MTLSConfig                       `json:"mtls,omitempty"`
	MultiCluster                *MultiClusterConfig               `json:"multiCluster,omitempty"`
	OneNamespace                *bool                             `json:"oneNamespace,omitempty"`
	OutboundTrafficPolicy       *OutboundTrafficPolicyConfig      `json:"outboundTrafficPolicy,omitempty"`
	PolicyCheckFailOpen         *bool                             `json:"policyCheckFailOpen,omitempty"`
	PolicyNamespace             *string                           `json:"policyNamespace,omitempty"`
	PriorityClassName           *string                           `json:"priorityClassName,omitempty"`
	Proxy                       *ProxyConfig                      `json:"proxy,omitempty"`
	ProxyInit                   *ProxyInitConfig                  `json:"proxy_init,omitempty"`
	SDS                         *SDSConfig                        `json:"sds,omitempty"`
	Tag                         *string                           `json:"tag,omitempty"`
	TelemetryNamespace          *string                           `json:"telemetryNamespace,omitempty"`
	Tracer                      *TracerConfig                     `json:"tracer,omitempty"`
	TrustDomain                 *string                           `json:"trustDomain,omitempty"`
	UseMCP                      *bool                             `json:"useMCP,omitempty"`
}

// ArchConfig is described in istio.io documentation.
type ArchConfig struct {
	Amd64   *uint8 `json:"amd64,omitempty"`
	Ppc64le *uint8 `json:"ppc64le,omitempty"`
	S390x   *uint8 `json:"s390x,omitempty"`
}

// DefaultPodDisruptionBudgetConfig is described in istio.io documentation.
type DefaultPodDisruptionBudgetConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// DefaultResourcesConfig is described in istio.io documentation.
type DefaultResourcesConfig struct {
	Requests *ResourcesRequestsConfig `json:"requests,omitempty"`
}

// KubernetesIngressConfig represents the configuration for Kubernetes Ingress.
type KubernetesIngressConfig struct {
	Enabled     *bool   `json:"enabled,omitempty"`
	EnableHTTPS *bool   `json:"enableHttps,omitempty"`
	GatewayName *string `json:"gatewayName,omitempty"`
}

// GlobalLoggingConfig is described in istio.io documentation.
type GlobalLoggingConfig struct {
	Level *string `json:"level,omitempty"`
}

// MeshExpansionConfig is described in istio.io documentation.
type MeshExpansionConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
	UseILB  *bool `json:"useILB,omitempty"`
}

// MTLSConfig is described in istio.io documentation.
type MTLSConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// MultiClusterConfig is described in istio.io documentation.
type MultiClusterConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// ProxyConfig specifies how proxies are configured within Istio.
type ProxyConfig struct {
	AccessLogFile                *string             `json:"accessLogFile,omitempty"`
	AccessLogFormat              *string             `json:"accessLogFormat,omitempty"`
	AccessLogEncoding            *string             `json:"accessLogEncoding,omitempty"`
	AutoInject                   *string             `json:"autoInject,omitempty"`
	ClusterDomain                *string             `json:"clusterDomain,omitempty"`
	ComponentLogLevel            *string             `json:"componentLogLevel,omitempty"`
	Concurrency                  *uint8              `json:"concurrency,omitempty"`
	DNSRefreshRate               *string             `json:"dnsRefreshRate,omitempty"`
	EnableCoreDump               *bool               `json:"enableCoreDump,omitempty"`
	EnvoyMetricsService          *EnvoyMetricsConfig `json:"envoyMetricsService,omitempty"`
	EnvoyStatsD                  *EnvoyMetricsConfig `json:"envoyStatsd,omitempty"`
	ExcludeInboundPorts          *string             `json:"excludeInboundPorts,omitempty"`
	ExcludeIPRanges              *string             `json:"excludeIPRanges,omitempty"`
	Image                        *string             `json:"image,omitempty"`
	IncludeInboundPorts          *string             `json:"includeInboundPorts,omitempty"`
	IncludeIPRanges              *string             `json:"includeIPRanges,omitempty"`
	KubevirtInterfaces           *string             `json:"kubevirtInterfaces,omitempty"`
	LogLevel                     *string             `json:"logLevel,omitempty"`
	Privileged                   *bool               `json:"privileged,omitempty"`
	ReadinessInitialDelaySeconds *uint16             `json:"readinessInitialDelaySeconds,omitempty"`
	ReadinessPeriodSeconds       *uint16             `json:"readinessPeriodSeconds,omitempty"`
	ReadinessFailureThreshold    *uint16             `json:"readinessFailureThreshold,omitempty"`
	StatusPort                   *uint16             `json:"statusPort,omitempty"`
	Resources                    *ResourcesConfig    `json:"resources,omitempty"`
	Tracer                       *string             `json:"tracer,omitempty"`
}

// EnvoyMetricsConfig is described in istio.io documentation.
type EnvoyMetricsConfig struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Host    *string `json:"host,omitempty"`
	Port    *int16  `json:"port,omitempty"`
}

// ProxyInitConfig is described in istio.io documentation.
type ProxyInitConfig struct {
	Image *string `json:"image,omitempty"`
}

// OutboundTrafficPolicyConfig is described in istio.io documentation.
type OutboundTrafficPolicyConfig struct {
	Mode string `json:"mode,omitempty"`
}

// SDSConfig is described in istio.io documentation.
type SDSConfig struct {
	Enabled           *bool   `json:"enabled,omitempty"`
	UDSPath           *string `json:"udsPath,omitempty"`
	UseNormalJWT      *bool   `json:"useNormalJwt,omitempty"`
	UseTrustworthyJWT *bool   `json:"useTrustworthyJwt,omitempty"`
}

// TracerConfig is described in istio.io documentation.
type TracerConfig struct {
	Datadog   *TracerDatadogConfig   `json:"datadog,omitempty"`
	LightStep *TracerLightStepConfig `json:"lightstep,omitempty"`
	Zipkin    *TracerZipkinConfig    `json:"zipkin,omitempty"`
}

// TracerDatadogConfig is described in istio.io documentation.
type TracerDatadogConfig struct {
	Address *string `json:"address,omitempty"`
}

// TracerLightStepConfig is described in istio.io documentation.
type TracerLightStepConfig struct {
	Address     *string `json:"address,omitempty"`
	AccessToken *string `json:"accessToken,omitempty"`
	CACertPath  *string `json:"cacertPath,omitempty"`
	Secure      *bool   `json:"secure,omitempty"`
}

// TracerZipkinConfig is described in istio.io documentation.
type TracerZipkinConfig struct {
	Address *string `json:"address,omitempty"`
}

// MixerConfig is described in istio.io documentation.
type MixerConfig struct {
	Adapters  *MixerAdaptersConfig  `json:"adapters,omitempty"`
	Enabled   *bool                 `json:"enabled,omitempty"`
	Env       map[string]string     `json:"env,omitempty"`
	Image     *string               `json:"image,omitempty"`
	Policy    *MixerPolicyConfig    `json:"policy,omitempty"`
	Telemetry *MixerTelemetryConfig `json:"telemetry,omitempty"`
}

// MixerAdaptersConfig is described in istio.io documentation.
type MixerAdaptersConfig struct {
	KubernetesEnv  *KubernetesEnvMixerAdapterConfig `json:"kubernetesenv,omitempty"`
	Prometheus     *PrometheusMixerAdapterConfig    `json:"prometheus,omitempty"`
	Stdio          *StdioMixerAdapterConfig         `json:"stdio,omitempty"`
	UseAdapterCRDs *bool                            `json:"useAdapterCRDs,omitempty"`
}

// KubernetesEnvMixerAdapterConfig is described in istio.io documentation.
type KubernetesEnvMixerAdapterConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// PrometheusMixerAdapterConfig is described in istio.io documentation.
type PrometheusMixerAdapterConfig struct {
	Enabled               *bool   `json:"enabled,omitempty"`
	MetricsExpiryDuration *string `json:"metricsExpiryDuration,omitempty"`
}

// StdioMixerAdapterConfig is described in istio.io documentation.
type StdioMixerAdapterConfig struct {
	Enabled      *bool `json:"enabled,omitempty"`
	OutputAsJSON *bool `json:"outputAsJson,omitempty"`
}

// MixerPolicyConfig is described in istio.io documentation.
type MixerPolicyConfig struct {
	AutoscaleEnabled *bool                       `json:"autoscaleEnabled,omitempty"`
	AutoscaleMax     *uint8                      `json:"autoscaleMax,omitempty"`
	AutoscaleMin     *uint8                      `json:"autoscaleMin,omitempty"`
	CPU              *CPUTargetUtilizationConfig `json:"cpu,omitempty"`
	Enabled          *bool                       `json:"enabled,omitempty"`
	Image            *string                     `json:"image,omitempty"`
	PodAnnotations   map[string]interface{}      `json:"podAnnotations,omitempty"`
	ReplicaCount     *uint8                      `json:"replicaCount,omitempty"`
}

// MixerTelemetryConfig is described in istio.io documentation.
type MixerTelemetryConfig struct {
	Adapters               *MixerAdaptersConfig       `json:"adapters,omitempty"`
	AutoscaleEnabled       *bool                      `json:"autoscaleEnabled,omitempty"`
	AutoscaleMax           *uint8                     `json:"autoscaleMax,omitempty"`
	AutoscaleMin           *uint8                     `json:"autoscaleMin,omitempty"`
	CPU                    CPUTargetUtilizationConfig `json:"cpu,omitempty"`
	Enabled                *bool                      `json:"enabled,omitempty"`
	Env                    map[string]string          `json:"env,omitempty"`
	Image                  *string                    `json:"image,omitempty"`
	LoadShedding           *LoadSheddingConfig        `json:"loadshedding,omitempty"`
	NodeSelector           map[string]interface{}     `json:"nodeSelector,omitempty"`
	PodAnnotations         map[string]interface{}     `json:"podAnnotations,omitempty"`
	ReplicaCount           *uint8                     `json:"replicaCount,omitempty"`
	Resources              *ResourcesConfig           `json:"resources,omitempty"`
	SessionAffinityEnabled *bool                      `json:"sessionAffinityEnabled,omitempty"`
}

// LoadSheddingConfig is described in istio.io documentation.
type LoadSheddingConfig struct {
	LatencyThreshold *string `json:"latencyThreshold,omitempty"`
	Mode             *string `json:"mode,omitempty"`
}

// NodeAgentConfig is described in istio.io documentation.
type NodeAgentConfig struct {
	Enabled      *bool                  `json:"enabled,omitempty"`
	Env          map[string]interface{} `json:"env,omitempty"`
	Image        *string                `json:"image,omitempty"`
	NodeSelector map[string]interface{} `json:"nodeSelector,omitempty"`
}

// PilotConfig is described in istio.io documentation.
type PilotConfig struct {
	Enabled                          *bool                       `json:"enabled,omitempty"`
	AutoscaleEnabled                 *bool                       `json:"autoscaleEnabled,omitempty"`
	AutoscaleMin                     *uint8                      `json:"autoscaleMin,omitempty"`
	AutoscaleMax                     *uint8                      `json:"autoscaleMax,omitempty"`
	ReplicaCount                     *uint8                      `json:"replicaCount,omitempty"`
	Image                            *string                     `json:"image,omitempty"`
	Sidecar                          *bool                       `json:"sidecar,omitempty"`
	TraceSampling                    *float64                    `json:"traceSampling,omitempty"`
	Resources                        *ResourcesConfig            `json:"resources,omitempty"`
	ConfigNamespace                  *string                     `json:"configNamespace,omitempty"`
	CPU                              *CPUTargetUtilizationConfig `json:"cpu,omitempty"`
	NodeSelector                     map[string]interface{}      `json:"nodeSelector,omitempty"`
	KeepaliveMaxServerConnectionAge  *string                     `json:"keepaliveMaxServerConnectionAge,omitempty"`
	DeploymentLabels                 map[string]string           `json:"deploymentLabels,omitempty"`
	MeshNetworks                     map[string]interface{}      `json:"meshNetworks,omitempty"`
	PodAntiAffinityLabelSelector     []map[string]interface{}    `json:"podAntiAffinityLabelSelector"`
	PodAntiAffinityTermLabelSelector []map[string]interface{}    `json:"podAntiAffinityTermLabelSelector"`
	ConfigMap                        *bool                       `json:"configMap,omitempty"`
	Ingress                          *PilotIngressConfig         `json:"ingress,omitempty"`
	UseMCP                           *bool                       `json:"useMCP,omitempty"`
	Env                              map[string]string           `json:"env,omitempty"`
	Policy                           *PilotPolicyConfig          `json:"policy,omitempty"`
	Telemetry                        *PilotTelemetryConfig       `json:"telemetry,omitempty"`
}

// PilotIngressConfig is described in istio.io documentation.
type PilotIngressConfig struct {
	IngressService        string `json:"ingressService,omitempty"`
	IngressControllerMode string `json:"ingressControllerMode,omitempty"`
	IngressClass          string `json:"ingressClass,omitempty"`
}

// PilotPolicyConfig is described in istio.io documentation.
type PilotPolicyConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// PilotTelemetryConfig is described in istio.io documentation.
type PilotTelemetryConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// PrometheusConfig is described in istio.io documentation.
type PrometheusConfig struct {
	CreatePrometheusResource *bool                     `json:"createPrometheusResource,omitempty"`
	Enabled                  *bool                     `json:"enabled,omitempty"`
	ReplicaCount             *uint8                    `json:"replicaCount,omitempty"`
	Hub                      *string                   `json:"hub,omitempty"`
	Tag                      *string                   `json:"tag,omitempty"`
	Retention                *string                   `json:"retention,omitempty"`
	NodeSelector             map[string]interface{}    `json:"nodeSelector,omitempty"`
	ScrapeInterval           *string                   `json:"scrapeInterval,omitempty"`
	ContextPath              *string                   `json:"contextPath,omitempty"`
	Ingress                  *AddonIngressConfig       `json:"ingress,omitempty"`
	Service                  *PrometheusServiceConfig  `json:"service,omitempty"`
	Security                 *PrometheusSecurityConfig `json:"security,omitempty"`
}

// PrometheusServiceConfig is described in istio.io documentation.
type PrometheusServiceConfig struct {
	Annotations map[string]interface{}           `json:"annotations,omitempty"`
	NodePort    *PrometheusServiceNodePortConfig `json:"nodePort,omitempty"`
}

// PrometheusServiceNodePortConfig is described in istio.io documentation.
type PrometheusServiceNodePortConfig struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Port    *uint16 `json:"port,omitempty"`
}

// PrometheusSecurityConfig is described in istio.io documentation.
type PrometheusSecurityConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// SecurityConfig is described in istio.io documentation.
type SecurityConfig struct {
	Enabled          *bool                  `json:"enabled,omitempty"`
	ReplicaCount     *uint8                 `json:"replicaCount,omitempty"`
	Image            *string                `json:"image,omitempty"`
	SelfSigned       *bool                  `json:"selfSigned,omitempty"`
	TrustDomain      *string                `json:"trustDomain,omitempty"`
	DNSCerts         map[string]string      `json:"dnsCerts,omitempty"`
	CreateMeshPolicy *bool                  `json:"createMeshPolicy,omitempty"`
	NodeSelector     map[string]interface{} `json:"nodeSelector,omitempty"`
}

// SidecarInjectorConfig is described in istio.io documentation.
type SidecarInjectorConfig struct {
	AlwaysInjectSelector             []map[string]interface{} `json:"alwaysInjectSelector,omitempty"`
	Enabled                          *bool                    `json:"enabled,omitempty"`
	EnableNamespacesByDefault        *bool                    `json:"enableNamespacesByDefault,omitempty"`
	Image                            *string                  `json:"image,omitempty"`
	NodeSelector                     map[string]interface{}   `json:"nodeSelector,omitempty"`
	NeverInjectSelector              []map[string]interface{} `json:"neverInjectSelector,omitempty"`
	PodAntiAffinityLabelSelector     []map[string]interface{} `json:"podAntiAffinityLabelSelector"`
	PodAntiAffinityTermLabelSelector []map[string]interface{} `json:"podAntiAffinityTermLabelSelector"`
	ReplicaCount                     *uint8                   `json:"replicaCount,omitempty"`
	RewriteAppHTTPProbe              *bool                    `json:"rewriteAppHTTPProbe,inline"`
	SelfSigned                       *bool                    `json:"selfSigned,omitempty"`
}

// TracingConfig is described in istio.io documentation.
type TracingConfig struct {
	Enabled      *bool                  `json:"enabled,omitempty"`
	Ingress      *TracingIngressConfig  `json:"ingress,omitempty"`
	Jaeger       *TracingJaegerConfig   `json:"jaeger,omitempty"`
	NodeSelector map[string]interface{} `json:"nodeSelector,omitempty"`
	Provider     *string                `json:"provider,omitempty"`
	Service      *ServiceConfig         `json:"service,omitempty"`
	Zipkin       *TracingZipkinConfig   `json:"zipkin,omitempty"`
}

// TracingIngressConfig is described in istio.io documentation.
type TracingIngressConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// TracingJaegerConfig is described in istio.io documentation.
type TracingJaegerConfig struct {
	Hub    *string                    `json:"hub,omitempty"`
	Tag    *string                    `json:"tag,omitempty"`
	Memory *TracingJaegerMemoryConfig `json:"memory,omitempty"`
}

// TracingJaegerMemoryConfig is described in istio.io documentation.
type TracingJaegerMemoryConfig struct {
	MaxTraces *string `json:"max_traces,omitempty"`
}

// TracingZipkinConfig is described in istio.io documentation.
type TracingZipkinConfig struct {
	Hub               *string                  `json:"hub,omitempty"`
	Tag               *string                  `json:"tag,omitempty"`
	ProbeStartupDelay *uint16                  `json:"probeStartupDelay,omitempty"`
	QueryPort         *uint16                  `json:"queryPort,omitempty"`
	Resources         *ResourcesConfig         `json:"resources,omitempty"`
	JavaOptsHeap      *string                  `json:"javaOptsHeap,omitempty"`
	MaxSpans          *string                  `json:"maxSpans,omitempty"`
	Node              *TracingZipkinNodeConfig `json:"node,omitempty"`
}

// TracingZipkinNodeConfig is described in istio.io documentation.
type TracingZipkinNodeConfig struct {
	CPUs *uint8 `json:"cpus,omitempty"`
}

// Shared types

// ResourcesConfig is described in istio.io documentation.
type ResourcesConfig struct {
	Requests *ResourcesRequestsConfig `json:"requests,omitempty"`
	Limits   *ResourcesRequestsConfig `json:"limits,omitempty"`
}

// ResourcesRequestsConfig is described in istio.io documentation.
type ResourcesRequestsConfig struct {
	CPU    *string `json:"cpu,omitempty"`
	Memory *string `json:"memory,omitempty"`
}

// ServiceConfig is described in istio.io documentation.
type ServiceConfig struct {
	Annotations  map[string]interface{} `json:"annotations,omitempty"`
	ExternalPort *uint16                `json:"externalPort,omitempty"`
	Name         *string                `json:"name,omitempty"`
	Type         corev1.ServiceType     `json:"type,omitempty"`
}

// CPUTargetUtilizationConfig is described in istio.io documentation.
type CPUTargetUtilizationConfig struct {
	TargetAverageUtilization *int32 `json:"targetAverageUtilization,omitempty"`
}

// PortsConfig is described in istio.io documentation.
type PortsConfig struct {
	Name       *string `json:"name,omitempty"`
	Port       *int16  `json:"port,omitempty"`
	NodePort   *int16  `json:"nodePort,omitempty"`
	TargetPort *int16  `json:"targetPort,omitempty"`
}

// SecretVolume is described in istio.io documentation.
type SecretVolume struct {
	MountPath  *string `json:"mountPath,omitempty"`
	Name       *string `json:"name,omitempty"`
	SecretName *string `json:"secretName,omitempty"`
}

// GatewayLabelsConfig is described in istio.io documentation.
type GatewayLabelsConfig struct {
	App   *string `json:"app,omitempty"`
	Istio *string `json:"istio,omitempty"`
}

// AddonIngressConfig is described in istio.io documentation.
type AddonIngressConfig struct {
	Enabled *bool    `json:"enabled,omitempty"`
	Hosts   []string `json:"hosts,omitempty"`
}

// define new type from k8s intstr to marshal/unmarshal jsonpb
type IntOrStringForPB struct {
	intstr.IntOrString
}

// MarshalJSONPB implements the jsonpb.JSONPBMarshaler interface.
func (intstrpb *IntOrStringForPB) MarshalJSONPB(_ *jsonpb.Marshaler) ([]byte, error) {
	return intstrpb.MarshalJSON()
}

// UnmarshalJSONPB implements the jsonpb.JSONPBUnmarshaler interface.
func (intstrpb *IntOrStringForPB) UnmarshalJSONPB(_ *jsonpb.Unmarshaler, value []byte) error {
	return intstrpb.UnmarshalJSON(value)
}

// FromInt creates an IntOrStringForPB object with an int32 value.
func FromInt(val int) IntOrStringForPB {
	return IntOrStringForPB{intstr.FromInt(val)}
}

// FromString creates an IntOrStringForPB object with a string value.
func FromString(val string) IntOrStringForPB {
	return IntOrStringForPB{intstr.FromString(val)}
}

// Validate checks
func (t *PilotConfig) Validate(failOnMissingValidation bool, values *Values, icpls *IstioControlPlaneSpec) util.Errors {
	var validationErrors util.Errors
	// Exmple
	// validationErrors = util.AppendErr(validationErrors, fmt.Errorf("pilotconfig has not been yet implemented"))

	return validationErrors
}

// Validate checks CNIConfig confiugration
func (t *CNIConfig) Validate(failOnMissingValidation bool, values *Values, icpls *IstioControlPlaneSpec) util.Errors {
	var validationErrors util.Errors
	// Example
	// validationErrors = util.AppendErr(validationErrors, fmt.Errorf("cniconfig has not been yet implemented"))

	return validationErrors
}
