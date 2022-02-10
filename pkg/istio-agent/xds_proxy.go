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

package istioagent // import "istio.io/istio/pkg/istio-agent"

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	gogotypes "github.com/gogo/protobuf/types"
	"go.uber.org/atomic"
	google_rpc "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	any "google.golang.org/protobuf/types/known/anypb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pilot/pkg/features"
	istiogrpc "istio.io/istio/pilot/pkg/grpc"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/constants"
	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/istio-agent/health"
	"istio.io/istio/pkg/istio-agent/metrics"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/uds"
	"istio.io/istio/pkg/util/gogo"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/wasm"
	"istio.io/istio/security/pkg/nodeagent/caclient"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"
)

const (
	defaultClientMaxReceiveMessageSize = math.MaxInt32
	defaultInitialConnWindowSize       = 1024 * 1024 // default gRPC InitialWindowSize
	defaultInitialWindowSize           = 1024 * 1024 // default gRPC ConnWindowSize
)

var connectionNumber = atomic.NewUint32(0)

// ResponseHandler handles a XDS response in the agent. These will not be forwarded to Envoy.
// Currently, all handlers function on a single resource per type, so the API only exposes one
// resource.
type ResponseHandler func(resp *any.Any) error

// XDS Proxy proxies all XDS requests from envoy to istiod, in addition to allowing
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
	istiodDialOptions    []grpc.DialOption
	optsMutex            sync.RWMutex
	handlers             map[string]ResponseHandler
	healthChecker        *health.WorkloadHealthChecker
	xdsHeaders           map[string]string
	xdsUdsPath           string
	proxyAddresses       []string

	httpTapServer      *http.Server
	tapMutex           sync.RWMutex
	tapResponseChannel chan *discovery.DiscoveryResponse

	// connected stores the active gRPC stream. The proxy will only have 1 connection at a time
	connected           *ProxyConnection
	initialRequest      *discovery.DiscoveryRequest
	initialDeltaRequest *discovery.DeltaDiscoveryRequest
	connectedMutex      sync.RWMutex

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

var proxyLog = log.RegisterScope("xdsproxy", "XDS Proxy in Istio Agent", 0)

