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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/ptypes"
	"go.uber.org/atomic"
	"golang.org/x/oauth2"
	google_rpc "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pilot/pkg/dns"
	nds "istio.io/istio/pilot/pkg/proto"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/istio-agent/health"
	"istio.io/istio/pkg/istio-agent/metrics"
	"istio.io/istio/pkg/mcp/status"
	"istio.io/istio/pkg/uds"
	"istio.io/istio/pkg/wasm"
	"istio.io/istio/security/pkg/nodeagent/caclient"
	"istio.io/pkg/log"
)

const (
	defaultClientMaxReceiveMessageSize = math.MaxInt32
	defaultInitialConnWindowSize       = 1024 * 1024     // default gRPC InitialWindowSize
	defaultInitialWindowSize           = 1024 * 1024     // default gRPC ConnWindowSize
	sendTimeout                        = 5 * time.Second // default upstream send timeout.
)

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
	localDNSServer       *dns.LocalDNSServer
	healthChecker        *health.WorkloadHealthChecker
	xdsHeaders           map[string]string
	xdsUdsPath           string

	// connected stores the active gRPC stream. The proxy will only have 1 connection at a time
	connected      *ProxyConnection
	initialRequest *discovery.DiscoveryRequest
	connectedMutex sync.RWMutex

	// Wasm cache and ecds channel are used to repalce wasm remote load with local file.
	wasmCache      wasm.Cache
	ecdsUpdateChan chan *discovery.DiscoveryResponse
	// ecds version and nonce uses atomic only to prevent race in testing.
	// In reality there should not be race as istiod will only have one
	// in flight update for each type of resource.
	ecdsLastAckVersion atomic.String
	ecdsLastNonce      atomic.String
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
	envoyProbe := &ready.Probe{
		AdminPort:     uint16(ia.proxyConfig.ProxyAdminPort),
		LocalHostAddr: localHostAddr,
	}
	proxy := &XdsProxy{
		istiodAddress:  ia.proxyConfig.DiscoveryAddress,
		clusterID:      ia.secOpts.ClusterID,
		localDNSServer: ia.localDNSServer,
		stopChan:       make(chan struct{}),
		healthChecker:  health.NewWorkloadHealthChecker(ia.proxyConfig.ReadinessProbe, envoyProbe),
		xdsHeaders:     ia.cfg.XDSHeaders,
		xdsUdsPath:     ia.cfg.XdsUdsPath,
		wasmCache:      wasm.NewLocalFileCache(constants.ConfigPathDir, wasm.DefaultWasmModulePurgeInteval, wasm.DefaultWasmModuleExpiry),
		ecdsUpdateChan: make(chan *discovery.DiscoveryResponse, 10),
	}

	proxyLog.Infof("Initializing with upstream address %q and cluster %q", proxy.istiodAddress, proxy.clusterID)

	if err = proxy.initDownstreamServer(); err != nil {
		return nil, err
	}

	if proxy.istiodDialOptions, err = proxy.buildUpstreamClientDialOpts(ia); err != nil {
		return nil, err
	}

	go func() {
		if err := proxy.downstreamGrpcServer.Serve(proxy.downstreamListener); err != nil {
			log.Errorf("failed to accept downstream gRPC connection %v", err)
		}
	}()

	go proxy.healthChecker.PerformApplicationHealthCheck(func(healthEvent *health.ProbeEvent) {
		var req *discovery.DiscoveryRequest
		if healthEvent.Healthy {
			req = &discovery.DiscoveryRequest{TypeUrl: v3.HealthInfoType}
		} else {
			req = &discovery.DiscoveryRequest{
				TypeUrl: v3.HealthInfoType,
				ErrorDetail: &google_rpc.Status{
					Code:    500,
					Message: healthEvent.UnhealthyMessage,
				},
			}
		}
		proxy.PersistRequest(req)
	}, proxy.stopChan)
	return proxy, nil
}

