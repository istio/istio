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
	"math"
	"net"
	"sync"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"go.uber.org/atomic"
	google_rpc "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	anypb "google.golang.org/protobuf/types/known/anypb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pilot/pkg/features"
	istiogrpc "istio.io/istio/pilot/pkg/grpc"
	"istio.io/istio/pkg/channels"
	"istio.io/istio/pkg/config/constants"
	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/istio-agent/health"
	"istio.io/istio/pkg/istio-agent/metrics"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/uds"
	"istio.io/istio/pkg/wasm"
	xdspkg "istio.io/istio/pkg/xds"
	"istio.io/istio/security/pkg/nodeagent/caclient"
	"istio.io/istio/security/pkg/pki/util"
)

const (
	defaultClientMaxReceiveMessageSize = math.MaxInt32
)

type (
	DiscoveryStream      = discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer
	DeltaDiscoveryStream = discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer
	DiscoveryClient      = discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient
	DeltaDiscoveryClient = discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesClient
)

var connectionNumber = atomic.NewUint32(0)

// ResponseHandler handles a XDS response in the agent. These will not be forwarded to Envoy.
// Currently, all handlers function on a single resource per type, so the API only exposes one
// resource.
type ResponseHandler func(resp *anypb.Any) error

// XdsProxy proxies all XDS requests from envoy to istiod, in addition to allowing
// subsystems inside the agent to also communicate with either istiod/envoy (eg dns, sds, etc).
// The goal here is to consolidate all xds related connections to istiod/envoy into a
// single tcp connection with multiple gRPC streams.
// TODO: Right now, the workloadSDS server and gatewaySDS servers are still separate
// connections. These need to be consolidated.
// TODO: consolidate/use ADSC struct - a lot of duplication.
type XdsProxy struct {
	stopChan             chan struct{}
	clusterID            string
	downstreamListener   net.Listener
	downstreamGrpcServer *grpc.Server
	istiodAddress        string
	optsMutex            sync.RWMutex
	dialOptions          []grpc.DialOption
	handlers             map[string]ResponseHandler
	healthChecker        *health.WorkloadHealthChecker
	xdsHeaders           map[string]string
	xdsUdsPath           string
	proxyAddresses       []string
	ia                   *Agent

	// connected stores the active gRPC stream. The proxy will only have 1 connection at a time
	connected                 *ProxyConnection
	initialHealthRequest      *discovery.DiscoveryRequest
	initialDeltaHealthRequest *discovery.DeltaDiscoveryRequest
	connectedMutex            sync.RWMutex

	// Wasm cache and ecds channel are used to replace wasm remote load with local file.
	wasmCache wasm.Cache

	// ecds version and nonce uses atomic only to prevent race in testing.
	// In reality there should not be race as istiod will only have one
	// in flight update for each type of resource.
	// TODO(bianpengyuan): this relies on the fact that istiod versions all ECDS resources
	// the same in a update response. This needs update to support per resource versioning,
	// in case istiod changes its behavior, or a different ECDS server is used.
	ecdsLastAckVersion    atomic.String
	ecdsLastNonce         atomic.String
	downstreamGrpcOptions []grpc.ServerOption
	istiodSAN             string
}

var proxyLog = log.RegisterScope("xdsproxy", "XDS Proxy in Istio Agent")

const (
	localHostIPv4 = "127.0.0.1"
	localHostIPv6 = "::1"
)

