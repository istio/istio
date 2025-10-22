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
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"go.uber.org/atomic"
	google_rpc "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	anypb "google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/channels"
	"istio.io/istio/pkg/istio-agent/metrics"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/wasm"
)

// sendDeltaRequest is a small wrapper around sending to con.requestsChan. This ensures that we do not
// block forever on
func (con *ProxyConnection) sendDeltaRequest(req *discovery.DeltaDiscoveryRequest) {
	con.deltaRequestsChan.Put(req)
}

// DeltaAggregatedResources is an implementation of Delta XDS API used for proxying between Istiod and Envoy.
// Every time envoy makes a fresh connection to the agent, we reestablish a new connection to the upstream xds
// This ensures that a new connection between istiod and agent doesn't end up consuming pending messages from envoy
// as the new connection may not go to the same istiod. Vice versa case also applies.
func (p *XdsProxy) DeltaAggregatedResources(downstream DeltaDiscoveryStream) error {
	proxyLog.Debugf("accepted delta xds connection from envoy, forwarding to upstream")

	con := &ProxyConnection{
		conID:             connectionNumber.Inc(),
		upstreamError:     make(chan error), // can be produced by recv and send
		downstreamError:   make(chan error), // can be produced by recv and send
		deltaRequestsChan: channels.NewUnbounded[*discovery.DeltaDiscoveryRequest](),
		// Allow a buffer of 1. This ensures we queue up at most 2 (one in process, 1 pending) responses before forwarding.
		deltaResponsesChan: make(chan *discovery.DeltaDiscoveryResponse, 1),
		stopChan:           make(chan struct{}),
		downstreamDeltas:   downstream,
	}
	p.registerStream(con)
	defer p.unregisterStream(con)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	upstreamConn, err := p.buildUpstreamConn(ctx)
	if err != nil {
		proxyLog.Errorf("failed to connect to upstream %s: %v", p.istiodAddress, err)
		metrics.IstiodConnectionFailures.Increment()
		return err
	}
	defer upstreamConn.Close()

	xds := discovery.NewAggregatedDiscoveryServiceClient(upstreamConn)
	ctx = metadata.AppendToOutgoingContext(context.Background(), "ClusterID", p.clusterID)
	for k, v := range p.xdsHeaders {
		ctx = metadata.AppendToOutgoingContext(ctx, k, v)
	}
	// We must propagate upstream termination to Envoy. This ensures that we resume the full XDS sequence on new connection
	return p.handleDeltaUpstream(ctx, con, xds)
}

func (p *XdsProxy) handleDeltaUpstream(ctx context.Context, con *ProxyConnection, xds discovery.AggregatedDiscoveryServiceClient) error {
	log := proxyLog.WithLabels("id", con.conID)
	deltaUpstream, err := xds.DeltaAggregatedResources(ctx,
		grpc.MaxCallRecvMsgSize(defaultClientMaxReceiveMessageSize))
	if err != nil {
		// Envoy logs errors again, so no need to log beyond debug level
		log.Debugf("failed to create delta upstream grpc client: %v", err)
		// Increase metric when xds connection error, for example: forgot to restart ingressgateway or sidecar after changing root CA.
		metrics.IstiodConnectionErrors.Increment()
		return err
	}
	log.Infof("connected to delta upstream XDS server: %s", p.istiodAddress)
	defer log.Debugf("disconnected from delta XDS server: %s", p.istiodAddress)

	con.upstreamDeltas = deltaUpstream

	// handle responses from istiod
	go func() {
		for {
			resp, err := con.upstreamDeltas.Recv()
			if err != nil {
				upstreamErr(con, err)
				return
			}
			select {
			case con.deltaResponsesChan <- resp:
			case <-con.stopChan:
			}
		}
	}()

	go p.handleUpstreamDeltaRequest(con)
	go p.handleUpstreamDeltaResponse(con)

	for {
		select {
		case err := <-con.upstreamError:
			return err
		case err := <-con.downstreamError:
			// On downstream error, we will return. This propagates the error to downstream envoy which will trigger reconnect
			return err
		case <-con.stopChan:
			log.Debugf("upstream stopped")
			return nil
		}
	}
}

