// Copyright 2020 Istio Authors
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

package xds

import (
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	corev2 "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discoveryv2 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// DiscoveryStreamV2Adapter is a DiscoveryStream that converts v3 Discovery messages to v2 messages.
// This allows v2 clients to connect, while keeping the core logic of Pilot in v3 XDS.
// This comes with a very small performance impact, as most resources are directly copied over - most
// importantly the actual XDS response objects are untouched.
// Note: this is just about the transport protocol. The XDS resource versioning is independent of transport
// protocol
type DiscoveryStreamV2Adapter struct {
	discoveryv2.AggregatedDiscoveryService_StreamAggregatedResourcesServer
}

// We implement the v3 DiscoveryStream API
var _ DiscoveryStream = &DiscoveryStreamV2Adapter{}

func (d DiscoveryStreamV2Adapter) Send(v3Resp *discovery.DiscoveryResponse) error {
	return d.AggregatedDiscoveryService_StreamAggregatedResourcesServer.Send(DowngradeV3Response(v3Resp))
}

func (d DiscoveryStreamV2Adapter) Recv() (*discovery.DiscoveryRequest, error) {
	v2Req, err := d.AggregatedDiscoveryService_StreamAggregatedResourcesServer.Recv()
	return UpgradeV2Request(v2Req), err
}

// Convert from v2 to v3
func UpgradeV2Request(v2Req *xdsapi.DiscoveryRequest) *discovery.DiscoveryRequest {
	if v2Req == nil {
		return nil
	}
	return &discovery.DiscoveryRequest{
		VersionInfo:   v2Req.VersionInfo,
		Node:          convertToV3Node(v2Req.Node),
		ResourceNames: v2Req.ResourceNames,
		TypeUrl:       v2Req.TypeUrl,
		ResponseNonce: v2Req.ResponseNonce,
		ErrorDetail:   v2Req.ErrorDetail,
	}
}

// DowngradeV3Response converts from v3 to v2
func DowngradeV3Response(v3Resp *discovery.DiscoveryResponse) *xdsapi.DiscoveryResponse {
	if v3Resp == nil {
		return nil
	}
	return &xdsapi.DiscoveryResponse{
		ControlPlane: convertToV2ControlPlane(v3Resp.ControlPlane),
		VersionInfo:  v3Resp.VersionInfo,
		Resources:    v3Resp.Resources,
		Canary:       v3Resp.Canary,
		TypeUrl:      v3Resp.TypeUrl,
		Nonce:        v3Resp.Nonce,
	}
}

func convertToV2ControlPlane(cp *corev3.ControlPlane) *corev2.ControlPlane {
	if cp == nil {
		return nil
	}
	return &corev2.ControlPlane{Identifier: cp.Identifier}
}

func convertToV3Node(node *corev2.Node) *corev3.Node {
	if node == nil {
		return nil
	}
	// We don't copy some fields we don't use like extensions and user_agent_name to avoid some extra copies
	// since we do not use them.
	resp := &corev3.Node{
		Id:       node.Id,
		Cluster:  node.Cluster,
		Metadata: node.Metadata,
	}
	if node.Locality != nil {
		l := node.Locality
		resp.Locality = &corev3.Locality{
			Region:  l.Region,
			Zone:    l.Zone,
			SubZone: l.SubZone,
		}
	}
	return resp
}

// discoveryServerV2Adapter is a DiscoveryServer that converts v3 Discovery messages to v2 messages.
// See notes in DiscoveryStreamV2Adapter for more info.
type discoveryServerV2Adapter struct {
	s *DiscoveryServer
}

// We implement the v2 ADS API
var _ discoveryv2.AggregatedDiscoveryServiceServer = &discoveryServerV2Adapter{}

func (d *discoveryServerV2Adapter) StreamAggregatedResources(stream discoveryv2.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	v3Stream := &DiscoveryStreamV2Adapter{stream}
	peerInfo, ok := peer.FromContext(stream.Context())
	peerAddr := "0.0.0.0"
	if ok {
		peerAddr = peerInfo.Addr.String()
	}
	adsLog.Infof("ADS: starting legacy v2 discovery stream from %v", peerAddr)
	return d.s.StreamAggregatedResources(v3Stream)
}

func (d discoveryServerV2Adapter) DeltaAggregatedResources(server discoveryv2.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return status.Errorf(codes.Unimplemented, "not implemented")
}

func (s *DiscoveryServer) createV2Adapter() discoveryv2.AggregatedDiscoveryServiceServer {
	return &discoveryServerV2Adapter{s}
}
