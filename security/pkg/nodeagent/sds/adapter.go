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

package sds

import (
	"context"

	xdsapiv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	discoveryv2 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/xds"
)

// sdsServiceAdapter is a sdsservice that converts v3 Discovery messages to v2 messages.
// See notes in DiscoveryStreamV2Adapter for more info.
type sdsServiceAdapter struct {
	s *sdsservice
}

func (d *sdsServiceAdapter) FetchSecrets(ctx context.Context, request *xdsapiv2.DiscoveryRequest) (*xdsapiv2.DiscoveryResponse, error) {
	v3Resp, err := d.s.FetchSecrets(ctx, xds.UpgradeV2Request(request))
	return xds.DowngradeV3Response(v3Resp), err
}

// We implement the v2 ADS API
var _ discoveryv2.SecretDiscoveryServiceServer = &sdsServiceAdapter{}

func (d *sdsServiceAdapter) StreamSecrets(stream discoveryv2.SecretDiscoveryService_StreamSecretsServer) error {
	v3Stream := &xds.DiscoveryStreamV2Adapter{stream}
	sdsServiceLog.Infof("SDS: starting legacy v2 discovery stream")
	return d.s.StreamSecrets(v3Stream)
}

func (d sdsServiceAdapter) DeltaSecrets(stream discoveryv2.SecretDiscoveryService_DeltaSecretsServer) error {
	return status.Error(codes.Unimplemented, "DeltaSecrets not implemented")
}

func (s *sdsservice) createV2Adapter() discoveryv2.SecretDiscoveryServiceServer {
	return &sdsServiceAdapter{s}
}