func initXdsProxy(ia *Agent) (*XdsProxy, error) {
	var err error
	localHostAddr := localHostIPv4
	if ia.cfg.IsIPv6 {
		localHostAddr = localHostIPv6
	}
	var envoyProbe ready.Prober
	if !ia.cfg.DisableEnvoy {
		envoyProbe = &ready.Probe{
			AdminPort:     uint16(ia.proxyConfig.ProxyAdminPort),
			LocalHostAddr: localHostAddr,
		}
	}

	cache := wasm.NewLocalFileCache(constants.IstioDataDir, ia.cfg.WASMOptions)
	proxy := &XdsProxy{
		istiodAddress:         ia.proxyConfig.DiscoveryAddress,
		istiodSAN:             ia.cfg.IstiodSAN,
		clusterID:             ia.secOpts.ClusterID,
		handlers:              map[string]ResponseHandler{},
		stopChan:              make(chan struct{}),
		healthChecker:         health.NewWorkloadHealthChecker(ia.proxyConfig.ReadinessProbe, envoyProbe, ia.cfg.ProxyIPAddresses, ia.cfg.IsIPv6),
		xdsHeaders:            ia.cfg.XDSHeaders,
		xdsUdsPath:            ia.cfg.XdsUdsPath,
		wasmCache:             cache,
		proxyAddresses:        ia.cfg.ProxyIPAddresses,
		ia:                    ia,
		downstreamGrpcOptions: ia.cfg.DownstreamGrpcOptions,
	}

	if ia.localDNSServer != nil {
		proxy.handlers[model.NameTableType] = func(resp *anypb.Any) error {
			var nt dnsProto.NameTable
			if err := resp.UnmarshalTo(&nt); err != nil {
				log.Errorf("failed to unmarshal name table: %v", err)
				return err
			}
			ia.localDNSServer.UpdateLookupTable(&nt)
			return nil
		}
	}
	if ia.cfg.EnableDynamicProxyConfig && ia.secretCache != nil {
		proxy.handlers[model.ProxyConfigType] = func(resp *anypb.Any) error {
			pc := &meshconfig.ProxyConfig{}
			if err := resp.UnmarshalTo(pc); err != nil {
				log.Errorf("failed to unmarshal proxy config: %v", err)
				return err
			}
			caCerts := pc.GetCaCertificatesPem()
			log.Debugf("received new certificates to add to mesh trust domain: %v", caCerts)
			trustBundle := []byte{}
			for _, cert := range caCerts {
				trustBundle = util.AppendCertByte(trustBundle, []byte(cert))
			}
			return ia.secretCache.UpdateConfigTrustBundle(trustBundle)
		}
	}

	proxyLog.Infof("Initializing with upstream address %q and cluster %q", proxy.istiodAddress, proxy.clusterID)

	if err = proxy.initDownstreamServer(); err != nil {
		return nil, err
	}

	if err = proxy.initIstiodDialOptions(ia); err != nil {
		return nil, err
	}

	go func() {
		if err := proxy.downstreamGrpcServer.Serve(proxy.downstreamListener); err != nil {
			log.Errorf("failed to accept downstream gRPC connection %v", err)
		}
	}()

	go proxy.healthChecker.PerformApplicationHealthCheck(func(healthEvent *health.ProbeEvent) {
		// Store the same response as Delta and SotW. Depending on how Envoy connects we will use one or the other.
		req := &discovery.DiscoveryRequest{TypeUrl: model.HealthInfoType}
		if !healthEvent.Healthy {
			req.ErrorDetail = &google_rpc.Status{
				Code:    int32(codes.Internal),
				Message: healthEvent.UnhealthyMessage,
			}
		}
		proxy.sendHealthCheckRequest(req)
		deltaReq := &discovery.DeltaDiscoveryRequest{TypeUrl: model.HealthInfoType}
		if !healthEvent.Healthy {
			deltaReq.ErrorDetail = &google_rpc.Status{
				Code:    int32(codes.Internal),
				Message: healthEvent.UnhealthyMessage,
			}
		}
		proxy.sendDeltaHealthRequest(deltaReq)
	}, proxy.stopChan)

	return proxy, nil
}

// sendHealthCheckRequest sends a request to the currently connected proxy. Additionally, on any reconnection
// to the upstream XDS request we will resend this request.
func (p *XdsProxy) sendHealthCheckRequest(req *discovery.DiscoveryRequest) {
	p.connectedMutex.Lock()
	// Immediately send if we are currently connected.
	if p.connected != nil && p.connected.requestsChan != nil {
		p.connected.requestsChan.Put(req)
	}
	// Otherwise place it as our initial request for new connections
	p.initialHealthRequest = req
	p.connectedMutex.Unlock()
}