const (
	localHostIPv4 = "127.0.0.1"
	localHostIPv6 = "[::1]"
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

	cache := wasm.NewLocalFileCache(constants.IstioDataDir, wasm.DefaultWasmModulePurgeInterval, wasm.DefaultWasmModuleExpiry, ia.cfg.WASMInsecureRegistries)
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
		downstreamGrpcOptions: ia.cfg.DownstreamGrpcOptions,
	}

	if ia.localDNSServer != nil {
		proxy.handlers[v3.NameTableType] = func(resp *any.Any) error {
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
		proxy.handlers[v3.ProxyConfigType] = func(resp *any.Any) error {
			var pc meshconfig.ProxyConfig
			if err := gogotypes.UnmarshalAny(gogo.ConvertAny(resp), &pc); err != nil {
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

	if err = proxy.InitIstiodDialOptions(ia); err != nil {
		return nil, err
	}

	go func() {
		if err := proxy.downstreamGrpcServer.Serve(proxy.downstreamListener); err != nil {
			log.Errorf("failed to accept downstream gRPC connection %v", err)
		}
	}()

	go proxy.healthChecker.PerformApplicationHealthCheck(func(healthEvent *health.ProbeEvent) {
		// Store the same response as Delta and SotW. Depending on how Envoy connects we will use one or the other.
		var req *discovery.DiscoveryRequest
		if healthEvent.Healthy {
			req = &discovery.DiscoveryRequest{TypeUrl: v3.HealthInfoType}
		} else {
			req = &discovery.DiscoveryRequest{
				TypeUrl: v3.HealthInfoType,
				ErrorDetail: &google_rpc.Status{
					Code:    int32(codes.Internal),
					Message: healthEvent.UnhealthyMessage,
				},
			}
		}
		proxy.PersistRequest(req)
		var deltaReq *discovery.DeltaDiscoveryRequest
		if healthEvent.Healthy {
			deltaReq = &discovery.DeltaDiscoveryRequest{TypeUrl: v3.HealthInfoType}
		} else {
			deltaReq = &discovery.DeltaDiscoveryRequest{
				TypeUrl: v3.HealthInfoType,
				ErrorDetail: &google_rpc.Status{
					Code:    int32(codes.Internal),
					Message: healthEvent.UnhealthyMessage,
				},
			}
		}
		proxy.PersistDeltaRequest(deltaReq)
	}, proxy.stopChan)

	return proxy, nil
}

// PersistRequest sends a request to the currently connected proxy. Additionally, on any reconnection
// to the upstream XDS request we will resend this request.
func (p *XdsProxy) PersistRequest(req *discovery.DiscoveryRequest) {
	var ch chan *discovery.DiscoveryRequest
	var stop chan struct{}

	p.connectedMutex.Lock()
	if p.connected != nil {
		ch = p.connected.requestsChan
		stop = p.connected.stopChan
	}
	p.initialRequest = req
	p.connectedMutex.Unlock()

	// Immediately send if we are currently connect
	if ch != nil {
		select {
		case ch <- req:
		case <-stop:
		}
	}
}

func (p *XdsProxy) UnregisterStream(c *ProxyConnection) {
	p.connectedMutex.Lock()
	defer p.connectedMutex.Unlock()
	if p.connected != nil && p.connected == c {
		close(p.connected.stopChan)
		p.connected = nil
	}
}

func (p *XdsProxy) RegisterStream(c *ProxyConnection) {
	p.connectedMutex.Lock()
	defer p.connectedMutex.Unlock()
	if p.connected != nil {
		proxyLog.Warnf("registered overlapping stream; closing previous")
		close(p.connected.stopChan)
	}
	p.connected = c
}

type ProxyConnection struct {
	conID              uint32
	upstreamError      chan error
	downstreamError    chan error
	requestsChan       chan *discovery.DiscoveryRequest
	responsesChan      chan *discovery.DiscoveryResponse
	deltaRequestsChan  chan *discovery.DeltaDiscoveryRequest
	deltaResponsesChan chan *discovery.DeltaDiscoveryResponse
	stopChan           chan struct{}
	downstream         adsStream
	upstream           discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient
	downstreamDeltas   discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer
	upstreamDeltas     discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesClient
}

// sendRequest is a small wrapper around sending to con.requestsChan. This ensures that we do not
// block forever on
func (con *ProxyConnection) sendRequest(req *discovery.DiscoveryRequest) {
	select {
	case con.requestsChan <- req:
	case <-con.stopChan:
	}
}

type adsStream interface {
	Send(*discovery.DiscoveryResponse) error
	Recv() (*discovery.DiscoveryRequest, error)
	Context() context.Context
}

// Every time envoy makes a fresh connection to the agent, we reestablish a new connection to the upstream xds
// This ensures that a new connection between istiod and agent doesn't end up consuming pending messages from envoy
// as the new connection may not go to the same istiod. Vice versa case also applies.
func (p *XdsProxy) StreamAggregatedResources(downstream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	proxyLog.Debugf("accepted XDS connection from Envoy, forwarding to upstream XDS server")
	return p.handleStream(downstream)
}

func (p *XdsProxy) handleStream(downstream adsStream) error {
	con := &ProxyConnection{
		conID:           connectionNumber.Inc(),
		upstreamError:   make(chan error, 2), // can be produced by recv and send
		downstreamError: make(chan error, 2), // can be produced by recv and send
		requestsChan:    make(chan *discovery.DiscoveryRequest, 10),
		responsesChan:   make(chan *discovery.DiscoveryResponse, 10),
		stopChan:        make(chan struct{}),
		downstream:      downstream,
	}

	p.RegisterStream(con)
	defer p.UnregisterStream(con)

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
	return p.HandleUpstream(ctx, con, xds)
}

func (p *XdsProxy) buildUpstreamConn(ctx context.Context) (*grpc.ClientConn, error) {
	p.optsMutex.RLock()
	opts := make([]grpc.DialOption, 0, len(p.istiodDialOptions))
	opts = append(opts, p.istiodDialOptions...)
	p.optsMutex.RUnlock()

	return grpc.DialContext(ctx, p.istiodAddress, opts...)
}

func (p *XdsProxy) HandleUpstream(ctx context.Context, con *ProxyConnection, xds discovery.AggregatedDiscoveryServiceClient) error {
	upstream, err := xds.StreamAggregatedResources(ctx,
		grpc.MaxCallRecvMsgSize(defaultClientMaxReceiveMessageSize))
	if err != nil {
		// Envoy logs errors again, so no need to log beyond debug level
		proxyLog.Debugf("failed to create upstream grpc client: %v", err)
		// Increase metric when xds connection error, for example: forgot to restart ingressgateway or sidecar after changing root CA.
		metrics.IstiodConnectionErrors.Increment()
		return err
	}
	proxyLog.Infof("connected to upstream XDS server: %s", p.istiodAddress)
	defer proxyLog.Debugf("disconnected from XDS server: %s", p.istiodAddress)

	con.upstream = upstream

	// Handle upstream xds recv
	go func() {
		for {
			// from istiod
			resp, err := con.upstream.Recv()
			if err != nil {
				select {
				case con.upstreamError <- err:
				case <-con.stopChan:
				}
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
			if istiogrpc.IsExpectedGRPCError(err) {
				proxyLog.Debugf("upstream [%d] terminated with status %v", con.conID, err)
				metrics.IstiodConnectionCancellations.Increment()
			} else {
				proxyLog.Warnf("upstream [%d] terminated with unexpected error %v", con.conID, err)
				metrics.IstiodConnectionErrors.Increment()
			}
			return err
		case err := <-con.downstreamError:
			// error from downstream Envoy.
			if istiogrpc.IsExpectedGRPCError(err) {
				proxyLog.Debugf("downstream [%d] terminated with status %v", con.conID, err)
				metrics.EnvoyConnectionCancellations.Increment()
			} else {
				proxyLog.Warnf("downstream [%d] terminated with unexpected error %v", con.conID, err)
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

func (p *XdsProxy) handleUpstreamRequest(con *ProxyConnection) {
	initialRequestsSent := atomic.NewBool(false)
	go func() {
		for {
			// recv xds requests from envoy
			req, err := con.downstream.Recv()
			if err != nil {
				select {
				case con.downstreamError <- err:
				case <-con.stopChan:
				}
				return
			}

			// forward to istiod
			con.sendRequest(req)
			if !initialRequestsSent.Load() && req.TypeUrl == v3.ListenerType {
				// fire off an initial NDS request
				if _, f := p.handlers[v3.NameTableType]; f {
					con.sendRequest(&discovery.DiscoveryRequest{
						TypeUrl: v3.NameTableType,
					})
				}
				// fire off an initial PCDS request
				if _, f := p.handlers[v3.ProxyConfigType]; f {
					con.sendRequest(&discovery.DiscoveryRequest{
						TypeUrl: v3.ProxyConfigType,
					})
				}
				// set flag before sending the initial request to prevent race.
				initialRequestsSent.Store(true)
				// Fire of a configured initial request, if there is one
				p.connectedMutex.RLock()
				initialRequest := p.initialRequest
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
		case req := <-con.requestsChan:
			if req.TypeUrl == v3.HealthInfoType && !initialRequestsSent.Load() {
				// only send healthcheck probe after LDS request has been sent
				continue
			}
			proxyLog.Debugf("request for type url %s", req.TypeUrl)
			metrics.XdsProxyRequests.Increment()
			if req.TypeUrl == v3.ExtensionConfigurationType {
				if req.VersionInfo != "" {
					p.ecdsLastAckVersion.Store(req.VersionInfo)
				}
				p.ecdsLastNonce.Store(req.ResponseNonce)
			}
			if err := sendUpstream(con.upstream, req); err != nil {
				proxyLog.Errorf("upstream [%d] send error for type url %s: %v", con.conID, req.TypeUrl, err)
				con.upstreamError <- err
				return
			}
		case <-con.stopChan:
			return
		}
	}
}

func (p *XdsProxy) handleUpstreamResponse(con *ProxyConnection) {
	for {
		select {
		case resp := <-con.responsesChan:
			// TODO: separate upstream response handling from requests sending, which are both time costly
			proxyLog.Debugf("response for type url %s", resp.TypeUrl)
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
			case v3.ExtensionConfigurationType:
				if features.WasmRemoteLoadConversion {
					// If Wasm remote load conversion feature is enabled, rewrite and send.
					go p.rewriteAndForward(con, resp)
				} else {
					// Otherwise, forward ECDS resource update directly to Envoy.
					forwardToEnvoy(con, resp)
				}
			default:
				if strings.HasPrefix(resp.TypeUrl, "istio.io/debug") {
					p.forwardToTap(resp)
				} else {
					forwardToEnvoy(con, resp)
				}
			}
		case <-con.stopChan:
			return
		}
	}
}

func (p *XdsProxy) rewriteAndForward(con *ProxyConnection, resp *discovery.DiscoveryResponse) {
	sendNack := wasm.MaybeConvertWasmExtensionConfig(resp.Resources, p.wasmCache)
	if sendNack {
		proxyLog.Debugf("sending NACK for ECDS resources %+v", resp.Resources)
		con.sendRequest(&discovery.DiscoveryRequest{
			VersionInfo:   p.ecdsLastAckVersion.Load(),
			TypeUrl:       v3.ExtensionConfigurationType,
			ResponseNonce: resp.Nonce,
			ErrorDetail: &google_rpc.Status{
				// TODO(bianpengyuan): make error message more informative.
				Message: "failed to fetch wasm module",
			},
		})
		return
	}
	proxyLog.Debugf("forward ECDS resources %+v", resp.Resources)
	forwardToEnvoy(con, resp)
}

func (p *XdsProxy) forwardToTap(resp *discovery.DiscoveryResponse) {
	select {
	case p.tapResponseChannel <- resp:
	default:
		log.Infof("tap response %q arrived too late; discarding", resp.TypeUrl)
	}
}

func forwardToEnvoy(con *ProxyConnection, resp *discovery.DiscoveryResponse) {
	if !v3.IsEnvoyType(resp.TypeUrl) {
		proxyLog.Errorf("Skipping forwarding type url %s to Envoy as is not a valid Envoy type", resp.TypeUrl)
		return
	}
	if err := sendDownstream(con.downstream, resp); err != nil {
		select {
		case con.downstreamError <- err:
			// we cannot return partial error and hope to restart just the downstream
			// as we are blindly proxying req/responses. For now, the best course of action
			// is to terminate upstream connection as well and restart afresh.
			proxyLog.Errorf("downstream [%d] send error: %v", con.conID, err)
		default:
			// Do not block on downstream error channel push, this could happen when forward
			// is triggered from a separated goroutine (e.g. ECDS processing go routine) while
			// downstream connection has already been teared down and no receiver is available
			// for downstream error channel.
			proxyLog.Debugf("downstream [%d] error channel full, but get downstream send error: %v", con.conID, err)
		}

		return
	}
}

func (p *XdsProxy) close() {
	close(p.stopChan)
	p.wasmCache.Cleanup()
	if p.httpTapServer != nil {
		_ = p.httpTapServer.Close()
	}
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
	opts = append(opts, istiogrpc.ServerOptions(istiokeepalive.DefaultOption())...)
	grpcs := grpc.NewServer(opts...)
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcs, p)
	reflection.Register(grpcs)
	p.downstreamGrpcServer = grpcs
	p.downstreamListener = l
	return nil
}

// getKeyCertPaths returns the paths for key and cert.
func (p *XdsProxy) getKeyCertPaths(opts *security.Options, proxyConfig *meshconfig.ProxyConfig) (string, string) {
	var key, cert string
	if opts.ProvCert != "" {
		key = path.Join(opts.ProvCert, constants.KeyFilename)
		cert = path.Join(opts.ProvCert, constants.CertChainFilename)

		// CSR may not have completed – use JWT to auth.
		if _, err := os.Stat(key); os.IsNotExist(err) {
			return "", ""
		}
		if _, err := os.Stat(cert); os.IsNotExist(err) {
			return "", ""
		}
	} else if opts.FileMountedCerts {
		key = proxyConfig.ProxyMetadata[MetadataClientCertKey]
		cert = proxyConfig.ProxyMetadata[MetadataClientCertChain]
	}
	return key, cert
}

func (p *XdsProxy) InitIstiodDialOptions(agent *Agent) error {
	opts, err := p.buildUpstreamClientDialOpts(agent)
	if err != nil {
		return err
	}

	p.optsMutex.Lock()
	p.istiodDialOptions = opts
	p.optsMutex.Unlock()
	return nil
}

func (p *XdsProxy) buildUpstreamClientDialOpts(sa *Agent) ([]grpc.DialOption, error) {
	tlsOpts, err := p.getTLSDialOption(sa)
	if err != nil {
		return nil, fmt.Errorf("failed to build TLS dial option to talk to upstream: %v", err)
	}

	keepaliveOption := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:    30 * time.Second,
		Timeout: 10 * time.Second,
	})

	initialWindowSizeOption := grpc.WithInitialWindowSize(int32(defaultInitialWindowSize))
	initialConnWindowSizeOption := grpc.WithInitialConnWindowSize(int32(defaultInitialConnWindowSize))
	msgSizeOption := grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(defaultClientMaxReceiveMessageSize))
	// Make sure the dial is blocking as we dont want any other operation to resume until the
	// connection to upstream has been made.
	dialOptions := []grpc.DialOption{
		tlsOpts,
		keepaliveOption, initialWindowSizeOption, initialConnWindowSizeOption, msgSizeOption,
	}

	dialOptions = append(dialOptions, grpc.WithPerRPCCredentials(caclient.NewXDSTokenProvider(sa.secOpts)))
	return dialOptions, nil
}

// Returns the TLS option to use when talking to Istiod
// If provisioned cert is set, it will return a mTLS related config
// Else it will return a one-way TLS related config with the assumption
// that the consumer code will use tokens to authenticate the upstream.
func (p *XdsProxy) getTLSDialOption(agent *Agent) (grpc.DialOption, error) {
	if agent.proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_NONE {
		return grpc.WithTransportCredentials(insecure.NewCredentials()), nil
	}
	rootCert, err := p.getRootCertificate(agent)
	if err != nil {
		return nil, err
	}

	config := tls.Config{
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			var certificate tls.Certificate
			key, cert := agent.GetKeyCertsForXDS()
			if key != "" && cert != "" {
				// Load the certificate from disk
				certificate, err = tls.LoadX509KeyPair(cert, key)
				if err != nil {
					return nil, err
				}
			}
			return &certificate, nil
		},
		RootCAs: rootCert,
	}

	// strip the port from the address
	parts := strings.Split(agent.proxyConfig.DiscoveryAddress, ":")
	config.ServerName = parts[0]

	// For debugging on localhost (with port forward)
	// This matches the logic for the CA; this code should eventually be shared
	if strings.Contains(config.ServerName, "localhost") {
		config.ServerName = "istiod.istio-system.svc"
	}

	if p.istiodSAN != "" {
		config.ServerName = p.istiodSAN
	}
	// TODO: if istiodSAN starts with spiffe://, use custom validation.

	config.MinVersion = tls.VersionTLS12
	transportCreds := credentials.NewTLS(&config)
	return grpc.WithTransportCredentials(transportCreds), nil
}

func (p *XdsProxy) getRootCertificate(agent *Agent) (*x509.CertPool, error) {
	var certPool *x509.CertPool
	var rootCert []byte

	xdsCACertPath, err := agent.FindRootCAForXDS()
	if err != nil {
		return nil, fmt.Errorf("failed to find root CA cert for XDS: %v", err)
	}

	if xdsCACertPath != "" {
		rootCert, err = os.ReadFile(xdsCACertPath)
		if err != nil {
			return nil, err
		}

		certPool = x509.NewCertPool()
		ok := certPool.AppendCertsFromPEM(rootCert)
		if !ok {
			return nil, fmt.Errorf("failed to create TLS dial option with root certificates")
		}
	} else {
		certPool, err = x509.SystemCertPool()
		if err != nil {
			return nil, err
		}
	}
	return certPool, nil
}

// sendUpstream sends discovery request.
func sendUpstream(upstream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient,
	request *discovery.DiscoveryRequest) error {
	return istiogrpc.Send(upstream.Context(), func() error { return upstream.Send(request) })
}

// sendDownstream sends discovery response.
func sendDownstream(downstream adsStream,
	response *discovery.DiscoveryResponse) error {
	return istiogrpc.Send(downstream.Context(), func() error { return downstream.Send(response) })
}

// tapRequest() sends "req" to Istiod, and returns a matching response, or `nil` on timeout.
// Requests are serialized -- only one may be in-flight at a time.
func (p *XdsProxy) tapRequest(req *discovery.DiscoveryRequest, timeout time.Duration) (*discovery.DiscoveryResponse, error) {
	if p.connected == nil {
		return nil, fmt.Errorf("proxy not connected to Istiod")
	}

	// Only allow one tap request at a time
	p.tapMutex.Lock()
	defer p.tapMutex.Unlock()

	// Send to Istiod
	p.connected.sendRequest(req)

	// Wait for expected response or timeout
	for {
		select {
		case res := <-p.tapResponseChannel:
			if res.TypeUrl == req.TypeUrl {
				return res, nil
			}
		case <-time.After(timeout):
			return nil, nil
		}
	}
}

func (p *XdsProxy) makeTapHandler() func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		qp, err := url.ParseQuery(req.URL.RawQuery)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "%v\n", err)
			return
		}
		typeURL := fmt.Sprintf("istio.io%s", req.URL.Path)
		dr := discovery.DiscoveryRequest{
			TypeUrl: typeURL,
		}
		resourceName := qp.Get("resourceName")
		if resourceName != "" {
			dr.ResourceNames = []string{resourceName}
		}
		response, err := p.tapRequest(&dr, 5*time.Second)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "%v\n", err)
			return
		}

		if response == nil {
			log.Infof("timed out waiting for Istiod to respond to %q", typeURL)
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}

		// Try to unmarshal Istiod's response using protojson (needed for Envoy protobufs)
		w.Header().Add("Content-Type", "application/json")
		b, err := protomarshal.MarshalIndent(response, "  ")
		if err == nil {
			_, err = w.Write(b)
			if err != nil {
				log.Infof("fail to write debug response: %v", err)
			}
			return
		}

		// Failed as protobuf.  Try as regular JSON
		proxyLog.Warnf("could not marshal istiod response as pb: %v", err)
		j, err := json.Marshal(response)
		if err != nil {
			// Couldn't unmarshal at all
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%v\n", err)
			return
		}
		_, err = w.Write(j)
		if err != nil {
			log.Infof("fail to write debug response: %v", err)
			return
		}
	}
}

// initDebugInterface() listens on localhost:${PORT} for path /debug/...
// forwards the paths to Istiod as xDS requests
// waits for response from Istiod, sends it as JSON
func (p *XdsProxy) initDebugInterface(port int) error {
	p.tapResponseChannel = make(chan *discovery.DiscoveryResponse)

	httpMux := http.NewServeMux()
	handler := p.makeTapHandler()
	httpMux.HandleFunc("/debug/", handler)
	httpMux.HandleFunc("/debug", handler) // For 1.10 Istiod which uses istio.io/debug

	p.httpTapServer = &http.Server{
		Addr:        fmt.Sprintf("localhost:%d", port),
		Handler:     httpMux,
		IdleTimeout: 90 * time.Second, // matches http.DefaultTransport keep-alive timeout
		ReadTimeout: 30 * time.Second,
	}

	// create HTTP listener
	listener, err := net.Listen("tcp", p.httpTapServer.Addr)
	if err != nil {
		return err
	}

	go func() {
		log.Infof("starting Http service at %s", listener.Addr())
		if err := p.httpTapServer.Serve(listener); err != nil {
			log.Errorf("error serving tap http server: %v", err)
		}
	}()

	return nil
}