func (p *XdsProxy) handleUpstreamDeltaRequest(con *ProxyConnection) {
	log := proxyLog.WithLabels("id", con.conID)
	initialRequestsSent := atomic.NewBool(false)
	go func() {
		for {
			// recv delta xds requests from envoy
			req, err := con.downstreamDeltas.Recv()
			if err != nil {
				downstreamErr(con, err)
				return
			}

			// forward to istiod
			con.sendDeltaRequest(req)
			if !initialRequestsSent.Load() && req.TypeUrl == model.ListenerType {
				// fire off an initial NDS request
				if _, f := p.handlers[model.NameTableType]; f {
					con.sendDeltaRequest(&discovery.DeltaDiscoveryRequest{
						TypeUrl: model.NameTableType,
					})
				}
				// fire off an initial PCDS request
				if _, f := p.handlers[model.ProxyConfigType]; f {
					con.sendDeltaRequest(&discovery.DeltaDiscoveryRequest{
						TypeUrl: model.ProxyConfigType,
					})
				}
				// set flag before sending the initial request to prevent race.
				initialRequestsSent.Store(true)
				// Fire of a configured initial request, if there is one
				p.connectionsMutex.RLock()
				initialRequest := p.initialDeltaHealthRequest
				if initialRequest != nil {
					con.sendDeltaRequest(initialRequest)
				}
				p.connectionsMutex.RUnlock()
			}
		}
	}()

	defer func() {
		_ = con.upstreamDeltas.CloseSend()
	}()
	for {
		select {
		case req := <-con.deltaRequestsChan.Get():
			con.deltaRequestsChan.Load()
			if req.TypeUrl == model.HealthInfoType && !initialRequestsSent.Load() {
				// only send healthcheck probe after LDS request has been sent
				continue
			}
			log.WithLabels(
				"type", model.GetShortType(req.TypeUrl),
				"sub", len(req.ResourceNamesSubscribe),
				"unsub", len(req.ResourceNamesUnsubscribe),
				"nonce", req.ResponseNonce,
				"initial", len(req.InitialResourceVersions),
			).Debugf("delta request")
			metrics.XdsProxyRequests.Increment()
			if req.TypeUrl == model.ExtensionConfigurationType {
				p.ecdsLastNonce.Store(req.ResponseNonce)
			}

			if err := con.upstreamDeltas.Send(req); err != nil {
				err = fmt.Errorf("send error for type url %s: %v", req.TypeUrl, err)
				upstreamErr(con, err)
				return
			}
		case <-con.stopChan:
			return
		}
	}
}

func (p *XdsProxy) handleUpstreamDeltaResponse(con *ProxyConnection) {
	forwardEnvoyCh := make(chan *discovery.DeltaDiscoveryResponse, 1)
	for {
		select {
		case resp := <-con.deltaResponsesChan:
			// TODO: separate upstream response handling from requests sending, which are both time costly
			proxyLog.WithLabels(
				"id", con.conID,
				"type", model.GetShortType(resp.TypeUrl),
				"nonce", resp.Nonce,
				"resources", len(resp.Resources),
				"removes", len(resp.RemovedResources),
			).Debugf("upstream response")
			metrics.XdsProxyResponses.Increment()
			if h, f := p.handlers[resp.TypeUrl]; f {
				if len(resp.Resources) == 0 {
					// Empty response, nothing to do
					// This assumes internal types are always singleton
					break
				}
				err := h(resp.Resources[0].Resource)
				var errorResp *google_rpc.Status
				if err != nil {
					errorResp = &google_rpc.Status{
						Code:    int32(codes.Internal),
						Message: err.Error(),
					}
				}
				// Send ACK/NACK
				con.sendDeltaRequest(&discovery.DeltaDiscoveryRequest{
					TypeUrl:       resp.TypeUrl,
					ResponseNonce: resp.Nonce,
					ErrorDetail:   errorResp,
				})
				continue
			}
			switch resp.TypeUrl {
			case model.ExtensionConfigurationType:
				if features.WasmRemoteLoadConversion {
					// If Wasm remote load conversion feature is enabled, rewrite and send.
					go p.deltaRewriteAndForward(con, resp, func(resp *discovery.DeltaDiscoveryResponse) {
						// Forward the response using the thread of `handleUpstreamResponse`
						// to prevent concurrent access to forwardToEnvoy
						select {
						case forwardEnvoyCh <- resp:
						case <-con.stopChan:
						}
					})
				} else {
					// Otherwise, forward ECDS resource update directly to Envoy.
					forwardDeltaToEnvoy(con, resp)
				}
			default:
				forwardDeltaToEnvoy(con, resp)
			}
		case resp := <-forwardEnvoyCh:
			forwardDeltaToEnvoy(con, resp)
		case <-con.stopChan:
			return
		}
	}
}

