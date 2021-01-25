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

package mesh

import (
	"fmt"
	"io/ioutil"
	"net"
	"time"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/hashicorp/go-multierror"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/validation"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/pkg/log"
)

// DefaultProxyConfig for individual proxies
func DefaultProxyConfig() meshconfig.ProxyConfig {
	// TODO: include revision based on REVISION env
	// TODO: set default namespace based on POD_NAMESPACE env
	return meshconfig.ProxyConfig{
		ConfigPath:               constants.ConfigPathDir,
		ServiceCluster:           constants.ServiceClusterName,
		DrainDuration:            types.DurationProto(45 * time.Second),
		ParentShutdownDuration:   types.DurationProto(60 * time.Second),
		TerminationDrainDuration: types.DurationProto(5 * time.Second),
		ProxyAdminPort:           15000,
		Concurrency:              &types.Int32Value{Value: 2},
		ControlPlaneAuthPolicy:   meshconfig.AuthenticationPolicy_MUTUAL_TLS,
		DiscoveryAddress:         "istiod.istio-system.svc:15012",
		Tracing: &meshconfig.Tracing{
			Tracer: &meshconfig.Tracing_Zipkin_{
				Zipkin: &meshconfig.Tracing_Zipkin{
					Address: "zipkin.istio-system:9411",
				},
			},
		},

		// Code defaults
		BinaryPath:     constants.BinaryPathFilename,
		StatNameLength: 189,
		StatusPort:     15020,
	}
}

// DefaultMeshConfig returns the default mesh config.
// This is merged with values from the mesh config map.
func DefaultMeshConfig() meshconfig.MeshConfig {
	proxyConfig := DefaultProxyConfig()

	// Defaults matching the standard install
	// order matches the generated mesh config.
	return meshconfig.MeshConfig{
		EnableTracing:               true,
		AccessLogFile:               "",
		AccessLogEncoding:           meshconfig.MeshConfig_TEXT,
		AccessLogFormat:             "",
		EnableEnvoyAccessLogService: false,
		ProtocolDetectionTimeout:    types.DurationProto(0),
		IngressService:              "istio-ingressgateway",
		IngressControllerMode:       meshconfig.MeshConfig_STRICT,
		IngressClass:                "istio",
		TrustDomain:                 "cluster.local",
		TrustDomainAliases:          []string{},
		EnableAutoMtls:              &types.BoolValue{Value: true},
		OutboundTrafficPolicy:       &meshconfig.MeshConfig_OutboundTrafficPolicy{Mode: meshconfig.MeshConfig_OutboundTrafficPolicy_ALLOW_ANY},
		LocalityLbSetting: &v1alpha3.LocalityLoadBalancerSetting{
			Enabled: &types.BoolValue{Value: true},
		},
		Certificates:  []*meshconfig.Certificate{},
		DefaultConfig: &proxyConfig,

		RootNamespace:                  constants.IstioSystemNamespace,
		ProxyListenPort:                15001,
		ConnectTimeout:                 types.DurationProto(10 * time.Second),
		DefaultServiceExportTo:         []string{"*"},
		DefaultVirtualServiceExportTo:  []string{"*"},
		DefaultDestinationRuleExportTo: []string{"*"},
		DnsRefreshRate:                 types.DurationProto(5 * time.Second), // 5 seconds is the default refresh rate used in Envoy
		ThriftConfig:                   &meshconfig.MeshConfig_ThriftConfig{},
		ServiceSettings:                make([]*meshconfig.MeshConfig_ServiceSettings, 0),
	}
}

// ApplyProxyConfig applies the give proxy config yaml to a mesh config object. The passed in mesh config
// will not be modified.
func ApplyProxyConfig(yaml string, meshConfig meshconfig.MeshConfig) (*meshconfig.MeshConfig, error) {
	mc := proto.Clone(&meshConfig).(*meshconfig.MeshConfig)
	if err := gogoprotomarshal.ApplyYAML(yaml, mc.DefaultConfig); err != nil {
		return nil, fmt.Errorf("could not parse proxy config: %v", err)
	}
	return mc, nil
}