func (p *XdsProxy) unregisterStream(c *ProxyConnection) {
	p.connectedMutex.Lock()
	defer p.connectedMutex.Unlock()
	if p.connected != nil && p.connected == c {
		close(p.connected.stopChan)
		p.connected = nil
	}
}

func (p *XdsProxy) registerStream(c *ProxyConnection) {
	p.connectedMutex.Lock()
	defer p.connectedMutex.Unlock()
	if p.connected != nil {
		proxyLog.Warnf("registered overlapping stream; closing previous")
		close(p.connected.stopChan)
	}
	p.connected = c
}

// ProxyConnection represents connection to downstream proxy.
type ProxyConnection struct {
	conID              uint32
	upstreamError      chan error
	downstreamError    chan error
	requestsChan       *channels.Unbounded[*discovery.DiscoveryRequest]
	responsesChan      chan *discovery.DiscoveryResponse
	deltaRequestsChan  *channels.Unbounded[*discovery.DeltaDiscoveryRequest]
	deltaResponsesChan chan *discovery.DeltaDiscoveryResponse
	stopChan           chan struct{}
	downstream         adsStream
	upstream           DiscoveryClient
	downstreamDeltas   DeltaDiscoveryStream
	upstreamDeltas     DeltaDiscoveryClient
}

// sendRequest is a small wrapper around sending to con.requestsChan. This ensures that we do not
// block forever on
func (con *ProxyConnection) sendRequest(req *discovery.DiscoveryRequest) {
	con.requestsChan.Put(req)
}

func (con *ProxyConnection) isClosed() bool {
	select {
	case <-con.stopChan:
		return true
	default:
		return false
	}
}

type adsStream interface {
	Send(*discovery.DiscoveryResponse) error
	Recv() (*discovery.DiscoveryRequest, error)
	Context() context.Context
}

// StreamAggregatedResources is an implementation of XDS API used for proxying between Istiod and Envoy.
// Every time envoy makes a fresh connection to the agent, we reestablish a new connection to the upstream xds
// This ensures that a new connection between istiod and agent doesn't end up consuming pending messages from envoy
// as the new connection may not go to the same istiod. Vice versa case also applies.
func (p *XdsProxy) StreamAggregatedResources(downstream DiscoveryStream) error {
	proxyLog.Debugf("accepted XDS connection from Envoy, forwarding to upstream XDS server")
	return p.handleStream(downstream)
}

