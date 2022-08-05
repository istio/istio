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
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	bootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd/pilot-agent/config"
	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/bootstrap"
	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/config/constants"
	dnsClient "istio.io/istio/pkg/dns/client"
	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/envoy"
	"istio.io/istio/pkg/istio-agent/grpcxds"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/wasm"
	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/istio/security/pkg/nodeagent/caclient"
	citadel "istio.io/istio/security/pkg/nodeagent/caclient/providers/citadel"
	gca "istio.io/istio/security/pkg/nodeagent/caclient/providers/google"
	cas "istio.io/istio/security/pkg/nodeagent/caclient/providers/google-cas"
	"istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"
)

// To debug:
// curl -X POST localhost:15000/logging?config=trace - to see SendingDiscoveryRequest

// Breakpoints in secretcache.go GenerateSecret..

// Note that istiod currently can't validate the JWT token unless it runs on k8s
// Main problem is the JWT validation check which hardcodes the k8s server address and token location.
//
// To test on a local machine, for debugging:
//
// kis exec $POD -- cat /run/secrets/istio-token/istio-token > var/run/secrets/tokens/istio-token
// kis port-forward $POD 15010:15010 &
//
// You can also copy the K8S CA and a token to be used to connect to k8s - but will need removing the hardcoded addr
// kis exec $POD -- cat /run/secrets/kubernetes.io/serviceaccount/{ca.crt,token} > var/run/secrets/kubernetes.io/serviceaccount/
//
// Or disable the jwt validation while debugging SDS problems.

const (
	// Location of K8S CA root.
	k8sCAPath = "./var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	// CitadelCACertPath is the directory for Citadel CA certificate.
	// This is mounted from config map 'istio-ca-root-cert'. Part of startup,
	// this may be replaced with ./etc/certs, if a root-cert.pem is found, to
	// handle secrets mounted from non-citadel CAs.
	CitadelCACertPath = "./var/run/secrets/istio"
)

const (
	// MetadataClientCertKey is ISTIO_META env var used for client key.
	MetadataClientCertKey = "ISTIO_META_TLS_CLIENT_KEY"
	// MetadataClientCertChain is ISTIO_META env var used for client cert chain.
	MetadataClientCertChain = "ISTIO_META_TLS_CLIENT_CERT_CHAIN"
	// MetadataClientRootCert is ISTIO_META env var used for client root cert.
	MetadataClientRootCert = "ISTIO_META_TLS_CLIENT_ROOT_CERT"
)

var _ ready.Prober = &Agent{}

// Agent contains the configuration of the agent, based on the injected
// environment:
// - SDS hostPath if node-agent was used
// - /etc/certs/key if Citadel or other mounted Secrets are used
// - root cert to use for connecting to XDS server
// - CA address, with proper defaults and detection
type Agent struct {
	proxyConfig *mesh.ProxyConfig

	cfg       *AgentOptions
	secOpts   *security.Options
	envoyOpts envoy.ProxyConfig

	envoyAgent  *envoy.Agent
	envoyWaitCh chan error

	sdsServer   *sds.Server
	secretCache *cache.SecretManagerClient

	// Used when proxying envoy xds via istio-agent is enabled.
	xdsProxy      *XdsProxy
	caFileWatcher filewatcher.FileWatcher

	// local DNS Server that processes DNS requests locally and forwards to upstream DNS if needed.
	localDNSServer *dnsClient.LocalDNSServer

	// Signals true completion (e.g. with delayed graceful termination of Envoy)
	wg sync.WaitGroup
}

