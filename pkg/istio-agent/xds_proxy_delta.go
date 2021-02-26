package istioagent

import (
	"context"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"istio.io/istio/pilot/pkg/features"
	nds "istio.io/istio/pilot/pkg/proto"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/istio-agent/metrics"
	"time"
)

// requests from envoy
// for aditya:
// downstream -> envoy (anything "behind" xds proxy)
// upstream -> istiod (in front of xds proxy)?
func (p *XdsProxy) DeltaAggregatedResources(downstream discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	//return errors.New("delta XDS is not implemented")

	proxyLog.Debugf("accepted delta xds connection from envoy, forwarding to upstream")
	con := &ProxyConnection{
		upstreamError:   make(chan error, 2), // can be produced by recv and send
		downstreamError: make(chan error, 2), // can be produced by recv and send
		deltaRequestsChan: make(chan *discovery.DeltaDiscoveryRequest, 10),
		deltaResponsesChan: make(chan *discovery.DeltaDiscoveryResponse, 10),
		stopChan:         make(chan struct{}),
		downstreamDeltas: downstream,
	}
	p.RegisterStream(con)
	defer p.UnregisterStream(con)

	// todo is there a better way to abstract this out?
	initialReqSent := false
	go p.relayEnvoyToIstiod(&initialReqSent)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	upstreamConn, err := grpc.DialContext(ctx, p.istiodAddress, p.istiodDialOptions...)
	if err != nil {
		proxyLog.Errorf("failed to connect to upstream %s: %v", p.istiodAddress, err)
		metrics.IstiodConnectionFailures.Increment()
		return err
	}
	defer func() {
		err = upstreamConn.Close()
		if err != nil {
			proxyLog.Debugf("could not disconnect from istiod: %v", err)
		}
	}()

	xds := discovery.NewAggregatedDiscoveryServiceClient(upstreamConn)
	ctx = metadata.AppendToOutgoingContext(context.Background(), "ClusterID", p.clusterID)
	for k, v := range p.xdsHeaders {
		ctx = metadata.AppendToOutgoingContext(ctx, k, v)
	}
	return p.HandleDeltaUpstream(ctx, con, xds)
}

func (p *XdsProxy) relayEnvoyToIstiod(initialReqSent* bool) {

	p.connectedMutex.RLock()
	initialDeltaRequest := p.initialDeltaRequest
	p.connectedMutex.RUnlock()
	for {
		// from envoy
		req, err := p.connected.downstreamDeltas.Recv()
		if err != nil {
			p.connected.downstreamError <- err
			return
		}
		// to istiod
		p.connected.deltaRequestsChan <- req
		if !*initialReqSent && req.TypeUrl == v3.ListenerType {
			// fire init nds req
			if p.localDNSServer != nil {
				p.connected.deltaRequestsChan <- &discovery.DeltaDiscoveryRequest{
					TypeUrl: v3.NameTableType,
				}
			}
			if initialDeltaRequest != nil {
				p.connected.deltaRequestsChan <- initialDeltaRequest
			}
			*initialReqSent = true
		}
	}
}

func (p *XdsProxy) HandleDeltaUpstream(ctx context.Context, con *ProxyConnection, xds discovery.AggregatedDiscoveryServiceClient) error {
	deltaUpstream, err := xds.DeltaAggregatedResources(ctx, grpc.MaxCallRecvMsgSize(defaultClientMaxReceiveMessageSize))
	if err != nil {
		proxyLog.Debugf("failed to create delta upstream grpc client: %v", err)
		return err
	}
	proxyLog.Infof("connected to delta upstream XDS server: %s", p.istiodAddress)
	defer proxyLog.Debugf("disconnected from delta XDS server: %s", p.istiodAddress)

	con.upstreamDeltas = deltaUpstream

	// handle responses from istiod
	go func() {
		for {
			resp, err := deltaUpstream.Recv()
			if err != nil {
				con.upstreamError <- err
				return
			}
			con.deltaResponsesChan <- resp
		}
	}()

	go p.handleUpstreamDeltaRequest(ctx, con)
	go p.handleUpstreamDeltaResponse(con)

	// todo wasm load conversion
	for {
		select {
		case err := <-con.upstreamError:
			// error from upstream Istiod.
			if isExpectedGRPCError(err) {
				proxyLog.Debugf("upstream terminated with status %v", err)
				metrics.IstiodConnectionCancellations.Increment()
			} else {
				proxyLog.Warnf("upstream terminated with unexpected error %v", err)
				metrics.IstiodConnectionErrors.Increment()
			}
			return nil
		case err := <-con.downstreamError:
			// error from downstream Envoy.
			if isExpectedGRPCError(err) {
				proxyLog.Debugf("downstream terminated with status %v", err)
				metrics.EnvoyConnectionCancellations.Increment()
			} else {
				proxyLog.Warnf("downstream terminated with unexpected error %v", err)
				metrics.EnvoyConnectionErrors.Increment()
			}
			// On downstream error, we will return. This propagates the error to downstream envoy which will trigger reconnect
			return err
		case <-con.stopChan:
			proxyLog.Debugf("stream stopped")
			return nil
		}
	}
}

