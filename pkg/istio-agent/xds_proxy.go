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
	"io/ioutil"
	"math"
	"net"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/ptypes"
	"golang.org/x/oauth2"
	google_rpc "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/dns"
	nds "istio.io/istio/pilot/pkg/proto"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/istio-agent/health"
	"istio.io/istio/pkg/istio-agent/upstream"
	"istio.io/istio/pkg/uds"
)

var (
	newFileWatcher = filewatcher.NewWatcher
)

const (
	defaultClientMaxReceiveMessageSize = math.MaxInt32
	defaultInitialConnWindowSize       = 1024 * 1024            // default gRPC InitialWindowSize
	defaultInitialWindowSize           = 1024 * 1024            // default gRPC ConnWindowSize
	watchDebounceDelay                 = 100 * time.Millisecond // file watcher event debounce delay.
)

const (
	xdsUdsPath = "./etc/istio/proxy/XDS"
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
	upstreamConnection   *grpc.ClientConn
	localDNSServer       *dns.LocalDNSServer
	agent                *Agent
	mutex                sync.RWMutex
	healthcheckResultCh  map[int64]chan *discovery.DiscoveryRequest
}

var proxyLog = log.RegisterScope("xdsproxy", "XDS Proxy in Istio Agent", 0)

func initXdsProxy(ia *Agent, dialOptions ...grpc.DialOption) (*XdsProxy, error) {
	var err error
	proxy := &XdsProxy{
		clusterID:           ia.secOpts.ClusterID,
		localDNSServer:      ia.localDNSServer,
		stopChan:            make(chan struct{}),
		healthcheckResultCh: make(map[int64]chan *discovery.DiscoveryRequest, 5),
		agent:               ia,
	}

	if err = proxy.initDownstreamServer(); err != nil {
		return nil, err
	}

	go func() {
		if err := proxy.downstreamGrpcServer.Serve(proxy.downstreamListener); err != nil {
			log.Errorf("failed to accept downstream gRPC connection %v", err)
		}
	}()

	proxyLog.Infof("Initializing with upstream address %s and cluster %s", ia.proxyConfig.DiscoveryAddress, proxy.clusterID)
	if len(dialOptions) == 0 {
		dialOptions, err = proxy.buildUpstreamClientDialOpts(ia)
		if err != nil {
			return nil, err
		}
	}

	proxy.upstreamConnection, err = grpc.Dial(ia.proxyConfig.DiscoveryAddress, dialOptions...)
	if err != nil {
		proxyLog.Errorf("failed to connect to upstream %s: %v", ia.proxyConfig.DiscoveryAddress, err)
		return nil, err
	}
	healthChecker := health.NewWorkloadHealthChecker(ia.proxyConfig.ReadinessProbe)
	go healthChecker.PerformApplicationHealthCheck(proxy.SendHealthCheckRequest, proxy.stopChan)
	return proxy, nil
}

// SendHealthCheckRequest sends a request to the currently connected proxy
func (p *XdsProxy) SendHealthCheckRequest(healthEvent *health.ProbeEvent) {
	var req *discovery.DiscoveryRequest
	if healthEvent.Healthy {
		req = &discovery.DiscoveryRequest{TypeUrl: health.HealthInfoTypeURL}
	} else {
		req = &discovery.DiscoveryRequest{
			TypeUrl: health.HealthInfoTypeURL,
			ErrorDetail: &google_rpc.Status{
				Code:    500,
				Message: healthEvent.UnhealthyMessage,
			},
		}
	}

	p.mutex.RLock()
	for _, ch := range p.healthcheckResultCh {
		select {
		case ch <- req:
		default:
			// TODO: maybe retry
			proxyLog.Warnf("health check: request channel blocking")
		}
	}
	p.mutex.RUnlock()
}

// Tracks connections, increment on each new connection.
var connectionNumber = int64(0)

func connectionID() int64 {
	return atomic.AddInt64(&connectionNumber, 1)
}

