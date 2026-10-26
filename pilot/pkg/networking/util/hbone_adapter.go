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
	HBONERejectionPool                        = "envoy.upstreams.istio_hbone"
	HBONERejectionPoolType                    = "type.googleapis.com/envoy.extensions.filters.network.istio_hbone.v3alpha.ConnectionPoolConfig"
	HBONEOuterOriginationAdapterListener      = "outer_connect_originate_hbone_adapter"
	HBONEOriginationAdapterListener           = "connect_originate_hbone_adapter"
	HBONEInnerOriginationAdapterListener      = "inner_connect_originate_hbone_adapter"
	HBONEOriginationAdapterFilter             = "envoy.filters.network.istio_hbone"
	HBONEOriginationAdapterTransport          = "envoy.transport_sockets.istio_hbone"
	HBONEOriginationAdapterConfigType         = "type.googleapis.com/envoy.extensions.filters.network.istio_hbone.v3alpha.Config"
	HBONEOriginationAdapterUpstreamConfigType = "type.googleapis.com/envoy.extensions.filters.network.istio_hbone.v3alpha.UpstreamConfig"
)

// HBONEConnectWaitEnabled opts a proxy into the complete HBONE CONNECT wait configuration.
// Supports single HBONE on sidecars, waypoints, and ingress gateways, and double HBONE
// on proxies with native double-HBONE resources.
func HBONEConnectWaitEnabled(proxy *model.Proxy) bool {
	if proxy == nil || proxy.Metadata == nil || !bool(proxy.Metadata.EnableHBONEConnectWait) ||
		bool(proxy.Metadata.DisableHBONESend) || proxy.IsProxylessGrpc() || proxy.IsAmbientEastWestGateway() {
		return false
	}
	// Enabling the transport also affects remote EDS entries. Require the nested
	// listeners and clusters before opting a multi-network proxy into the adapter.
	if features.EnableAmbientMultiNetwork && !model.ShouldCreateDoubleHBONEResources(proxy) {
		return false
	}
	return proxy.IsWaypointProxy() ||
		((proxy.Type == model.SidecarProxy || proxy.Type == model.Router) && features.EnableHBONESend)
}

// UseHBONEConnectWait must be applied to both CDS and EDS. Unsupported traffic keeps
// the original internal transport and listener; it must never be paired with half of the adapter.
// Restrict CONNECT wait to EDS services without application TLS or PROXY protocol origination.
func UseHBONEConnectWait(enabled bool, service *model.Service, policy *networking.TrafficPolicy) bool {
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

// HBONEOriginationAdapterTransportSocket carries the same host/cluster metadata as the
// internal upstream transport. The adapter itself supplies its raw buffer transport.
func HBONEOriginationAdapterTransportSocket(metadata []*internalupstream.InternalUpstreamTransport_MetadataValueSource) *core.TransportSocket {
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
		Name: HBONEOriginationAdapterTransport,
		ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.TypedStructWithFields(
			HBONEOriginationAdapterUpstreamConfigType, map[string]any{"passthrough_metadata": passthrough},
		)},
	}
}
