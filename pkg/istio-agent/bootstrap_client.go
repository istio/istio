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

type bootstrapDiscoveryClient struct {
	node        *model.Node
	envoyWaitCh chan error
	envoyUpdate func(data []byte) error
	sent        bool
	received    bool
}

// Send refers to a request from the xDS proxy.
func (b *bootstrapDiscoveryClient) Send(resp *discovery.DiscoveryResponse) error {
	if resp.TypeUrl == v3.BootstrapType && !b.received {
		b.received = true
		if len(resp.Resources) != 1 {
			b.envoyWaitCh <- fmt.Errorf("unexpected number of bootstraps: %d", len(resp.Resources))
			return nil
		}
		var bs bootstrapv3.Bootstrap
		if err := resp.Resources[0].UnmarshalTo(&bs); err != nil {
			b.envoyWaitCh <- fmt.Errorf("failed to unmarshal bootstrap: %v", err)
			return nil
		}
		by, err := protomarshal.MarshalIndent(&bs, "  ")
		if err != nil {
			b.envoyWaitCh <- fmt.Errorf("failed to marshal bootstrap as JSON: %v", err)
			return nil
		}
		if err := b.envoyUpdate(by); err != nil {
			b.envoyWaitCh <- fmt.Errorf("failed to update bootstrap from discovery: %v", err)
			return nil
		}
		close(b.envoyWaitCh)
	}
	return nil
}

// Recv Receive refers to a request to the xDS proxy.
func (b *bootstrapDiscoveryClient) Recv() (*discovery.DiscoveryRequest, error) {
	if b.sent {
		<-b.envoyWaitCh
		return nil, io.EOF
	}
	b.sent = true
	return &discovery.DiscoveryRequest{
		TypeUrl: v3.BootstrapType,
		Node:    bootstrap.ConvertNodeToXDSNode(b.node),
	}, nil
}

func (b *bootstrapDiscoveryClient) Context() context.Context { return context.Background() }
