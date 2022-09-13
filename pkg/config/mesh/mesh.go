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
	"os"
	"time"

	"github.com/hashicorp/go-multierror"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"
	"sigs.k8s.io/yaml"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/validation"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

// DefaultProxyConfig for individual proxies
func DefaultProxyConfig() *meshconfig.ProxyConfig {
	// TODO: include revision based on REVISION env
	// TODO: set default namespace based on POD_NAMESPACE env
	return &meshconfig.ProxyConfig{
		ConfigPath:               constants.ConfigPathDir,
		ClusterName:              &meshconfig.ProxyConfig_ServiceCluster{ServiceCluster: constants.ServiceClusterName},
		DrainDuration:            durationpb.New(45 * time.Second),
		ParentShutdownDuration:   durationpb.New(60 * time.Second),
		TerminationDrainDuration: durationpb.New(5 * time.Second),
		ProxyAdminPort:           15000,
		Concurrency:              &wrappers.Int32Value{Value: 2},
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

// DefaultMeshNetworks returns a default meshnetworks configuration.
// By default, it is empty.
func DefaultMeshNetworks() *meshconfig.MeshNetworks {
	mn := EmptyMeshNetworks()
	return &mn
}

// DefaultMeshConfig returns the default mesh config.
// This is merged with values from the mesh config map.
func DefaultMeshConfig() *meshconfig.MeshConfig {
	proxyConfig := DefaultProxyConfig()

	// Defaults matching the standard install
	// order matches the generated mesh config.
	return &meshconfig.MeshConfig{
		EnableTracing:               true,
		AccessLogFile:               "",
		AccessLogEncoding:           meshconfig.MeshConfig_TEXT,
		AccessLogFormat:             "",
		EnableEnvoyAccessLogService: false,
		ProtocolDetectionTimeout:    durationpb.New(0),
		IngressService:              "istio-ingressgateway",
		IngressControllerMode:       meshconfig.MeshConfig_STRICT,
		IngressClass:                "istio",
		TrustDomain:                 constants.DefaultClusterLocalDomain,
		TrustDomainAliases:          []string{},
		EnableAutoMtls:              &wrappers.BoolValue{Value: true},
		OutboundTrafficPolicy:       &meshconfig.MeshConfig_OutboundTrafficPolicy{Mode: meshconfig.MeshConfig_OutboundTrafficPolicy_ALLOW_ANY},
		LocalityLbSetting: &v1alpha3.LocalityLoadBalancerSetting{
			Enabled: &wrappers.BoolValue{Value: true},
		},
		Certificates:  []*meshconfig.Certificate{},
		DefaultConfig: proxyConfig,

		RootNamespace:                  constants.IstioSystemNamespace,
		ProxyListenPort:                15001,
		ConnectTimeout:                 durationpb.New(10 * time.Second),
		DefaultServiceExportTo:         []string{"*"},
		DefaultVirtualServiceExportTo:  []string{"*"},
		DefaultDestinationRuleExportTo: []string{"*"},
		// DnsRefreshRate is only used when DNS requests fail (NXDOMAIN or SERVFAIL). For success, the TTL
		// will be used.
		// https://datatracker.ietf.org/doc/html/rfc2308#section-3 defines how negative DNS results should handle TTLs,
		// but Envoy does not respect this (https://github.com/envoyproxy/envoy/issues/20885).
		// To counter this, we bump up the default to 60s to avoid overloading DNS servers.
		DnsRefreshRate:  durationpb.New(60 * time.Second),
		ThriftConfig:    &meshconfig.MeshConfig_ThriftConfig{},
		ServiceSettings: make([]*meshconfig.MeshConfig_ServiceSettings, 0),

		DefaultProviders: &meshconfig.MeshConfig_DefaultProviders{},
		ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "prometheus",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_Prometheus{
					Prometheus: &meshconfig.MeshConfig_ExtensionProvider_PrometheusMetricsProvider{},
				},
			},
			{
				Name: "stackdriver",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_Stackdriver{
					Stackdriver: &meshconfig.MeshConfig_ExtensionProvider_StackdriverProvider{},
				},
			},
			{
				Name: "envoy",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: "/dev/stdout",
					},
				},
			},
		},
	}
}

// ApplyProxyConfig applies the give proxy config yaml to a mesh config object. The passed in mesh config
// will not be modified.
func ApplyProxyConfig(yaml string, meshConfig *meshconfig.MeshConfig) (*meshconfig.MeshConfig, error) {
	mc := proto.Clone(meshConfig).(*meshconfig.MeshConfig)
	pc, err := applyProxyConfig(yaml, mc.DefaultConfig)
	if err != nil {
		return nil, err
	}
	mc.DefaultConfig = pc
	return mc, nil
}

func applyProxyConfig(yaml string, proxyConfig *meshconfig.ProxyConfig) (*meshconfig.ProxyConfig, error) {
	origMetadata := proxyConfig.ProxyMetadata
	if err := protomarshal.ApplyYAML(yaml, proxyConfig); err != nil {
		return nil, fmt.Errorf("could not parse proxy config: %v", err)
	}
	newMetadata := proxyConfig.ProxyMetadata
	proxyConfig.ProxyMetadata = mergeMap(origMetadata, newMetadata)
	return proxyConfig, nil
}