// Every time envoy makes a fresh connection to the agent, we reestablish a new connection to the upstream xds
// This ensures that a new connection between istiod and agent doesn't end up consuming pending messages from envoy
// as the new connection may not go to the same istiod. Vice versa case also applies.
func (p *XdsProxy) StreamAggregatedResources(downstream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	proxyLog.Infof("Envoy ADS stream established")
	requestChan := make(chan *discovery.DiscoveryRequest, 10)
	connID := connectionID()
	p.mutex.Lock()
	p.healthcheckResultCh[connID] = requestChan
	p.mutex.Unlock()
	defer func() {
		p.mutex.Lock()
		delete(p.healthcheckResultCh, connID)
		p.mutex.Unlock()
	}()

	ctx := metadata.AppendToOutgoingContext(context.Background(), "ClusterID", p.clusterID)
	if p.agent.cfg.XDSHeaders != nil {
		for k, v := range p.agent.cfg.XDSHeaders {
			ctx = metadata.AppendToOutgoingContext(ctx, k, v)
		}
	}

	client := upstream.New(ctx, p.upstreamConnection, proxyLog)
	respCh, cancel := client.OpenStream(requestChan)
	defer cancel()

	// indicates that this downstream exit
	shutDownCh := make(chan struct{})
	// Handle downstream recv
	firstNDSSent := false
	go func() {
		for {
			// From Envoy
			req, err := downstream.Recv()
			if err != nil {
				proxyLog.Errorf("downstream recv error: %v", err)
				close(shutDownCh)
				return
			}
			// forward to istiod
			requestChan <- req
			if !firstNDSSent && req.TypeUrl == v3.ListenerType {
				// fire off an initial NDS request
				requestChan <- &discovery.DiscoveryRequest{
					TypeUrl: v3.NameTableType,
				}
				firstNDSSent = true
			}
		}
	}()

	// Handle downstream send
	go func() {
		for {
			select {
			case response := <-respCh:
				if response.TypeUrl != v3.NameTableType {
					// TODO: Validate the known type urls before forwarding them to Envoy.
					if err := downstream.Send(response); err != nil {
						proxyLog.Errorf("downstream send error: %v", err)
						// we cannot return partial error and hope to restart just the downstream
						// as we are blindly proxying req/responses. For now, the best course of action
						// is to terminate upstream connection as well and restart afresh.
						return
					}
				} else {
					// intercept. This is for the dns server
					if p.localDNSServer != nil && len(response.Resources) > 0 {
						var nt nds.NameTable
						if err := ptypes.UnmarshalAny(response.Resources[0], &nt); err != nil {
							log.Errorf("failed to unmarshal name table: %v", err)
							// nack
							requestChan <- &discovery.DiscoveryRequest{
								VersionInfo:   response.VersionInfo,
								TypeUrl:       v3.NameTableType,
								ResponseNonce: response.Nonce,
								ErrorDetail: &google_rpc.Status{
									Code:    500,
									Message: "unable to unmarshal",
								},
							}
							continue
						}
						p.localDNSServer.UpdateLookupTable(&nt)
					}

					// queue the next nds request. This wont block most likely as we are the only
					// users of this channel, compared to the requestChan that could be populated with
					// request from envoy
					requestChan <- &discovery.DiscoveryRequest{
						VersionInfo:   response.VersionInfo,
						TypeUrl:       v3.NameTableType,
						ResponseNonce: response.Nonce,
					}
				}
			case <-shutDownCh: // in case goroutine leak
				return
			}
		}
	}()

	// wait for downstream handle done
	select {
	case <-p.stopChan:
		return nil
	case <-shutDownCh:
		return nil
	}
}

func (p *XdsProxy) DeltaAggregatedResources(server discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return errors.New("delta XDS is not implemented")
}

func (p *XdsProxy) close() {
	close(p.stopChan)
	if p.downstreamGrpcServer != nil {
		_ = p.downstreamGrpcServer.Stop
	}
	if p.downstreamListener != nil {
		_ = p.downstreamListener.Close()
	}
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
	l, err := uds.NewListener(xdsUdsPath)
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
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff:           backoff.DefaultConfig,
			MinConnectTimeout: 1 * time.Second,
		}),
		keepaliveOption, initialWindowSizeOption, initialConnWindowSizeOption, msgSizeOption,
		grpc.WithBlock(),
	}

	// TODO: This is not a valid way of detecting if we are on VM vs k8s
	// Some end users do not use Istiod for CA but run on k8s with file mounted certs
	// In these cases, while we fallback to mTLS to istiod using the provisioned certs
	// it would be ideal to keep using token plus k8s ca certs for control plane communication
	// as the intention behind provisioned certs on k8s pods is only for data plane comm.
	if sa.proxyConfig.ControlPlaneAuthPolicy != meshconfig.AuthenticationPolicy_NONE {
		if sa.secOpts.ProvCert == "" || !sa.secOpts.FileMountedCerts {
			dialOptions = append(dialOptions, grpc.WithPerRPCCredentials(oauth.TokenSource{TokenSource: &fileTokenSource{sa.secOpts.JWTPath}}))
		}
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
