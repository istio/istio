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

package model

import (
	"fmt"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	"google.golang.org/protobuf/types/known/durationpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/util/protoconv"
)

var defaultConfigSource = &core.ConfigSource{
	ConfigSourceSpecifier: &core.ConfigSource_Ads{
		Ads: &core.AggregatedConfigSource{},
	},
	ResourceApiVersion:  core.ApiVersion_V3,
	InitialFetchTimeout: &durationpb.Duration{Seconds: 0},
}

// StatsProvider is the type of stats provider
// Only prometheus and stackdriver are supported currently
type StatsProvider string

const (
	StatsProviderPrometheus  StatsProvider = "prometheus"
	StatsProviderStackdriver StatsProvider = "stackdriver"
)

type StatsConfig struct {
	Provider         StatsProvider
	NodeType         NodeType
	ListenerClass    networking.ListenerClass
	ListenerProtocol networking.ListenerProtocol
}

func (cfg StatsConfig) String() string {
	return fmt.Sprintf("%s/%s/%v/%v", cfg.Provider, cfg.NodeType, cfg.ListenerClass, cfg.ListenerProtocol)
}

func StatsECDSResourceName(cfg StatsConfig) string {
	return fmt.Sprintf("istio.io/telemetry/stats/%s", cfg.String())
}

func buildHTTPTypedExtensionConfig(class networking.ListenerClass, metricsCfg []telemetryFilterConfig) []*core.TypedExtensionConfig {
	res := make([]*core.TypedExtensionConfig, 0, telemetryFilterHandled)
	for _, cfg := range metricsCfg {
		switch cfg.Provider.GetProvider().(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_Prometheus:
			if cfg.NodeType == Waypoint {
				res = append(res, &core.TypedExtensionConfig{
					Name: StatsECDSResourceName(StatsConfig{
						Provider:         StatsProviderPrometheus,
						NodeType:         cfg.NodeType,
						ListenerClass:    class,
						ListenerProtocol: networking.ListenerProtocolHTTP,
					}),
					TypedConfig: waypointStatsConfig,
				})
			} else {
				if statsCfg := generateStatsConfig(class, cfg); statsCfg != nil {
					res = append(res, &core.TypedExtensionConfig{
						Name: StatsECDSResourceName(StatsConfig{
							Provider:         StatsProviderPrometheus,
							NodeType:         cfg.NodeType,
							ListenerClass:    class,
							ListenerProtocol: networking.ListenerProtocolHTTP,
						}),
						TypedConfig: statsCfg,
					})
				}
			}
		case *meshconfig.MeshConfig_ExtensionProvider_Stackdriver:
			sdCfg := generateSDConfig(class, cfg)
			vmConfig := ConstructVMConfig("envoy.wasm.null.stackdriver")
			vmConfig.VmConfig.VmId = stackdriverVMID(class)

			wasmConfig := &wasm.PluginConfig{
				RootId:        vmConfig.VmConfig.VmId,
				Vm:            vmConfig,
				Configuration: sdCfg,
			}

			res = append(res, &core.TypedExtensionConfig{
				Name: StatsECDSResourceName(StatsConfig{
					Provider:         StatsProviderStackdriver,
					NodeType:         cfg.NodeType,
					ListenerClass:    class,
					ListenerProtocol: networking.ListenerProtocolHTTP,
				}),
				TypedConfig: protoconv.MessageToAny(wasmConfig),
			})
		default:
			// Only prometheus and SD supported currently
			continue
		}
	}
	return res
}

func buildTCPTypedExtensionConfig(class networking.ListenerClass, metricsCfg []telemetryFilterConfig) []*core.TypedExtensionConfig {
	res := make([]*core.TypedExtensionConfig, 0, telemetryFilterHandled)
	for _, cfg := range metricsCfg {
		switch cfg.Provider.GetProvider().(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_Prometheus:
			if cfg.NodeType == Waypoint {
				res = append(res, &core.TypedExtensionConfig{
					Name: StatsECDSResourceName(StatsConfig{
						Provider:         StatsProviderPrometheus,
						NodeType:         cfg.NodeType,
						ListenerClass:    class,
						ListenerProtocol: networking.ListenerProtocolTCP,
					}),
					TypedConfig: waypointStatsConfig,
				})
			} else {
				if statsCfg := generateStatsConfig(class, cfg); statsCfg != nil {
					res = append(res, &core.TypedExtensionConfig{
						Name: StatsECDSResourceName(StatsConfig{
							Provider:         StatsProviderPrometheus,
							NodeType:         cfg.NodeType,
							ListenerClass:    class,
							ListenerProtocol: networking.ListenerProtocolTCP,
						}),
						TypedConfig: statsCfg,
					})
				}
			}
		case *meshconfig.MeshConfig_ExtensionProvider_Stackdriver:
			sdCfg := generateSDConfig(class, cfg)
			vmConfig := ConstructVMConfig("envoy.wasm.null.stackdriver")
			vmConfig.VmConfig.VmId = stackdriverVMID(class)

			wasmConfig := &wasm.PluginConfig{
				RootId:        vmConfig.VmConfig.VmId,
				Vm:            vmConfig,
				Configuration: sdCfg,
			}

			res = append(res, &core.TypedExtensionConfig{
				Name: StatsECDSResourceName(StatsConfig{
					Provider:         StatsProviderStackdriver,
					NodeType:         cfg.NodeType,
					ListenerClass:    class,
					ListenerProtocol: networking.ListenerProtocolHTTP,
				}),
				TypedConfig: protoconv.MessageToAny(wasmConfig),
			})
		default:
			// Only prometheus and SD supported currently
			continue
		}
	}
	return res
}

func (t *Telemetries) HTTPTypedExtensionConfigFilters(proxy *Proxy, class networking.ListenerClass) []*core.TypedExtensionConfig {
	if !features.EnableECDSForStats.Get() {
		return nil
	}

	if res := t.telemetryFilters(proxy, class, networking.ListenerProtocolHTTP); res != nil {
		return res.([]*core.TypedExtensionConfig)
	}
	return nil
}

func (t *Telemetries) TCPTypedExtensionConfigFilters(proxy *Proxy, class networking.ListenerClass) []*core.TypedExtensionConfig {
	if !features.EnableECDSForStats.Get() {
		return nil
	}

	if res := t.telemetryFilters(proxy, class, networking.ListenerProtocolTCP); res != nil {
		return res.([]*core.TypedExtensionConfig)
	}
	return nil
}