func extractYamlField(key string, mp map[string]any) (string, error) {
	proxyConfig := mp[key]
	if proxyConfig == nil {
		return "", nil
	}
	bytes, err := yaml.Marshal(proxyConfig)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func toMap(yamlText string) (map[string]any, error) {
	mp := map[string]any{}
	if err := yaml.Unmarshal([]byte(yamlText), &mp); err != nil {
		return nil, err
	}
	return mp, nil
}

// ApplyMeshConfig returns a new MeshConfig decoded from the
// input YAML with the provided defaults applied to omitted configuration values.
func ApplyMeshConfig(yaml string, defaultConfig *meshconfig.MeshConfig) (*meshconfig.MeshConfig, error) {
	// We want to keep semantics that all fields are overrides, except proxy config is a merge. This allows
	// decent customization while also not requiring users to redefine the entire proxy config if they want to override
	// Note: if we want to add more structure in the future, we will likely need to revisit this idea.

	// Store the current set proxy config so we don't wipe it out, we will configure this later
	prevProxyConfig := defaultConfig.DefaultConfig
	prevDefaultProvider := defaultConfig.DefaultProviders
	prevExtensionProviders := defaultConfig.ExtensionProviders
	prevTrustDomainAliases := defaultConfig.TrustDomainAliases

	defaultConfig.DefaultConfig = DefaultProxyConfig()
	if err := protomarshal.ApplyYAML(yaml, defaultConfig); err != nil {
		return nil, multierror.Prefix(err, "failed to convert to proto.")
	}
	defaultConfig.DefaultConfig = prevProxyConfig

	raw, err := toMap(yaml)
	if err != nil {
		return nil, err
	}
	// Get just the proxy config yaml
	pc, err := extractYamlField("defaultConfig", raw)
	if err != nil {
		return nil, multierror.Prefix(err, "failed to extract proxy config")
	}
	if pc != "" {
		pc, err := applyProxyConfig(pc, defaultConfig.DefaultConfig)
		if err != nil {
			return nil, err
		}
		defaultConfig.DefaultConfig = pc
	}

	defaultConfig.DefaultProviders = prevDefaultProvider
	dp, err := extractYamlField("defaultProviders", raw)
	if err != nil {
		return nil, multierror.Prefix(err, "failed to extract default providers")
	}
	if dp != "" {
		if err := protomarshal.ApplyYAML(dp, defaultConfig.DefaultProviders); err != nil {
			return nil, fmt.Errorf("could not parse default providers: %v", err)
		}
	}

	newExtensionProviders := defaultConfig.ExtensionProviders
	defaultConfig.ExtensionProviders = prevExtensionProviders
	for _, p := range newExtensionProviders {
		found := false
		for _, e := range defaultConfig.ExtensionProviders {
			if p.Name == e.Name {
				e.Provider = p.Provider
				found = true
				break
			}
		}
		if !found {
			defaultConfig.ExtensionProviders = append(defaultConfig.ExtensionProviders, p)
		}
	}

	defaultConfig.TrustDomainAliases = sets.New(append(defaultConfig.TrustDomainAliases, prevTrustDomainAliases...)...).SortedList()

	if err := validation.ValidateMeshConfig(defaultConfig); err != nil {
		return nil, err
	}

	return defaultConfig, nil
}

func mergeMap(original map[string]string, merger map[string]string) map[string]string {
	if original == nil && merger == nil {
		return nil
	}
	if original == nil {
		original = map[string]string{}
	}
	for k, v := range merger {
		original[k] = v
	}
	return original
}

// ApplyMeshConfigDefaults returns a new MeshConfig decoded from the
// input YAML with defaults applied to omitted configuration values.
func ApplyMeshConfigDefaults(yaml string) (*meshconfig.MeshConfig, error) {
	return ApplyMeshConfig(yaml, DefaultMeshConfig())
}

func DeepCopyMeshConfig(mc *meshconfig.MeshConfig) (*meshconfig.MeshConfig, error) {
	j, err := protomarshal.ToJSON(mc)
	if err != nil {
		return nil, err
	}
	nmc := &meshconfig.MeshConfig{}
	if err := protomarshal.ApplyJSON(j, nmc); err != nil {
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
	if err := protomarshal.ApplyYAML(yaml, &out); err != nil {
		return nil, multierror.Prefix(err, "failed to convert to proto.")
	}

	if err := validation.ValidateMeshNetworks(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReadMeshNetworks gets mesh networks configuration from a config file
func ReadMeshNetworks(filename string) (*meshconfig.MeshNetworks, error) {
	yaml, err := os.ReadFile(filename)
	if err != nil {
		return nil, multierror.Prefix(err, "cannot read networks config file")
	}
	return ParseMeshNetworks(string(yaml))
}

// ReadMeshConfig gets mesh configuration from a config file
func ReadMeshConfig(filename string) (*meshconfig.MeshConfig, error) {
	yaml, err := os.ReadFile(filename)
	if err != nil {
		return nil, multierror.Prefix(err, "cannot read mesh config file")
	}
	return ApplyMeshConfigDefaults(string(yaml))
}

// ReadMeshConfigData gets mesh configuration yaml from a config file
func ReadMeshConfigData(filename string) (string, error) {
	yaml, err := os.ReadFile(filename)
	if err != nil {
		return "", multierror.Prefix(err, "cannot read mesh config file")
	}
	return string(yaml), nil
}