func (p* XdsProxy) handleUpstreamDeltaRequest(ctx context.Context, con* ProxyConnection) {
	defer con.upstreamDeltas.CloseSend()
	for {
		select {
		case req := <-con.deltaRequestsChan:
			proxyLog.Debugf("delta request for type url %s", req.TypeUrl)
			metrics.XdsProxyRequests.Increment()
			if req.TypeUrl == v3.ExtensionConfigurationType {
				p.ecdsLastNonce.Store(req.ResponseNonce)
			}
			if err := sendUpstreamDeltaWithTimeout(ctx, con.upstreamDeltas, req); err != nil {
				proxyLog.Errorf("upstream send error for type url %s: %v", req.TypeUrl, err)
				con.upstreamError <- err
				return
			}
		case <-con.stopChan:
			return
		}
	}
}

func (p *XdsProxy) handleUpstreamDeltaResponse(con *ProxyConnection) {
	for {
		select {
		case resp := <- con.deltaResponsesChan:
			proxyLog.Debugf("delta response for type url %s", resp.TypeUrl)
			metrics.XdsProxyResponses.Increment()
			switch resp.TypeUrl {
			case v3.NameTableType:
				// intercept. This is for the dns server
				if p.localDNSServer != nil && len(resp.Resources) > 0 {
					var nt nds.NameTable
					// TODO we should probably send ACK and not update nametable here
					if err := ptypes.UnmarshalAny(resp.Resources[0].Resource, &nt); err != nil {
						proxyLog.Errorf("failed to unmarshall name table: %v", err)
					}
					p.localDNSServer.UpdateLookupTable(&nt)
				}

				// Send ACK
				con.deltaRequestsChan <- &discovery.DeltaDiscoveryRequest{
					TypeUrl:       v3.NameTableType,
					ResponseNonce: resp.Nonce,
				}
			case v3.ExtensionConfigurationType:
				if features.WasmRemoteLoadConversion {
					// If Wasm remote load conversion feature is enabled, push ECDS update into
					// conversion channel.
					p.ecdsDeltaUpdateChan <- resp
				} else {
					// Otherwise, forward ECDS resource update directly to Envoy.
					forwardDeltaToEnvoy(con, resp)
				}
			default:
				forwardDeltaToEnvoy(con, resp)
			}
		case <-con.stopChan:
			return
		}
	}
}

func forwardDeltaToEnvoy(con *ProxyConnection, resp *discovery.DeltaDiscoveryResponse) {
	if err := sendDownstreamDeltaWithTimout(con.downstreamDeltas, resp); err != nil {
		select {
		case con.downstreamError <- err:
			proxyLog.Errorf("downstream send error: %v", err)
		default:
			proxyLog.Debugf("downstream error channel full, but get downstream send error: %v", err)
		}

		return
	}
}

func sendUpstreamDeltaWithTimeout(ctx context.Context, deltaUpstream discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesClient,
	req *discovery.DeltaDiscoveryRequest) error {
	return sendWithTimeout(ctx, func(errChan chan error) {
		errChan <- deltaUpstream.Send(req)
		close(errChan)
	})
}

func sendDownstreamDeltaWithTimout(deltaUpstream discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer,
	req *discovery.DeltaDiscoveryResponse) error {
	return sendWithTimeout(context.Background(), func(errChan chan error) {
		errChan <- deltaUpstream.Send(req)
		close(errChan)
	})
}

func (p *XdsProxy) PersistDeltaRequest(req *discovery.DeltaDiscoveryRequest) {
	var ch chan *discovery.DeltaDiscoveryRequest

	p.connectedMutex.Lock()
	if p.connected != nil {
		ch = p.connected.deltaRequestsChan
	}
	p.initialDeltaRequest = req
	p.connectedMutex.Unlock()

	// Immediately send if we are currently connect
	if ch != nil {
		ch <- req
	}
}