func extractProxyConfig(yamlText string) (string, error) {
	mp := map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(yamlText), &mp); err != nil {
		return "", err
	}
	proxyConfig := mp["defaultConfig"]
	if proxyConfig == nil {
		return "", nil
	}
	bytes, err := yaml.Marshal(proxyConfig)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// ApplyMeshConfig returns a new MeshConfig decoded from the
// input YAML with the provided defaults applied to omitted configuration values.
func ApplyMeshConfig(yaml string, defaultConfig meshconfig.MeshConfig) (*meshconfig.MeshConfig, error) {
	// We want to keep semantics that all fields are overrides, except proxy config is a merge. This allows
	// decent customization while also not requiring users to redefine the entire proxy config if they want to override
	// Note: if we want to add more structure in the future, we will likely need to revisit this idea.

	// Store the current set proxy config so we don't wipe it out, we will configure this later
	prevProxyConfig := defaultConfig.DefaultConfig
	defaultProxyConfig := DefaultProxyConfig()
	defaultConfig.DefaultConfig = &defaultProxyConfig
	if err := gogoprotomarshal.ApplyYAML(yaml, &defaultConfig); err != nil {
		return nil, multierror.Prefix(err, "failed to convert to proto.")
	}
	defaultConfig.DefaultConfig = prevProxyConfig

	// Get just the proxy config yaml
	pc, err := extractProxyConfig(yaml)
	if err != nil {
		return nil, multierror.Prefix(err, "failed to extract proxy config")
	}
	if pc != "" {
		// Apply proxy config yaml on to the merged mesh config. This gives us "merge" semantics for proxy config
		if err := gogoprotomarshal.ApplyYAML(pc, defaultConfig.DefaultConfig); err != nil {
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

func DeepCopyMeshConfig(mc *meshconfig.MeshConfig) (*meshconfig.MeshConfig, error) {
	j, err := gogoprotomarshal.ToJSON(mc)
	if err != nil {
		return nil, err
	}
	nmc := &meshconfig.MeshConfig{}
	if err := gogoprotomarshal.ApplyJSON(j, nmc); err != nil {
		return nil, err
	}
	return nmc, nil
}

// EmptyMeshNetworks configuration with no networks
func EmptyMeshNetworks() meshconfig.MeshNetworks {
	return meshconfig.MeshNetworks{
		Networks: map[string]*meshconfig.Network{},
	}
}

// ParseMeshNetworks returns a new MeshNetworks decoded from the
// input YAML.
func ParseMeshNetworks(yaml string) (*meshconfig.MeshNetworks, error) {
	out := EmptyMeshNetworks()
	if err := gogoprotomarshal.ApplyYAML(yaml, &out); err != nil {
		return nil, multierror.Prefix(err, "failed to convert to proto.")
	}

	if err := validation.ValidateMeshNetworks(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReadMeshNetworks gets mesh networks configuration from a config file
func ReadMeshNetworks(filename string) (*meshconfig.MeshNetworks, error) {
	yaml, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, multierror.Prefix(err, "cannot read networks config file")
	}
	return ParseMeshNetworks(string(yaml))
}

// ReadMeshConfig gets mesh configuration from a config file
func ReadMeshConfig(filename string) (*meshconfig.MeshConfig, error) {
	yaml, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, multierror.Prefix(err, "cannot read mesh config file")
	}
	return ApplyMeshConfigDefaults(string(yaml))
}

// ResolveHostsInNetworksConfig will go through the Gateways addresses for all
// networks in the config and if it's not an IP address it will try to lookup
// that hostname and replace it with the IP address in the config
func ResolveHostsInNetworksConfig(config *meshconfig.MeshNetworks) {
	if config == nil {
		return
	}
	for _, n := range config.Networks {
		for _, gw := range n.Gateways {
			gwAddr := gw.GetAddress()
			gwIP := net.ParseIP(gwAddr)
			if gwIP == nil && len(gwAddr) != 0 {
				addrs, err := net.LookupHost(gwAddr)
				if err != nil {
					log.Warnf("error resolving host %#v: %v", gw.GetAddress(), err)
				} else {
					gw.Gw = &meshconfig.Network_IstioNetworkGateway_Address{
						Address: addrs[0],
					}
				}
			}
		}
	}
}