// AgentOptions contains additional config for the agent, not included in ProxyConfig.
// Most are from env variables ( still experimental ) or for testing only.
// Eventually most non-test settings should graduate to ProxyConfig
// Please don't add 100 parameters to the NewAgent function (or any other)!
type AgentOptions struct {
	// ProxyXDSDebugViaAgent if true will listen on 15004 and forward queries
	// to XDS istio.io/debug. (Requires ProxyXDSViaAgent).
	ProxyXDSDebugViaAgent bool
	// Port value for the debugging endpoint.
	ProxyXDSDebugViaAgentPort int
	// DNSCapture indicates if the XDS proxy has dns capture enabled or not
	// This option will not be considered if proxyXDSViaAgent is false.
	DNSCapture bool
	// DNSAddr is the DNS capture address
	DNSAddr string
	// DNSForwardParallel indicates whether the agent should send parallel DNS queries to all upstream nameservers.
	DNSForwardParallel bool
	// ProxyType is the type of proxy we are configured to handle
	ProxyType model.NodeType
	// ProxyNamespace to use for local dns resolution
	ProxyNamespace string
	// ProxyDomain is the DNS domain associated with the proxy (assumed
	// to include the namespace as well) (for local dns resolution)
	ProxyDomain string
	// Node identifier used by Envoy
	ServiceNode string

	// XDSRootCerts is the location of the root CA for the XDS connection. Used for setting platform certs or
	// using custom roots.
	XDSRootCerts string

	// CARootCerts of the location of the root CA for the CA connection. Used for setting platform certs or
	// using custom roots.
	CARootCerts string

	// Extra headers to add to the XDS connection.
	XDSHeaders map[string]string

	// Is the proxy an IPv6 proxy
	IsIPv6 bool

	// Path to local UDS to communicate with Envoy
	XdsUdsPath string

	// Ability to retrieve ProxyConfig dynamically through XDS
	EnableDynamicProxyConfig bool

	// All of the proxy's IP Addresses
	ProxyIPAddresses []string

	// Enables dynamic generation of bootstrap.
	EnableDynamicBootstrap bool

	// Envoy status port (that circles back to the agent status port). Really belongs to the proxy config.
	// Cannot be eradicated because mistakes have been made.
	EnvoyStatusPort int

	// Envoy prometheus port that circles back to its admin port for prom endpoint. Really belongs to the
	// proxy config.
	EnvoyPrometheusPort int

	MinimumDrainDuration time.Duration

	ExitOnZeroActiveConnections bool

	// Cloud platform
	Platform platform.Environment

	// GRPCBootstrapPath if set will generate a file compatible with GRPC_XDS_BOOTSTRAP
	GRPCBootstrapPath string

	// Disables all envoy agent features
	DisableEnvoy          bool
	DownstreamGrpcOptions []grpc.ServerOption

	IstiodSAN string

	WASMOptions wasm.Options
}

// NewAgent hosts the functionality for local SDS and XDS. This consists of the local SDS server and
// associated clients to sign certificates (when not using files), and the local XDS proxy (including
// health checking for VMs and DNS proxying).
func NewAgent(proxyConfig *mesh.ProxyConfig, agentOpts *AgentOptions, sopts *security.Options, eopts envoy.ProxyConfig) *Agent {
	return &Agent{
		proxyConfig:   proxyConfig,
		cfg:           agentOpts,
		secOpts:       sopts,
		envoyOpts:     eopts,
		caFileWatcher: filewatcher.NewWatcher(),
	}
}

// EnvoyDisabled if true indicates calling Run will not run and wait for Envoy.
func (a *Agent) EnvoyDisabled() bool {
	return a.envoyOpts.TestOnly || a.cfg.DisableEnvoy
}

// WaitForSigterm if true indicates calling Run will block until SIGTERM or SIGNT is received.
func (a *Agent) WaitForSigterm() bool {
	return a.EnvoyDisabled() && !a.envoyOpts.TestOnly
}

