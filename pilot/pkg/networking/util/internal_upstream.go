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
	rawbuffer "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/raw_buffer/v3"
	metadata "github.com/envoyproxy/go-control-plane/envoy/type/metadata/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/util/protoconv"
)

const (
	// PeerDirectRemoteIPFilterStateKey is a filter state key where a listener that terminates the downstream
	// connection, such as the HBONE CONNECT termination listener, stores the direct remote address of that
	// connection for the internal listener it forwards to. On the internal listener the direct remote address is an
	// internal Envoy address, so authorization policy matches source IPs against this value instead.
	//
	// The value is connection scoped: every CONNECT stream multiplexed on one HBONE connection has the same direct
	// peer. This matches ztunnel, which enforces ipBlocks against the peer of the HBONE connection too, and which
	// pools HBONE connections per source workload and source IP.
	PeerDirectRemoteIPFilterStateKey = "io.istio.peer_direct_remote_ip"

	// PeerDirectRemoteAddressFilterStateFactory creates the PeerDirectRemoteIPFilterStateKey object from an
	// "IP:port" string. Its objects are hashable addresses, so Envoy does not share an upstream internal connection
	// between downstream connections with different peers.
	PeerDirectRemoteAddressFilterStateFactory = "envoy.filters.listener.original_dst.remote_ip"
)

func RawBufferTransport() *core.TransportSocket {
	return &core.TransportSocket{
		Name:       "raw_buffer",
		ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&rawbuffer.RawBuffer{})},
	}
}

// InternalUpstreamTransportSocket wraps provided transport socket into Envoy InteralUpstreamTransport.
func InternalUpstreamTransportSocket(name string, transport *core.TransportSocket) *core.TransportSocket {
	return &core.TransportSocket{
		Name: name,
		ConfigType: &core.TransportSocket_TypedConfig{
			TypedConfig: protoconv.MessageToAny(
				&internalupstream.InternalUpstreamTransport{
					TransportSocket: transport,
				},
			),
		},
	}
}

// DefaultInternalUpstreamTransportSocket provides an internal_upstream transport that does not passthrough any metadata.
var DefaultInternalUpstreamTransportSocket = InternalUpstreamTransportSocket("internal_upstream", RawBufferTransport())

// WaypointInternalUpstreamTransportSocket builds an internal upstream transport socket suitable for usage in a waypoint
// This will passthrough the OrigDst key and HBONE destination address.
func WaypointInternalUpstreamTransportSocket(inner *core.TransportSocket) *core.TransportSocket {
	passthroughMetadata := []*internalupstream.InternalUpstreamTransport_MetadataValueSource{
		{
			Kind: &metadata.MetadataKind{Kind: &metadata.MetadataKind_Host_{Host: &metadata.MetadataKind_Host{}}},
			Name: OriginalDstMetadataKey,
		},
		{
			Kind: &metadata.MetadataKind{Kind: &metadata.MetadataKind_Host_{Host: &metadata.MetadataKind_Host{}}},
			Name: "istio",
		},
	}

	if features.EnableAmbientBaggage {
		passthroughMetadata = append(passthroughMetadata,
			&internalupstream.InternalUpstreamTransport_MetadataValueSource{
				Kind: &metadata.MetadataKind{Kind: &metadata.MetadataKind_Cluster_{Cluster: &metadata.MetadataKind_Cluster{}}},
				Name: "istio.peer_metadata",
			},
			&internalupstream.InternalUpstreamTransport_MetadataValueSource{
				Kind: &metadata.MetadataKind{Kind: &metadata.MetadataKind_Host_{Host: &metadata.MetadataKind_Host{}}},
				Name: "istio.peer_metadata",
			},
		)
	}

	return &core.TransportSocket{
		Name: "internal_upstream",
		ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&internalupstream.InternalUpstreamTransport{
			PassthroughMetadata: passthroughMetadata,
			TransportSocket:     inner,
		})},
	}
}

// FullMetadataPassthroughInternalUpstreamTransportSocket builds an internal upstream transport socket suitable for usage in
// originating HBONE. For waypoints, use WaypointInternalUpstreamTransportSocket.
func FullMetadataPassthroughInternalUpstreamTransportSocket(inner *core.TransportSocket) *core.TransportSocket {
	passthroughMetadata := []*internalupstream.InternalUpstreamTransport_MetadataValueSource{
		{
			Kind: &metadata.MetadataKind{Kind: &metadata.MetadataKind_Host_{Host: &metadata.MetadataKind_Host{}}},
			Name: OriginalDstMetadataKey,
		},
		{
			Kind: &metadata.MetadataKind{Kind: &metadata.MetadataKind_Cluster_{Cluster: &metadata.MetadataKind_Cluster{}}},
			Name: "istio",
		},
		{
			Kind: &metadata.MetadataKind{Kind: &metadata.MetadataKind_Host_{Host: &metadata.MetadataKind_Host{}}},
			Name: "istio",
		},
	}

	if features.EnableAmbientBaggage {
		passthroughMetadata = append(passthroughMetadata,
			&internalupstream.InternalUpstreamTransport_MetadataValueSource{
				Kind: &metadata.MetadataKind{Kind: &metadata.MetadataKind_Cluster_{Cluster: &metadata.MetadataKind_Cluster{}}},
				Name: "istio.peer_metadata",
			},
			&internalupstream.InternalUpstreamTransport_MetadataValueSource{
				Kind: &metadata.MetadataKind{Kind: &metadata.MetadataKind_Host_{Host: &metadata.MetadataKind_Host{}}},
				Name: "istio.peer_metadata",
			},
		)
	}

	return &core.TransportSocket{
		Name: "envoy.transport_sockets.internal_upstream",
		ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&internalupstream.InternalUpstreamTransport{
			PassthroughMetadata: passthroughMetadata,
			TransportSocket:     inner,
		})},
	}
}
