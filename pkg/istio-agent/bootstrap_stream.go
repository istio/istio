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
	"context"
	"fmt"
	"io"

	bootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/bootstrap"
	"istio.io/istio/pkg/util/protomarshal"
)

type bootstrapDiscoveryStream struct {
	node        *model.Node
	errCh       chan error
	envoyUpdate func(data []byte) error
	sent        bool
	received    bool
}

// Send refers to a request from the xDS proxy.
func (b *bootstrapDiscoveryStream) Send(resp *discovery.DiscoveryResponse) error {
	if resp.TypeUrl == v3.BootstrapType && !b.received {
		b.received = true
		if len(resp.Resources) != 1 {
			b.errCh <- fmt.Errorf("unexpected number of bootstraps: %d", len(resp.Resources))
			return nil
		}
		var bs bootstrapv3.Bootstrap
		if err := resp.Resources[0].UnmarshalTo(&bs); err != nil {
			sendToChannelWithoutBlock(b.errCh, fmt.Errorf("failed to unmarshal bootstrap: %v", err))
			return nil
		}
		by, err := protomarshal.MarshalIndent(&bs, "  ")
		if err != nil {
			sendToChannelWithoutBlock(b.errCh, fmt.Errorf("failed to marshal bootstrap as JSON: %v", err))
			return nil
		}
		if err := b.envoyUpdate(by); err != nil {
			sendToChannelWithoutBlock(b.errCh, fmt.Errorf("failed to update bootstrap from discovery: %v", err))
			return nil
		}
		select {
		case <-b.errCh:
		default:
			close(b.errCh)
		}
	}
	return nil
}

// Recv Receive refers to a request to the xDS proxy.
func (b *bootstrapDiscoveryStream) Recv() (*discovery.DiscoveryRequest, error) {
	if b.sent {
		<-b.errCh
		return nil, io.EOF
	}
	b.sent = true
	return &discovery.DiscoveryRequest{
		TypeUrl: v3.BootstrapType,
		Node:    bootstrap.ConvertNodeToXDSNode(b.node),
	}, nil
}

func (b *bootstrapDiscoveryStream) Context() context.Context { return context.Background() }

func sendToChannelWithoutBlock(errCh chan error, err error) {
	select {
	case errCh <- err:
	default:
	}
}