func (a *Agent) generateNodeMetadata() (*model.Node, error) {
	provCert, err := a.FindRootCAForXDS()
	if err != nil {
		return nil, fmt.Errorf("failed to find root CA cert for XDS: %v", err)
	}

	if provCert == "" {
		// Envoy only supports load from file. If we want to use system certs, use best guess
		// To be more correct this could lookup all the "well known" paths but this is extremely \
		// unlikely to run on a non-debian based machine, and if it is it can be explicitly configured
		provCert = "/etc/ssl/certs/ca-certificates.crt"
	}
	var pilotSAN []string
	if a.proxyConfig.ControlPlaneAuthPolicy == mesh.AuthenticationPolicy_MUTUAL_TLS {
		// Obtain Pilot SAN, using DNS.
		pilotSAN = []string{config.GetPilotSan(a.proxyConfig.DiscoveryAddress)}
	}

	return bootstrap.GetNodeMetaData(bootstrap.MetadataOptions{
		ID:                          a.cfg.ServiceNode,
		Envs:                        os.Environ(),
		Platform:                    a.cfg.Platform,
		InstanceIPs:                 a.cfg.ProxyIPAddresses,
		StsPort:                     a.secOpts.STSPort,
		ProxyConfig:                 a.proxyConfig,
		PilotSubjectAltName:         pilotSAN,
		OutlierLogPath:              a.envoyOpts.OutlierLogPath,
		ProvCert:                    provCert,
		EnvoyPrometheusPort:         a.cfg.EnvoyPrometheusPort,
		EnvoyStatusPort:             a.cfg.EnvoyStatusPort,
		ExitOnZeroActiveConnections: a.cfg.ExitOnZeroActiveConnections,
		XDSRootCert:                 a.cfg.XDSRootCerts,
	})
}

func (a *Agent) initializeEnvoyAgent(ctx context.Context, credentialSocketExists bool) error {
	node, err := a.generateNodeMetadata()
	if credentialSocketExists {
		node.RawMetadata[security.CredentialMetaDataName] = "true"
	}
	if err != nil {
		return fmt.Errorf("failed to generate bootstrap metadata: %v", err)
	}

	log.Infof("Pilot SAN: %v", node.Metadata.PilotSubjectAltName)

	// Note: the cert checking still works, the generated file is updated if certs are changed.
	// We just don't save the generated file, but use a custom one instead. Pilot will keep
	// monitoring the certs and restart if the content of the certs changes.
	if len(a.proxyConfig.CustomConfigFile) > 0 {
		// there is a custom configuration. Don't write our own config - but keep watching the certs.
		a.envoyOpts.ConfigPath = a.proxyConfig.CustomConfigFile
		a.envoyOpts.ConfigCleanup = false
	} else {
		out, err := bootstrap.New(bootstrap.Config{
			Node: node,
		}).CreateFile()
		if err != nil {
			return fmt.Errorf("failed to generate bootstrap config: %v", err)
		}
		a.envoyOpts.ConfigPath = out
		a.envoyOpts.ConfigCleanup = true
	}

	// Back-fill envoy options from proxy config options
	a.envoyOpts.BinaryPath = a.proxyConfig.BinaryPath
	a.envoyOpts.AdminPort = a.proxyConfig.ProxyAdminPort
	a.envoyOpts.DrainDuration = a.proxyConfig.DrainDuration
	a.envoyOpts.ParentShutdownDuration = a.proxyConfig.ParentShutdownDuration
	a.envoyOpts.Concurrency = a.proxyConfig.Concurrency.GetValue()

	// Checking only uid should be sufficient - but tests also run as root and
	// will break due to permission errors if we start envoy as 1337.
	// This is a mode used for permission-less docker, where iptables can't be
	// used.
	a.envoyOpts.AgentIsRoot = os.Getuid() == 0 && strings.HasSuffix(a.cfg.DNSAddr, ":53")

	envoyProxy := envoy.NewProxy(a.envoyOpts)

	drainDuration := a.proxyConfig.TerminationDrainDuration.AsDuration()
	localHostAddr := localHostIPv4
	if a.cfg.IsIPv6 {
		localHostAddr = localHostIPv6
	}
	a.envoyAgent = envoy.NewAgent(envoyProxy, drainDuration, a.cfg.MinimumDrainDuration, localHostAddr,
		int(a.proxyConfig.ProxyAdminPort), a.cfg.EnvoyStatusPort, a.cfg.EnvoyPrometheusPort, a.cfg.ExitOnZeroActiveConnections)
	a.envoyWaitCh = make(chan error, 1)
	if a.cfg.EnableDynamicBootstrap {
		// Simulate an xDS request for a bootstrap
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()

			// wait indefinitely and keep retrying with jittered exponential backoff
			backoff := 500
			max := 30000
		retries:
			for {
				// handleStream hands on to request after exit, so create a fresh one instead.
				request := &bootstrapDiscoveryRequest{
					node:        node,
					envoyWaitCh: a.envoyWaitCh,
					envoyUpdate: envoyProxy.UpdateConfig,
				}
				_ = a.xdsProxy.handleStream(request)
				select {
				case <-a.envoyWaitCh:
					break retries
				default:
				}
				delay := time.Duration(rand.Int()%backoff) * time.Millisecond
				log.Infof("retrying bootstrap discovery request with backoff: %v", delay)
				select {
				case <-ctx.Done():
					break retries
				case <-time.After(delay):
				}
				if backoff < max/2 {
					backoff *= 2
				} else {
					backoff = max
				}
			}
		}()
	} else {
		close(a.envoyWaitCh)
	}
	return nil
}