// PersistRequest sends a request to the currently connected proxy. Additionally, on any reconnection
// to the upstream XDS request we will resend this request.
func (p *XdsProxy) PersistRequest(req *discovery.DiscoveryRequest) {
	var ch chan *discovery.DiscoveryRequest

	p.connectedMutex.Lock()
	if p.connected != nil {
		ch = p.connected.requestsChan
	}
	p.initialRequest = req
	p.connectedMutex.Unlock()

	// Immediately send if we are currently connect
	if ch != nil {
		ch <- req
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
	upstreamError   chan error
	downstreamError chan error
	requestsChan    chan *discovery.DiscoveryRequest
	responsesChan   chan *discovery.DiscoveryResponse
	stopChan        chan struct{}
	downstream      discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer
	upstream        discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient
}

// Every time envoy makes a fresh connection to the agent, we reestablish a new connection to the upstream xds
// This ensures that a new connection between istiod and agent doesn't end up consuming pending messages from envoy
// as the new connection may not go to the same istiod. Vice versa case also applies.
func (p *XdsProxy) StreamAggregatedResources(downstream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	proxyLog.Debugf("accepted XDS connection from Envoy, forwarding to upstream XDS server")

	con := &ProxyConnection{
		upstreamError:   make(chan error, 2), // can be produced by recv and send
		downstreamError: make(chan error, 2), // can be produced by recv and send
		requestsChan:    make(chan *discovery.DiscoveryRequest, 10),
		responsesChan:   make(chan *discovery.DiscoveryResponse, 10),
		stopChan:        make(chan struct{}),
		downstream:      downstream,
	}

	p.RegisterStream(con)
	defer p.UnregisterStream(con)

	// Handle downstream xds
	initialRequestsSent := false
	go func() {
		// Send initial request
		p.connectedMutex.RLock()
		initialRequest := p.initialRequest
		p.connectedMutex.RUnlock()

		for {
			// From Envoy
			req, err := downstream.Recv()
			if err != nil {
				con.downstreamError <- err
				return
			}
			// forward to istiod
			con.requestsChan <- req
			if !initialRequestsSent && req.TypeUrl == v3.ListenerType {
				// fire off an initial NDS request
				if p.localDNSServer != nil {
					con.requestsChan <- &discovery.DiscoveryRequest{
						TypeUrl: v3.NameTableType,
					}
				}
				// Fire of a configured initial request, if there is one
				if initialRequest != nil {
					con.requestsChan <- initialRequest
				}
				initialRequestsSent = true
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	upstreamConn, err := grpc.DialContext(ctx, p.istiodAddress, p.istiodDialOptions...)
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

func (p *XdsProxy) HandleUpstream(ctx context.Context, con *ProxyConnection, xds discovery.AggregatedDiscoveryServiceClient) error {
	upstream, err := xds.StreamAggregatedResources(ctx,
		grpc.MaxCallRecvMsgSize(defaultClientMaxReceiveMessageSize))
	if err != nil {
		// Envoy logs errors again, so no need to log beyond debug level
		proxyLog.Debugf("failed to create upstream grpc client: %v", err)
		return err
	}
	proxyLog.Infof("connected to upstream XDS server: %s", p.istiodAddress)
	defer proxyLog.Debugf("disconnected from XDS server: %s", p.istiodAddress)

	con.upstream = upstream

	// Handle upstream xds recv
	go func() {
		for {
			// from istiod
			resp, err := upstream.Recv()
			if err != nil {
				con.upstreamError <- err
				return
			}
			con.responsesChan <- resp
		}
	}()

	go p.handleUpstreamRequest(ctx, con)
	go p.handleUpstreamResponse(con)

	// Handle ECDS update asynchronously, it might need to download Wasm modules.
	go p.handleUpstreamECDSResponse(con)

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

func (p *XdsProxy) handleUpstreamRequest(ctx context.Context, con *ProxyConnection) {
	defer con.upstream.CloseSend() // nolint
	for {
		select {
		case req := <-con.requestsChan:
			proxyLog.Debugf("request for type url %s", req.TypeUrl)
			metrics.XdsProxyRequests.Increment()
			if req.TypeUrl == v3.ExtensionConfigurationType {
				if req.VersionInfo != "" {
					p.ecdsLastAckVersion.Store(req.VersionInfo)
				}
				p.ecdsLastNonce.Store(req.ResponseNonce)
			}
			if err := sendUpstreamWithTimeout(ctx, con.upstream, req); err != nil {
				proxyLog.Errorf("upstream send error for type url %s: %v", req.TypeUrl, err)
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
			switch resp.TypeUrl {
			case v3.NameTableType:
				// intercept. This is for the dns server
				if p.localDNSServer != nil && len(resp.Resources) > 0 {
					var nt nds.NameTable
					// TODO we should probably send ACK and not update nametable here
					if err := ptypes.UnmarshalAny(resp.Resources[0], &nt); err != nil {
						log.Errorf("failed to unmarshall name table: %v", err)
					}
					p.localDNSServer.UpdateLookupTable(&nt)
				}

				// Send ACK
				con.requestsChan <- &discovery.DiscoveryRequest{
					VersionInfo:   resp.VersionInfo,
					TypeUrl:       v3.NameTableType,
					ResponseNonce: resp.Nonce,
				}
			case v3.ExtensionConfigurationType:
				p.ecdsUpdateChan <- resp
			default:
				forwardToEnvoy(con, resp)
			}
		case <-con.stopChan:
			return
		}
	}
}

func (p *XdsProxy) handleUpstreamECDSResponse(con *ProxyConnection) {
	// TODO(bianpengyuan): Add metrics about ecds handling.
	for {
		select {
		case resp := <-p.ecdsUpdateChan:
			sendNack := wasm.MaybeConvertWasmExtensionConfig(resp.Resources, p.wasmCache)
			if sendNack {
				con.requestsChan <- &discovery.DiscoveryRequest{
					VersionInfo:   p.ecdsLastAckVersion.Load(),
					TypeUrl:       v3.ExtensionConfigurationType,
					ResponseNonce: resp.Nonce,
					ErrorDetail: &google_rpc.Status{
						// TODO(bianpengyuan): make error message more informative.
						Message: "failed to fetch wasm module",
					},
				}
				return
			}
			proxyLog.Debugf("forward ECDS resources %+v", resp.Resources)
			forwardToEnvoy(con, resp)

		case <-con.stopChan:
			return
		}
	}
}

func forwardToEnvoy(con *ProxyConnection, resp *discovery.DiscoveryResponse) {
	// TODO: Validate the known type urls before forwarding them to Envoy.
	if err := sendDownstreamWithTimeout(con.downstream, resp); err != nil {
		select {
		case con.downstreamError <- err:
			// we cannot return partial error and hope to restart just the downstream
			// as we are blindly proxying req/responses. For now, the best course of action
			// is to terminate upstream connection as well and restart afresh.
			proxyLog.Errorf("downstream send error: %v", err)
		default:
			// Do not block on downstream error channel push, this could happen when forward
			// is triggered from a separated goroutine (e.g. ECDS processing go routine) while
			// downstream connection has already been teared down and no receiver is available
			// for downstream error channel.
			proxyLog.Debugf("downstream error channel full, but get downstream send error: %v", err)
		}

		return
	}
}

func (p *XdsProxy) DeltaAggregatedResources(server discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return errors.New("delta XDS is not implemented")
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

// isExpectedGRPCError checks a gRPC error code and determines whether it is an expected error when
// things are operating normally. This is basically capturing when the client disconnects.
func isExpectedGRPCError(err error) bool {
	if err == io.EOF {
		return true
	}

	s := status.Convert(err)
	if s.Code() == codes.Canceled || s.Code() == codes.DeadlineExceeded {
		return true
	}
	if s.Code() == codes.Unavailable && (s.Message() == "client disconnected" || s.Message() == "transport is closing") {
		return true
	}
	return false
}

type fileTokenSource struct {
	path string
}

var _ = oauth2.TokenSource(&fileTokenSource{})

func (ts *fileTokenSource) Token() (*oauth2.Token, error) {
	tokb, err := ioutil.ReadFile(ts.path)
	if err != nil {
		proxyLog.Errorf("failed to read token file %q: %v", ts.path, err)
		return nil, fmt.Errorf("failed to read token file %q: %v", ts.path, err)
	}
	tok := strings.TrimSpace(string(tokb))
	if len(tok) == 0 {
		proxyLog.Errorf("read empty token from file %q", ts.path)
		return nil, fmt.Errorf("read empty token from file %q", ts.path)
	}

	return &oauth2.Token{
		AccessToken: tok,
	}, nil
}

func (p *XdsProxy) initDownstreamServer() error {
	l, err := uds.NewListener(p.xdsUdsPath)
	if err != nil {
		return err
	}
	grpcs := grpc.NewServer()
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcs, p)
	reflection.Register(grpcs)
	p.downstreamGrpcServer = grpcs
	p.downstreamListener = l
	return nil
}

// getCertKeyPaths returns the paths for key and cert.
func (p *XdsProxy) getCertKeyPaths(agent *Agent) (string, string) {
	var key, cert string
	if agent.secOpts.ProvCert != "" {
		key = path.Join(agent.secOpts.ProvCert, constants.KeyFilename)
		cert = path.Join(path.Join(agent.secOpts.ProvCert, constants.CertChainFilename))

		// CSR may not have completed – use JWT to auth.
		if _, err := os.Stat(key); os.IsNotExist(err) {
			return "", ""
		}
		if _, err := os.Stat(cert); os.IsNotExist(err) {
			return "", ""
		}
	} else if agent.secOpts.FileMountedCerts {
		key = agent.proxyConfig.ProxyMetadata[MetadataClientCertKey]
		cert = agent.proxyConfig.ProxyMetadata[MetadataClientCertChain]
	}
	return key, cert
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

	if !sa.secOpts.FileMountedCerts {
		dialOptions = append(dialOptions, grpc.WithPerRPCCredentials(caclient.NewXDSTokenProvider(sa.secOpts)))
	}
	return dialOptions, nil
}

// Returns the TLS option to use when talking to Istiod
// If provisioned cert is set, it will return a mTLS related config
// Else it will return a one-way TLS related config with the assumption
// that the consumer code will use tokens to authenticate the upstream.
func (p *XdsProxy) getTLSDialOption(agent *Agent) (grpc.DialOption, error) {
	if agent.proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_NONE {
		return grpc.WithInsecure(), nil
	}
	rootCert, err := p.getRootCertificate(agent)
	if err != nil {
		return nil, err
	}

	config := tls.Config{
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			var certificate tls.Certificate
			key, cert := p.getCertKeyPaths(agent)
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
	config.MinVersion = tls.VersionTLS12
	transportCreds := credentials.NewTLS(&config)
	return grpc.WithTransportCredentials(transportCreds), nil
}

func (p *XdsProxy) getRootCertificate(agent *Agent) (*x509.CertPool, error) {
	var certPool *x509.CertPool
	var err error
	var rootCert []byte
	xdsCACertPath := agent.FindRootCAForXDS()
	rootCert, err = ioutil.ReadFile(xdsCACertPath)
	if err != nil {
		return nil, err
	}

	certPool = x509.NewCertPool()
	ok := certPool.AppendCertsFromPEM(rootCert)
	if !ok {
		return nil, fmt.Errorf("failed to create TLS dial option with root certificates")
	}
	return certPool, nil
}

// sendUpstreamWithTimeout sends discovery request with default send timeout.
func sendUpstreamWithTimeout(ctx context.Context, upstream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient,
	request *discovery.DiscoveryRequest) error {
	return sendWithTimeout(ctx, func(errChan chan error) {
		errChan <- upstream.Send(request)
		close(errChan)
	})
}

// sendDownstreamWithTimeout sends discovery response with default send timeout.
func sendDownstreamWithTimeout(downstream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer,
	response *discovery.DiscoveryResponse) error {
	return sendWithTimeout(context.Background(), func(errChan chan error) {
		errChan <- downstream.Send(response)
		close(errChan)
	})
}

func sendWithTimeout(ctx context.Context, sendFunc func(errorChan chan error)) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	errChan := make(chan error, 1)
	go sendFunc(errChan)

	select {
	case <-timeoutCtx.Done():
		return timeoutCtx.Err()
	case err := <-errChan:
		return err
	}
}
