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

package telemetry

import (
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pkg/util/sets"
)

var allClasses = []networking.ListenerClass{
	networking.ListenerClassSidecarInbound,
	networking.ListenerClassSidecarOutbound,
	networking.ListenerClassGateway,
}

var providers = []model.StatsProvider{
	model.StatsProviderStackdriver,
	model.StatsProviderPrometheus,
}

func InsertedExtensionConfigurations(proxy *model.Proxy, push *model.PushContext, extensionConfigNames []string) []*core.TypedExtensionConfig {
	hasNames := sets.New(extensionConfigNames...)
	result := make([]*core.TypedExtensionConfig, 0)

	for _, c := range allClasses {
		for _, p := range providers {
			resourceName := model.StatsECDSResourceName(model.StatsConfig{
				Provider:         p,
				NodeType:         proxy.Type,
				ListenerClass:    c,
				ListenerProtocol: networking.ListenerProtocolHTTP,
			})
			if hasNames.Contains(resourceName) {
				result = append(result, push.Telemetry.HTTPTypedExtensionConfigFilters(proxy, c)...)
			}

			resourceName = model.StatsECDSResourceName(model.StatsConfig{
				Provider:         p,
				NodeType:         proxy.Type,
				ListenerClass:    c,
				ListenerProtocol: networking.ListenerProtocolTCP,
			})
			if hasNames.Contains(resourceName) {
				result = append(result, push.Telemetry.TCPTypedExtensionConfigFilters(proxy, c)...)
			}
		}
	}

	return result
}
