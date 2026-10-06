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
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	internalupstream "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/internal_upstream/v3"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
)

const (
	HBONEOriginationShimListener           = "connect_originate_hbone_shim"
	HBONEOriginationShimFilter             = "envoy.filters.network.istio_hbone"
	HBONEOriginationShimTransport          = "envoy.transport_sockets.istio_hbone"
	HBONEOriginationShimConfigType         = "type.googleapis.com/envoy.extensions.filters.network.istio_hbone.v3alpha.Config"
	HBONEOriginationShimUpstreamConfigType = "type.googleapis.com/envoy.extensions.filters.network.istio_hbone.v3alpha.UpstreamConfig"
)

// HBONEOriginationShimEnabled is an explicit capability opt-in for custom proxy images.
// The POC only supports single-HBONE origination on waypoints and ingress gateways.
func HBONEOriginationShimEnabled(proxy *model.Proxy) bool {
	if proxy == nil || proxy.Metadata == nil || !bool(proxy.Metadata.EnableHBONEOriginationShim) ||
		bool(proxy.Metadata.DisableHBONESend) || proxy.IsAmbientEastWestGateway() || features.EnableAmbientMultiNetwork {
		return false
	}
	return proxy.IsWaypointProxy() || (proxy.Type == model.Router && features.EnableHBONESend)
}

// UseHBONEOriginationShim must be applied to both CDS and EDS. Unsupported traffic keeps
// the original internal transport and listener; it must never be paired with half of the shim.
// Restrict the POC to EDS services without application TLS or PROXY protocol origination.
func UseHBONEOriginationShim(enabled bool, service *model.Service, policy *networking.TrafficPolicy) bool {
	if !enabled || service == nil || service.Resolution != model.ClientSideLB || policy.GetProxyProtocol() != nil {
		return false
	}
	switch policy.GetTls().GetMode() {
	case networking.ClientTLSSettings_DISABLE, networking.ClientTLSSettings_ISTIO_MUTUAL:
		return true
	default:
		return false
	}
}

// HBONEOriginationShimTransportSocket carries the same host/cluster metadata as the
// internal upstream transport. The shim itself supplies its raw buffer transport.
func HBONEOriginationShimTransportSocket(metadata []*internalupstream.InternalUpstreamTransport_MetadataValueSource) *core.TransportSocket {
	passthrough := make([]any, 0, len(metadata))
	for _, source := range metadata {
		kind := "host"
		if source.GetKind().GetCluster() != nil {
			kind = "cluster"
		}
		passthrough = append(passthrough, map[string]any{
			"name": source.Name,
			"kind": map[string]any{kind: map[string]any{}},
		})
	}
	return &core.TransportSocket{
		Name: HBONEOriginationShimTransport,
		ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.TypedStructWithFields(
			HBONEOriginationShimUpstreamConfigType, map[string]any{"passthrough_metadata": passthrough},
		)},
	}
}