type bootstrapDiscoveryRequest struct {
	node        *model.Node
	envoyWaitCh chan error
	envoyUpdate func(data []byte) error
	sent        bool
	received    bool
}

// Send refers to a request from the xDS proxy.
func (b *bootstrapDiscoveryRequest) Send(resp *discovery.DiscoveryResponse) error {
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
func (b *bootstrapDiscoveryRequest) Recv() (*discovery.DiscoveryRequest, error) {
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

func (b *bootstrapDiscoveryRequest) Context() context.Context { return context.Background() }

// Run is a non-blocking call which returns either an error or a function to await for completion.
func (a *Agent) Run(ctx context.Context) (func(), error) {
	var err error
	if err = a.initLocalDNSServer(); err != nil {
		return nil, fmt.Errorf("failed to start local DNS server: %v", err)
	}

	socketExists, err := checkSocket(ctx, security.WorkloadIdentitySocketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check SDS socket: %v", err)
	}

	if socketExists {
		log.Info("Workload SDS socket found. Istio SDS Server won't be started")
	} else {
		log.Info("Workload SDS socket not found. Starting Istio SDS Server")
		err = a.initSdsServer()
		if err != nil {
			return nil, fmt.Errorf("failed to start SDS server: %v", err)
		}
	}
	credentialSocketExists, err := checkSocket(ctx, security.CredentialNameSocketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check credential SDS socket: %v", err)
	}
	if credentialSocketExists {
		log.Info("Credential SDS socket found")
	}
	a.xdsProxy, err = initXdsProxy(a)
	if err != nil {
		return nil, fmt.Errorf("failed to start xds proxy: %v", err)
	}
	if a.cfg.ProxyXDSDebugViaAgent {
		err = a.xdsProxy.initDebugInterface(a.cfg.ProxyXDSDebugViaAgentPort)
		if err != nil {
			return nil, fmt.Errorf("failed to start istio tap server: %v", err)
		}
	}

	if a.cfg.GRPCBootstrapPath != "" {
		if err := a.generateGRPCBootstrap(credentialSocketExists); err != nil {
			return nil, fmt.Errorf("failed generating gRPC XDS bootstrap: %v", err)
		}
	}
	rootCAForXDS, err := a.FindRootCAForXDS()
	if err != nil {
		return nil, fmt.Errorf("failed to find root XDS CA: %v", err)
	}
	go a.caFileWatcherHandler(ctx, rootCAForXDS)

	if !a.EnvoyDisabled() {
		err = a.initializeEnvoyAgent(ctx, credentialSocketExists)
		if err != nil {
			return nil, fmt.Errorf("failed to start envoy agent: %v", err)
		}

		a.wg.Add(1)
		go func() {
			defer a.wg.Done()

			if a.cfg.EnableDynamicBootstrap {
				start := time.Now()
				var err error
				select {
				case err = <-a.envoyWaitCh:
				case <-ctx.Done():
					// Early cancellation before envoy started.
					return
				}
				if err != nil {
					log.Errorf("failed to write updated envoy bootstrap: %v", err)
					return
				}
				log.Infof("received server-side bootstrap in %v", time.Since(start))
			}

			// This is a blocking call for graceful termination.
			a.envoyAgent.Run(ctx)
		}()
	} else if a.WaitForSigterm() {
		// wait for SIGTERM and perform graceful shutdown
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			<-ctx.Done()
		}()
	}
	return a.wg.Wait, nil
}

func (a *Agent) initSdsServer() error {
	var err error
	if security.CheckWorkloadCertificate(security.WorkloadIdentityCertChainPath, security.WorkloadIdentityKeyPath, security.WorkloadIdentityRootCertPath) {
		log.Info("workload certificate files detected, creating secret manager without caClient")
		a.secOpts.RootCertFilePath = security.WorkloadIdentityRootCertPath
		a.secOpts.CertChainFilePath = security.WorkloadIdentityCertChainPath
		a.secOpts.KeyFilePath = security.WorkloadIdentityKeyPath
		a.secOpts.FileMountedCerts = true
	}

	a.secretCache, err = a.newSecretManager()
	if err != nil {
		return fmt.Errorf("failed to start workload secret manager %v", err)
	}

	if a.cfg.DisableEnvoy {
		// For proxyless we don't need an SDS server, but still need the keys and
		// we need them refreshed periodically.
		//
		// This is based on the code from newSDSService, but customized to have explicit rotation.
		go func() {
			st := a.secretCache
			st.RegisterSecretHandler(func(resourceName string) {
				// The secret handler is called when a secret should be renewed, after invalidating the cache.
				// The handler does not call GenerateSecret - it is a side-effect of the SDS generate() method, which
				// is called by sdsServer.OnSecretUpdate, which triggers a push and eventually calls sdsservice.Generate
				// TODO: extract the logic to detect expiration time, and use a simpler code to rotate to files.
				_, _ = a.getWorkloadCerts(st)
			})
			_, _ = a.getWorkloadCerts(st)
		}()
	} else {
		pkpConf := a.proxyConfig.GetPrivateKeyProvider()
		a.sdsServer = sds.NewServer(a.secOpts, a.secretCache, pkpConf)
		a.secretCache.RegisterSecretHandler(a.sdsServer.OnSecretUpdate)
	}

	return nil
}

// getWorkloadCerts will attempt to get a cert, with infinite exponential backoff
// It will not return until both workload cert and root cert are generated.
//
// TODO: evaluate replacing the STS server with a file data source, to simplify Envoy config
func (a *Agent) getWorkloadCerts(st *cache.SecretManagerClient) (sk *security.SecretItem, err error) {
	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = 0

	for {
		sk, err = st.GenerateSecret(security.WorkloadKeyCertResourceName)
		if err == nil {
			break
		}
		log.Warnf("failed to get certificate: %v", err)
		time.Sleep(b.NextBackOff())
	}
	for {
		_, err := st.GenerateSecret(security.RootCertReqResourceName)
		if err == nil {
			break
		}
		log.Warnf("failed to get root certificate: %v", err)
		time.Sleep(b.NextBackOff())
	}
	return
}

func (a *Agent) caFileWatcherHandler(ctx context.Context, caFile string) {
	if err := a.caFileWatcher.Add(caFile); err != nil {
		log.Warnf("Failed to add file watcher %s, caFile", caFile)
	}

	log.Debugf("Add CA file %s watcher", caFile)
	for {
		select {
		case gotEvent := <-a.caFileWatcher.Events(caFile):
			log.Debugf("Receive file %s event %v", caFile, gotEvent)
			if err := a.xdsProxy.initIstiodDialOptions(a); err != nil {
				log.Warnf("Failed to init xds proxy dial options")
			}
		case <-ctx.Done():
			return
		}
	}
}

func (a *Agent) initLocalDNSServer() (err error) {
	// we don't need dns server on gateways
	if a.cfg.DNSCapture && a.cfg.ProxyType == model.SidecarProxy {
		if a.localDNSServer, err = dnsClient.NewLocalDNSServer(a.cfg.ProxyNamespace, a.cfg.ProxyDomain, a.cfg.DNSAddr,
			a.cfg.DNSForwardParallel); err != nil {
			return err
		}
		a.localDNSServer.StartDNS()
	}
	return nil
}

func (a *Agent) generateGRPCBootstrap(credentialSocketExists bool) error {
	// generate metadata
	node, err := a.generateNodeMetadata()
	if credentialSocketExists {
		node.RawMetadata[security.CredentialMetaDataName] = "true"
	}

	if err != nil {
		return fmt.Errorf("failed generating node metadata: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(a.cfg.GRPCBootstrapPath), 0o700); err != nil {
		return err
	}

	_, err = grpcxds.GenerateBootstrapFile(grpcxds.GenerateBootstrapOptions{
		Node:             node,
		XdsUdsPath:       a.cfg.XdsUdsPath,
		DiscoveryAddress: a.proxyConfig.DiscoveryAddress,
		CertDir:          a.secOpts.OutputKeyCertToDir,
	}, a.cfg.GRPCBootstrapPath)
	if err != nil {
		return err
	}
	return nil
}

// Check is used in to readiness check of agent to ensure DNSServer is ready.
func (a *Agent) Check() (err error) {
	// we dont need dns server on gateways
	if a.cfg.DNSCapture && a.cfg.ProxyType == model.SidecarProxy {
		if !a.localDNSServer.IsReady() {
			return errors.New("istio DNS capture is turned ON and DNS lookup table is not ready yet")
		}
	}
	return nil
}

// GetDNSTable builds DNS table used in debugging interface.
func (a *Agent) GetDNSTable() *dnsProto.NameTable {
	if a.localDNSServer != nil && a.localDNSServer.NameTable() != nil {
		nt := a.localDNSServer.NameTable()
		nt = proto.Clone(nt).(*dnsProto.NameTable)
		a.localDNSServer.BuildAlternateHosts(nt, func(althosts map[string]struct{}, ipv4 []net.IP, ipv6 []net.IP, _ []string) {
			for host := range althosts {
				if _, exists := nt.Table[host]; !exists {
					addresses := make([]string, len(ipv4)+len(ipv6))
					for _, ip := range ipv4 {
						addresses = append(addresses, ip.String())
					}
					for _, ip := range ipv6 {
						addresses = append(addresses, ip.String())
					}
					nt.Table[host] = &dnsProto.NameTable_NameInfo{
						Ips:      addresses,
						Registry: "Kubernetes",
					}
				}
			}
		})
		return nt
	}
	return nil
}

func (a *Agent) close() {
	if a.xdsProxy != nil {
		a.xdsProxy.close()
	}
	if a.localDNSServer != nil {
		a.localDNSServer.Close()
	}
	if a.sdsServer != nil {
		a.sdsServer.Stop()
	}
	if a.secretCache != nil {
		a.secretCache.Close()
	}
	if a.caFileWatcher != nil {
		_ = a.caFileWatcher.Close()
	}
}

// FindRootCAForXDS determines the root CA to be configured in bootstrap file.
// It may be different from the CA for the cert server - which is based on CA_ADDR
// In addition it deals with the case the XDS server is on port 443, expected with a proper cert.
// /etc/ssl/certs/ca-certificates.crt
func (a *Agent) FindRootCAForXDS() (string, error) {
	var rootCAPath string

	if a.cfg.XDSRootCerts == security.SystemRootCerts {
		// Special case input for root cert configuration to use system root certificates
		return "", nil
	} else if a.cfg.XDSRootCerts != "" {
		// Using specific platform certs or custom roots
		rootCAPath = a.cfg.XDSRootCerts
	} else if fileExists(security.DefaultRootCertFilePath) {
		// Old style - mounted cert. This is used for XDS auth only,
		// not connecting to CA_ADDR because this mode uses external
		// agent (Secret refresh, etc)
		return security.DefaultRootCertFilePath, nil
	} else if a.secOpts.PilotCertProvider == constants.CertProviderKubernetes {
		// Using K8S - this is likely incorrect, may work by accident (https://github.com/istio/istio/issues/22161)
		rootCAPath = k8sCAPath
	} else if a.secOpts.ProvCert != "" {
		// This was never completely correct - PROV_CERT are only intended for auth with CA_ADDR,
		// and should not be involved in determining the root CA.
		// For VMs, the root cert file used to auth may be populated afterwards.
		// Thus, return directly here and skip checking for existence.
		return a.secOpts.ProvCert + "/root-cert.pem", nil
	} else if a.secOpts.FileMountedCerts {
		// FileMountedCerts - Load it from Proxy Metadata.
		rootCAPath = a.proxyConfig.ProxyMetadata[MetadataClientRootCert]
	} else if a.secOpts.PilotCertProvider == constants.CertProviderNone {
		return "", fmt.Errorf("root CA file for XDS required but configured provider as none")
	} else {
		// PILOT_CERT_PROVIDER - default is istiod
		// This is the default - a mounted config map on K8S
		rootCAPath = path.Join(CitadelCACertPath, constants.CACertNamespaceConfigMapDataName)
	}

	// Additional checks for root CA cert existence. Fail early, instead of obscure envoy errors
	if fileExists(rootCAPath) {
		return rootCAPath, nil
	}

	return "", fmt.Errorf("root CA file for XDS does not exist %s", rootCAPath)
}

// GetKeyCertsForXDS return the key cert files path for connecting with xds.
func (a *Agent) GetKeyCertsForXDS() (string, string) {
	var key, cert string
	if a.secOpts.ProvCert != "" {
		key, cert = getKeyCertInner(a.secOpts.ProvCert)
	} else if a.secOpts.FileMountedCerts {
		key = a.proxyConfig.ProxyMetadata[MetadataClientCertKey]
		cert = a.proxyConfig.ProxyMetadata[MetadataClientCertChain]
	}
	return key, cert
}

func fileExists(path string) bool {
	if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
		return true
	}
	return false
}

func socketFileExists(path string) bool {
	if fi, err := os.Stat(path); err == nil && !fi.Mode().IsRegular() {
		return true
	}
	return false
}

// Checks whether the socket exists and is responsive.
// If it doesn't exist, returns (false, nil)
// If it exists and is NOT responsive, tries to delete the socket file.
// If it can be deleted, returns (false, nil).
// If it cannot be deleted, returns (false, error).
// Otherwise, returns (true, nil)
func checkSocket(ctx context.Context, socketPath string) (bool, error) {
	socketExists := socketFileExists(socketPath)
	if !socketExists {
		return false, nil
	}

	err := socketHealthCheck(ctx, socketPath)
	if err != nil {
		log.Debugf("SDS socket detected but not healthy: %v", err)
		err = os.Remove(socketPath)
		if err != nil {
			return false, fmt.Errorf("existing SDS socket could not be removed: %v", err)
		}
		return false, nil
	}

	return true, nil
}

func socketHealthCheck(ctx context.Context, socketPath string) error {
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(time.Second))
	defer cancel()

	conn, err := grpc.DialContext(ctx, fmt.Sprintf("unix:%s", socketPath),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.FailOnNonTempDialError(true),
		grpc.WithReturnConnectionError(),
		grpc.WithBlock(),
	)
	if err != nil {
		return err
	}
	conn.Close()

	return nil
}