func (p *XdsProxy) handleStream(downstream adsStream) error {
	con := &ProxyConnection{
		conID:           connectionNumber.Inc(),
		upstreamError:   make(chan error), // can be produced by recv and send
		downstreamError: make(chan error), // can be produced by recv and send
		// Requests channel is unbounded. The Envoy<->XDS Proxy<->Istiod system produces a natural
		// looping of Recv and Send. Due to backpressure introduced by gRPC natively (that is, Send() can
		// only send so much data without being Recv'd before it starts blocking), along with the
		// backpressure provided by our channels, we have a risk of deadlock where both Xdsproxy and
		// Istiod are trying to Send, but both are blocked by gRPC backpressure until Recv() is called.
		// However, Recv can fail to be called by Send being blocked. This can be triggered by the two
		// sources in our system (Envoy request and Istiod pushes) producing more events than we can keep
		// up with.
		// See https://github.com/istio/istio/issues/39209 for more information
		//
		// To prevent these issues, we need to either:
		// 1. Apply backpressure directly to Envoy requests or Istiod pushes
		// 2. Make part of the system unbounded
		//
		// (1) is challenging because we cannot do a conditional Recv (for Envoy requests), and changing
		// the control plane requires substantial changes. Instead, we make the requests channel
		// unbounded. This is the least likely to cause issues as the messages we store here are the
		// smallest relative to other channels.
		requestsChan: channels.NewUnbounded[*discovery.DiscoveryRequest](),
		// Allow a buffer of 1. This ensures we queue up at most 2 (one in process, 1 pending) responses before forwarding.
		responsesChan: make(chan *discovery.DiscoveryResponse, 1),
		stopChan:      make(chan struct{}),
		downstream:    downstream,
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
	return p.handleUpstream(ctx, con, xds)
}

func (p *XdsProxy) buildUpstreamConn(ctx context.Context) (*grpc.ClientConn, error) {
	p.optsMutex.RLock()
	opts := p.dialOptions
	p.optsMutex.RUnlock()
	return grpc.DialContext(ctx, p.istiodAddress, opts...)
}

func (p *XdsProxy) handleUpstream(ctx context.Context, con *ProxyConnection, xds discovery.AggregatedDiscoveryServiceClient) error {
	log := proxyLog.WithLabels("id", con.conID)
	upstream, err := xds.StreamAggregatedResources(ctx,
		grpc.MaxCallRecvMsgSize(defaultClientMaxReceiveMessageSize))
	if err != nil {
		// Envoy logs errors again, so no need to log beyond debug level
		log.Debugf("failed to create upstream grpc client: %v", err)
		// Increase metric when xds connection error, for example: forgot to restart ingressgateway or sidecar after changing root CA.
		metrics.IstiodConnectionErrors.Increment()
		return err
	}
	log.Infof("connected to upstream XDS server: %s", p.istiodAddress)
	defer log.Debugf("disconnected from XDS server: %s", p.istiodAddress)

	con.upstream = upstream

	// Handle upstream xds recv
	go func() {
		for {
			// from istiod
			resp, err := con.upstream.Recv()
			if err != nil {
				upstreamErr(con, err)
				return
			}
			select {
			case con.responsesChan <- resp:
			case <-con.stopChan:
			}
		}
	}()

	go p.handleUpstreamRequest(con)
	go p.handleUpstreamResponse(con)

	for {
		select {
		case err := <-con.upstreamError:
			// error from upstream Istiod.
			return err
		case err := <-con.downstreamError:
			// error from downstream Envoy.
			// On downstream error, we will return. This propagates the error to downstream envoy which will trigger reconnect
			return err
		case <-con.stopChan:
			log.Debugf("upstream stopped")
			return nil
		}
	}
}

func (p *XdsProxy) handleUpstreamRequest(con *ProxyConnection) {
	initialRequestsSent := atomic.NewBool(false)
	go func() {
		for {
			// recv xds requests from envoy
			req, err := con.downstream.Recv()
			if err != nil {
				downstreamErr(con, err)
				return
			}

			// forward to istiod
			con.sendRequest(req)
			if !initialRequestsSent.Load() && req.TypeUrl == model.ListenerType {
				// fire off an initial NDS request
				if _, f := p.handlers[model.NameTableType]; f {
					con.sendRequest(&discovery.DiscoveryRequest{
						TypeUrl: model.NameTableType,
					})
				}
				// fire off an initial PCDS request
				if _, f := p.handlers[model.ProxyConfigType]; f {
					con.sendRequest(&discovery.DiscoveryRequest{
						TypeUrl: model.ProxyConfigType,
					})
				}
				// set flag before sending the initial request to prevent race.
				initialRequestsSent.Store(true)
				// Fire of a configured initial request, if there is one
				p.connectedMutex.RLock()
				initialRequest := p.initialHealthRequest
				if initialRequest != nil {
					con.sendRequest(initialRequest)
				}
				p.connectedMutex.RUnlock()
			}
		}
	}()

	defer con.upstream.CloseSend() // nolint
	for {
		select {
		case req := <-con.requestsChan.Get():
			con.requestsChan.Load()
			if req.TypeUrl == model.HealthInfoType && !initialRequestsSent.Load() {
				// only send healthcheck probe after LDS request has been sent
				continue
			}
			proxyLog.Debugf("request for type url %s", req.TypeUrl)
			metrics.XdsProxyRequests.Increment()
			if req.TypeUrl == model.ExtensionConfigurationType {
				if req.VersionInfo != "" {
					p.ecdsLastAckVersion.Store(req.VersionInfo)
				}
				p.ecdsLastNonce.Store(req.ResponseNonce)
			}
			if err := con.upstream.Send(req); err != nil {
				err = fmt.Errorf("send error for type url %s: %v", req.TypeUrl, err)
				upstreamErr(con, err)
				return
			}
		case <-con.stopChan:
			return
		}
	}
}

func (p *XdsProxy) handleUpstreamResponse(con *ProxyConnection) {
	forwardEnvoyCh := make(chan *discovery.DiscoveryResponse, 1)
	for {
		select {
		case resp := <-con.responsesChan:
			// TODO: separate upstream response handling from requests sending, which are both time costly
			proxyLog.WithLabels(
				"id", con.conID,
				"type", model.GetShortType(resp.TypeUrl),
				"resources", len(resp.Resources),
			).Debugf("upstream response")
			metrics.XdsProxyResponses.Increment()
			if h, f := p.handlers[resp.TypeUrl]; f {
				if len(resp.Resources) == 0 {
					// Empty response, nothing to do
					// This assumes internal types are always singleton
					break
				}
				err := h(resp.Resources[0])
				var errorResp *google_rpc.Status
				if err != nil {
					errorResp = &google_rpc.Status{
						Code:    int32(codes.Internal),
						Message: err.Error(),
					}
				}
				// Send ACK/NACK
				con.sendRequest(&discovery.DiscoveryRequest{
					VersionInfo:   resp.VersionInfo,
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
					go p.rewriteAndForward(con, resp, func(resp *discovery.DiscoveryResponse) {
						// Forward the response using the thread of `handleUpstreamResponse`
						// to prevent concurrent access to forwardToEnvoy
						select {
						case forwardEnvoyCh <- resp:
						case <-con.stopChan:
						}
					})
				} else {
					// Otherwise, forward ECDS resource update directly to Envoy.
					forwardToEnvoy(con, resp)
				}
			default:
				forwardToEnvoy(con, resp)
			}
		case resp := <-forwardEnvoyCh:
			forwardToEnvoy(con, resp)
		case <-con.stopChan:
			return
		}
	}
}

func (p *XdsProxy) rewriteAndForward(con *ProxyConnection, resp *discovery.DiscoveryResponse, forward func(resp *discovery.DiscoveryResponse)) {
	if err := wasm.MaybeConvertWasmExtensionConfig(resp.Resources, p.wasmCache); err != nil {
		proxyLog.Debugf("sending NACK for ECDS resources %+v, err: %+v", resp.Resources, err)
		con.sendRequest(&discovery.DiscoveryRequest{
			VersionInfo:   p.ecdsLastAckVersion.Load(),
			TypeUrl:       resp.TypeUrl,
			ResponseNonce: resp.Nonce,
			ErrorDetail: &google_rpc.Status{
				Code:    int32(codes.Internal),
				Message: err.Error(),
			},
		})
		return
	}
	proxyLog.Debugf("forward ECDS resources %+v", resp.Resources)
	forward(resp)
}

func forwardToEnvoy(con *ProxyConnection, resp *discovery.DiscoveryResponse) {
	if !model.IsEnvoyType(resp.TypeUrl) && resp.TypeUrl != model.WorkloadType {
		proxyLog.Errorf("Skipping forwarding type url %s to Envoy as is not a valid Envoy type", resp.TypeUrl)
		return
	}
	if con.isClosed() {
		proxyLog.WithLabels("id", con.conID).Errorf("downstream dropped xds push to Envoy, connection already closed")
		return
	}
	if err := sendDownstream(con.downstream, resp); err != nil {
		err = fmt.Errorf("send error for type url %s: %v", resp.TypeUrl, err)
		downstreamErr(con, err)
		return
	}
}

// sendDownstream sends discovery response.
func sendDownstream(downstream adsStream, response *discovery.DiscoveryResponse) error {
	tStart := time.Now()
	defer func() {
		// This is a hint to help debug slow responses.
		if time.Since(tStart) > 10*time.Second {
			proxyLog.Warnf("sendDownstream took %v", time.Since(tStart))
		}
	}()
	return downstream.Send(response)
}

func (p *XdsProxy) close() {
	close(p.stopChan)
	p.wasmCache.Cleanup()
	if p.downstreamGrpcServer != nil {
		p.downstreamGrpcServer.Stop()
	}
	if p.downstreamListener != nil {
		_ = p.downstreamListener.Close()
	}
}

func (p *XdsProxy) initDownstreamServer() error {
	l, err := uds.NewListener(p.xdsUdsPath)
	if err != nil {
		return err
	}
	// TODO: Expose keepalive options to agent cmd line flags.
	opts := p.downstreamGrpcOptions
	opts = append(opts, istiogrpc.ServerOptions(istiokeepalive.DefaultOption(), xdspkg.RecordRecvSize)...)
	grpcs := grpc.NewServer(opts...)
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcs, p)
	reflection.Register(grpcs)
	p.downstreamGrpcServer = grpcs
	p.downstreamListener = l
	return nil
}

func (p *XdsProxy) initIstiodDialOptions(agent *Agent) error {
	opts, err := p.buildUpstreamClientDialOpts(agent)
	if err != nil {
		return err
	}

	p.optsMutex.Lock()
	p.dialOptions = opts
	p.optsMutex.Unlock()
	return nil
}

func (p *XdsProxy) buildUpstreamClientDialOpts(sa *Agent) ([]grpc.DialOption, error) {
	tlsOpts, err := p.getTLSOptions(sa)
	if err != nil {
		return nil, fmt.Errorf("failed to get TLS options to talk to upstream: %v", err)
	}
	options, err := istiogrpc.ClientOptions(nil, tlsOpts)
	if err != nil {
		return nil, err
	}
	if sa.secOpts.CredFetcher != nil {
		options = append(options, grpc.WithPerRPCCredentials(caclient.NewDefaultTokenProvider(sa.secOpts)))
	}
	return options, nil
}

// Returns the TLS option to use when talking to Istiod
func (p *XdsProxy) getTLSOptions(agent *Agent) (*istiogrpc.TLSOptions, error) {
	if agent.proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_NONE {
		return nil, nil
	}
	xdsCACertPath, err := agent.FindRootCAForXDS()
	if err != nil {
		return nil, fmt.Errorf("failed to find root CA cert for XDS: %v", err)
	}
	key, cert := agent.GetKeyCertsForXDS()
	return &istiogrpc.TLSOptions{
		RootCert:      xdsCACertPath,
		Key:           key,
		Cert:          cert,
		ServerAddress: agent.proxyConfig.DiscoveryAddress,
		SAN:           p.istiodSAN,
	}, nil
}

// upstreamErr sends the error to upstreamError channel, and return immediately if the connection closed.
func upstreamErr(con *ProxyConnection, err error) {
	switch istiogrpc.GRPCErrorType(err) {
	case istiogrpc.GracefulTermination:
		err = nil
		fallthrough
	case istiogrpc.ExpectedError:
		proxyLog.WithLabels("id", con.conID).Debugf("upstream terminated with status %v", err)
		metrics.IstiodConnectionCancellations.Increment()
	default:
		proxyLog.WithLabels("id", con.conID).Warnf("upstream terminated with unexpected error %v", err)
		metrics.IstiodConnectionErrors.Increment()
	}
	select {
	case con.upstreamError <- err:
	case <-con.stopChan:
	}
}

// downstreamErr sends the error to downstreamError channel, and return immediately if the connection closed.
func downstreamErr(con *ProxyConnection, err error) {
	switch istiogrpc.GRPCErrorType(err) {
	case istiogrpc.GracefulTermination:
		err = nil
		fallthrough
	case istiogrpc.ExpectedError:
		proxyLog.WithLabels("id", con.conID).Debugf("downstream terminated with status %v", err)
		metrics.EnvoyConnectionCancellations.Increment()
	default:
		proxyLog.WithLabels("id", con.conID).Warnf("downstream terminated with unexpected error %v", err)
		metrics.EnvoyConnectionErrors.Increment()
	}
	select {
	case con.downstreamError <- err:
	case <-con.stopChan:
	}
}
