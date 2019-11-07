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

package mesh

import (
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/hashicorp/go-multierror"

	meshconfig "istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/validation"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

// DefaultProxyConfig for individual proxies
func DefaultProxyConfig() meshconfig.ProxyConfig {
	return meshconfig.ProxyConfig{
		ConfigPath:             constants.ConfigPathDir,
		BinaryPath:             constants.BinaryPathFilename,
		ServiceCluster:         constants.ServiceClusterName,
		DrainDuration:          types.DurationProto(45 * time.Second),
		ParentShutdownDuration: types.DurationProto(60 * time.Second),
		DiscoveryAddress:       constants.DiscoveryPlainAddress,
		ConnectTimeout:         types.DurationProto(1 * time.Second),
		StatsdUdpAddress:       "",
		EnvoyMetricsService:    &meshconfig.RemoteService{Address: ""},
		EnvoyAccessLogService:  &meshconfig.RemoteService{Address: ""},
		ProxyAdminPort:         15000,
		ControlPlaneAuthPolicy: meshconfig.AuthenticationPolicy_NONE,
		CustomConfigFile:       "",
		Concurrency:            0,
		StatNameLength:         189,
		Tracing:                nil,
	}
}

// DefaultMeshConfig configuration
func DefaultMeshConfig() meshconfig.MeshConfig {
	proxyConfig := DefaultProxyConfig()
	return meshconfig.MeshConfig{
		MixerCheckServer:                  "",
		MixerReportServer:                 "",
		DisablePolicyChecks:               true,
		PolicyCheckFailOpen:               false,
		SidecarToTelemetrySessionAffinity: false,
		RootNamespace:                     constants.IstioSystemNamespace,
		ProxyListenPort:                   15001,
		ConnectTimeout:                    types.DurationProto(1 * time.Second),
		IngressService:                    "istio-ingressgateway",
		EnableTracing:                     true,
		AccessLogFile:                     "/dev/stdout",
		AccessLogEncoding:                 meshconfig.MeshConfig_TEXT,
		DefaultConfig:                     &proxyConfig,
		SdsUdsPath:                        "",
		EnableSdsTokenMount:               false,
		TrustDomain:                       "",
		TrustDomainAliases:                []string{},
		DefaultServiceExportTo:            []string{"*"},
		DefaultVirtualServiceExportTo:     []string{"*"},
		DefaultDestinationRuleExportTo:    []string{"*"},
		OutboundTrafficPolicy:             &meshconfig.MeshConfig_OutboundTrafficPolicy{Mode: meshconfig.MeshConfig_OutboundTrafficPolicy_ALLOW_ANY},
		DnsRefreshRate:                    types.DurationProto(5 * time.Second), // 5 seconds is the default refresh rate used in Envoy
		ProtocolDetectionTimeout:          types.DurationProto(100 * time.Millisecond),
		EnableAutoMtls:                    &types.BoolValue{Value: false},
	}
}

// ApplyMeshConfig returns a new MeshConfig decoded from the
// input YAML with the provided defaults applied to omitted configuration values.
func ApplyMeshConfig(yaml string, defaultConfig meshconfig.MeshConfig) (*meshconfig.MeshConfig, error) {
	if err := gogoprotomarshal.ApplyYAML(yaml, &defaultConfig); err != nil {
		return nil, multierror.Prefix(err, "failed to convert to proto.")
	}

	// Reset the default ProxyConfig as jsonpb.UnmarshalString doesn't
	// handled nested decode properly for our use case.
	prevDefaultConfig := defaultConfig.DefaultConfig
	defaultProxyConfig := DefaultProxyConfig()
	defaultConfig.DefaultConfig = &defaultProxyConfig

	// Re-apply defaults to ProxyConfig if they were defined in the
	// original input MeshConfig.ProxyConfig.
	if prevDefaultConfig != nil {
		origProxyConfigYAML, err := gogoprotomarshal.ToYAML(prevDefaultConfig)
		if err != nil {
			return nil, multierror.Prefix(err, "failed to re-encode default proxy config")
		}
		if err := gogoprotomarshal.ApplyYAML(origProxyConfigYAML, defaultConfig.DefaultConfig); err != nil {
			return nil, multierror.Prefix(err, "failed to convert to proto.")
		}
	}

	if err := validation.ValidateMeshConfig(&defaultConfig); err != nil {
		return nil, err
	}

	return &defaultConfig, nil
}

// ApplyMeshConfigDefaults returns a new MeshConfig decoded from the
// input YAML with defaults applied to omitted configuration values.
func ApplyMeshConfigDefaults(yaml string) (*meshconfig.MeshConfig, error) {
	return ApplyMeshConfig(yaml, DefaultMeshConfig())
}

// EmptyMeshNetworks configuration with no networks
func EmptyMeshNetworks() meshconfig.MeshNetworks {
	return meshconfig.MeshNetworks{
		Networks: map[string]*meshconfig.Network{},
	}
}

// LoadMeshNetworksConfig returns a new MeshNetworks decoded from the
// input YAML.
func LoadMeshNetworksConfig(yaml string) (*meshconfig.MeshNetworks, error) {
	out := EmptyMeshNetworks()
	if err := gogoprotomarshal.ApplyYAML(yaml, &out); err != nil {
		return nil, multierror.Prefix(err, "failed to convert to proto.")
	}

	// TODO validate the loaded MeshNetworks
	// if err := ValidateMeshNetworks(&out); err != nil {
	// 	return nil, err
	// }
	return &out, nil
}