// FindRootCAForCA Find the root CA to use when connecting to the CA (Istiod or external).
func (a *Agent) FindRootCAForCA() (string, error) {
	var rootCAPath string

	if a.cfg.CARootCerts == security.SystemRootCerts {
		return "", nil
	} else if a.cfg.CARootCerts != "" {
		rootCAPath = a.cfg.CARootCerts
	} else if a.secOpts.PilotCertProvider == constants.CertProviderKubernetes {
		// Using K8S - this is likely incorrect, may work by accident.
		// API is GA.
		rootCAPath = k8sCAPath // ./var/run/secrets/kubernetes.io/serviceaccount/ca.crt
	} else if a.secOpts.PilotCertProvider == constants.CertProviderCustom {
		rootCAPath = security.DefaultRootCertFilePath // ./etc/certs/root-cert.pem
	} else if a.secOpts.ProvCert != "" {
		// This was never completely correct - PROV_CERT are only intended for auth with CA_ADDR,
		// and should not be involved in determining the root CA.
		// For VMs, the root cert file used to auth may be populated afterwards.
		// Thus, return directly here and skip checking for existence.
		return a.secOpts.ProvCert + "/root-cert.pem", nil
	} else if a.secOpts.PilotCertProvider == constants.CertProviderNone {
		return "", fmt.Errorf("root CA file for CA required but configured provider as none")
	} else {
		// This is the default - a mounted config map on K8S
		rootCAPath = path.Join(CitadelCACertPath, constants.CACertNamespaceConfigMapDataName)
		// or: "./var/run/secrets/istio/root-cert.pem"
	}

	// Additional checks for root CA cert existence.
	if fileExists(rootCAPath) {
		return rootCAPath, nil
	}

	return "", fmt.Errorf("root CA file for CA does not exist %s", rootCAPath)
}