func (p *XdsProxy) deltaRewriteAndForward(con *ProxyConnection, resp *discovery.DeltaDiscoveryResponse, forward func(resp *discovery.DeltaDiscoveryResponse)) {
	resources := make([]*anypb.Any, 0, len(resp.Resources))
	for i := range resp.Resources {
		resources = append(resources, resp.Resources[i].Resource)
	}

	if err := wasm.MaybeConvertWasmExtensionConfig(resources, p.wasmCache); err != nil {
		proxyLog.Debugf("sending NACK for ECDS resources %+v, err: %+v", resp.Resources, err)
		con.sendDeltaRequest(&discovery.DeltaDiscoveryRequest{
			TypeUrl:       resp.TypeUrl,
			ResponseNonce: resp.Nonce,
			ErrorDetail: &google_rpc.Status{
				Code:    int32(codes.Internal),
				Message: err.Error(),
			},
		})
		return
	}

	for i := range resources {
		resp.Resources[i].Resource = resources[i]
	}

	proxyLog.WithLabels("resources", slices.Map(resp.Resources, (*discovery.Resource).GetName), "removes", resp.RemovedResources).Debugf("forward ECDS")
	forward(resp)
}

func forwardDeltaToEnvoy(con *ProxyConnection, resp *discovery.DeltaDiscoveryResponse) {
	if !model.IsEnvoyType(resp.TypeUrl) && resp.TypeUrl != model.WorkloadType {
		proxyLog.Errorf("Skipping forwarding type url %s to Envoy as is not a valid Envoy type", resp.TypeUrl)
		return
	}
	if con.isClosed() {
		proxyLog.WithLabels("id", con.conID).Errorf("downstream dropped delta xds push to Envoy, connection already closed")
		return
	}
	if err := sendDownstreamDelta(con.downstreamDeltas, resp); err != nil {
		err = fmt.Errorf("send error for type url %s: %v", resp.TypeUrl, err)
		downstreamErr(con, err)
		return
	}
}

func sendDownstreamDelta(deltaDownstream DeltaDiscoveryStream, res *discovery.DeltaDiscoveryResponse) error {
	tStart := time.Now()
	defer func() {
		// This is a hint to help debug slow responses.
		if time.Since(tStart) > 10*time.Second {
			proxyLog.Warnf("sendDownstreamDelta took %v", time.Since(tStart))
		}
	}()
	return deltaDownstream.Send(res)
}

func (p *XdsProxy) sendDeltaHealthRequest(req *discovery.DeltaDiscoveryRequest) {
	p.connectionsMutex.Lock()
	// Immediately send if we have any connections
	for _, conn := range p.connections {
		if conn.deltaRequestsChan != nil {
			conn.deltaRequestsChan.Put(req)
		}
	}
	// Also place it as our initial request for new connections
	p.initialDeltaHealthRequest = req
	p.connectionsMutex.Unlock()
}
