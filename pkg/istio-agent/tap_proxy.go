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

package istioagent

import (
	"fmt"
	"strings"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	istiogrpc "istio.io/istio/pilot/pkg/grpc"
	"istio.io/istio/pilot/pkg/xds"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	"istio.io/pkg/log"
)

type tapProxy struct {
	xdsProxy *XdsProxy
}

func NewTapGrpcHandler(xdsProxy *XdsProxy) (*grpc.Server, error) {
	proxy := &tapProxy{
		xdsProxy: xdsProxy,
	}
	grpcs := grpc.NewServer(istiogrpc.ServerOptions(istiokeepalive.DefaultOption())...)
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcs, proxy)
	reflection.Register(grpcs)
	return grpcs, nil
}

func (p *tapProxy) StreamAggregatedResources(downstream xds.DiscoveryStream) error {
	timeout := time.Second * 15
	req, err := downstream.Recv()
	if err != nil {
		log.Errorf("failed to recv: %v", err)
		return err
	}
	if strings.HasPrefix(req.TypeUrl, "istio.io/debug/") {
		if resp, err := p.xdsProxy.tapRequest(req, timeout); err == nil {
			err := downstream.Send(resp)
			if err != nil {
				log.Errorf("failed to send: %v", err)
				return err
			}
		} else {
			log.Errorf("failed to call tap request: %v", err)
			return err
		}
	}
	return nil
}

func (p *tapProxy) DeltaAggregatedResources(downstream xds.DeltaDiscoveryStream) error {
	return fmt.Errorf("not implemented")
}