// getKeyCertsForXDS return the key cert files path for connecting with CA server.
func (a *Agent) getKeyCertsForCA() (string, string) {
	var key, cert string
	if a.secOpts.ProvCert != "" {
		key, cert = getKeyCertInner(a.secOpts.ProvCert)
	}
	return key, cert
}

func getKeyCertInner(certPath string) (string, string) {
	key := path.Join(certPath, constants.KeyFilename)
	cert := path.Join(certPath, constants.CertChainFilename)
	return key, cert
}

// newSecretManager creates the SecretManager for workload secrets
func (a *Agent) newSecretManager() (*cache.SecretManagerClient, error) {
	// If proxy is using file mounted certs, we do not have to connect to CA.
	if a.secOpts.FileMountedCerts {
		log.Info("Workload is using file mounted certificates. Skipping connecting to CA")
		return cache.NewSecretManagerClient(nil, a.secOpts)
	}
	log.Infof("CA Endpoint %s, provider %s", a.secOpts.CAEndpoint, a.secOpts.CAProviderName)

	// TODO: this should all be packaged in a plugin, possibly with optional compilation.
	if a.secOpts.CAProviderName == security.GoogleCAProvider {
		// Use a plugin to an external CA - this has direct support for the K8S JWT token
		// This is only used if the proper env variables are injected - otherwise the existing Citadel or Istiod will be
		// used.
		caClient, err := gca.NewGoogleCAClient(a.secOpts.CAEndpoint, true, caclient.NewCATokenProvider(a.secOpts))
		if err != nil {
			return nil, err
		}
		return cache.NewSecretManagerClient(caClient, a.secOpts)
	} else if a.secOpts.CAProviderName == security.GoogleCASProvider {
		// Use a plugin
		caClient, err := cas.NewGoogleCASClient(a.secOpts.CAEndpoint,
			option.WithGRPCDialOption(grpc.WithPerRPCCredentials(caclient.NewCATokenProvider(a.secOpts))))
		if err != nil {
			return nil, err
		}
		return cache.NewSecretManagerClient(caClient, a.secOpts)
	}

	// Using citadel CA
	var tlsOpts *citadel.TLSOptions
	var err error
	// Special case: if Istiod runs on a secure network, on the default port, don't use TLS
	// TODO: may add extra cases or explicit settings - but this is a rare use cases, mostly debugging
	if strings.HasSuffix(a.secOpts.CAEndpoint, ":15010") {
		log.Warn("Debug mode or IP-secure network")
	} else {
		tlsOpts = &citadel.TLSOptions{}
		tlsOpts.RootCert, err = a.FindRootCAForCA()
		if err != nil {
			return nil, fmt.Errorf("failed to find root CA cert for CA: %v", err)
		}

		if tlsOpts.RootCert == "" {
			log.Infof("Using CA %s cert with system certs", a.secOpts.CAEndpoint)
		} else if _, err := os.Stat(tlsOpts.RootCert); os.IsNotExist(err) {
			log.Fatalf("invalid config - %s missing a root certificate %s", a.secOpts.CAEndpoint, tlsOpts.RootCert)
		} else {
			log.Infof("Using CA %s cert with certs: %s", a.secOpts.CAEndpoint, tlsOpts.RootCert)
		}

		tlsOpts.Key, tlsOpts.Cert = a.getKeyCertsForCA()
	}

	// Will use TLS unless the reserved 15010 port is used ( istiod on an ipsec/secure VPC)
	// rootCert may be nil - in which case the system roots are used, and the CA is expected to have public key
	// Otherwise assume the injection has mounted /etc/certs/root-cert.pem
	caClient, err := citadel.NewCitadelClient(a.secOpts, tlsOpts)
	if err != nil {
		return nil, err
	}

	return cache.NewSecretManagerClient(caClient, a.secOpts)
}

// GRPCBootstrapPath returns the most recently generated gRPC bootstrap or nil if there is none.
func (a *Agent) GRPCBootstrapPath() string {
	return a.cfg.GRPCBootstrapPath
}
