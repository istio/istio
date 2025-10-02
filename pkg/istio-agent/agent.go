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
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd/pilot-agent/config"
	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pkg/backoff"
	"istio.io/istio/pkg/bootstrap"
	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/config/constants"
	dnsClient "istio.io/istio/pkg/dns/client"
	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/envoy"
	common_features "istio.io/istio/pkg/features"
	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/istio-agent/grpcxds"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/wasm"
	"istio.io/istio/security/pkg/nodeagent/cache"
)

const (
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

type LifecycleEvent string

const (
	DrainLifecycleEvent LifecycleEvent = "drain"
	ExitLifecycleEvent  LifecycleEvent = "exit"
)

type SDSService interface {
	OnSecretUpdate(resourceName string)
	Stop()
}

type SDSServiceFactory = func(_ *security.Options, _ security.SecretManager, _ *mesh.PrivateKeyProvider) SDSService

// Shared properties with Pilot Proxy struct.
type Proxy struct {
	ID          string
	IPAddresses []string
	Type        model.NodeType
	ipMode      model.IPMode
	DNSDomain   string
}

func (node *Proxy) DiscoverIPMode() {
	node.ipMode = model.DiscoverIPMode(node.IPAddresses)
}

// IsIPv6 returns true if proxy only supports IPv6 addresses.
func (node *Proxy) IsIPv6() bool {
	return node.ipMode == model.IPv6
}

func (node *Proxy) SupportsIPv6() bool {
	return node.ipMode == model.IPv6 || node.ipMode == model.Dual
}

const (
	serviceNodeSeparator = "~"
)

func (node *Proxy) ServiceNode() string {
	ip := ""
	if len(node.IPAddresses) > 0 {
		ip = node.IPAddresses[0]
	}
	return strings.Join([]string{
		string(node.Type), ip, node.ID, node.DNSDomain,
	}, serviceNodeSeparator)
}

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

	envoyAgent *envoy.Agent

	sdsServer   SDSService
	secretCache *cache.SecretManagerClient

	// Used when proxying envoy xds via istio-agent is enabled.
	xdsProxy    *XdsProxy
	fileWatcher filewatcher.FileWatcher

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
	// DNSCapture indicates if the XDS proxy has dns capture enabled or not
	DNSCapture bool
	// Enables DNS server at Gateways.
	DNSAtGateway bool
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

	// Enable metadata discovery bootstrap extension
	MetadataDiscovery *bool

	SDSFactory func(options *security.Options, workloadSecretCache security.SecretManager, pkpConf *mesh.PrivateKeyProvider) SDSService

	// Name of the socket file which will be used for workload SDS.
	// If this is set to something other than the default socket file used
	// by Istio's default SDS server, the socket file must be present.
	// Note that the path is not configurable by design - only the socket file name.
	WorkloadIdentitySocketFile string

	EnvoySkipDeprecatedLogs bool
}

// NewAgent hosts the functionality for local SDS and XDS. This consists of the local SDS server and
// associated clients to sign certificates (when not using files), and the local XDS proxy (including
// health checking for VMs and DNS proxying).
func NewAgent(proxyConfig *mesh.ProxyConfig, agentOpts *AgentOptions, sopts *security.Options, eopts envoy.ProxyConfig) *Agent {
	return &Agent{
		proxyConfig: proxyConfig,
		cfg:         agentOpts,
		secOpts:     sopts,
		envoyOpts:   eopts,
		fileWatcher: filewatcher.NewWatcher(),
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
	var pilotSAN []string
	if a.proxyConfig.ControlPlaneAuthPolicy == mesh.AuthenticationPolicy_MUTUAL_TLS {
		// Obtain Pilot SAN, using DNS.
		pilotSAN = []string{config.GetPilotSan(a.proxyConfig.DiscoveryAddress)}
	}

	credentialSocketExists, err := checkSocket(context.TODO(), security.CredentialNameSocketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check credential SDS socket: %v", err)
	}
	if credentialSocketExists {
		log.Info("Credential SDS socket found")
	}

	return bootstrap.GetNodeMetaData(bootstrap.MetadataOptions{
		ID:                          a.cfg.ServiceNode,
		Envs:                        os.Environ(),
		Platform:                    a.cfg.Platform,
		InstanceIPs:                 a.cfg.ProxyIPAddresses,
		StsPort:                     a.secOpts.STSPort,
		ProxyConfig:                 a.proxyConfig,
		PilotSubjectAltName:         pilotSAN,
		CredentialSocketExists:      credentialSocketExists,
		CustomCredentialsFileExists: a.secOpts.ServeOnlyFiles,
		OutlierLogPath:              a.envoyOpts.OutlierLogPath,
		EnvoyPrometheusPort:         a.cfg.EnvoyPrometheusPort,
		EnvoyStatusPort:             a.cfg.EnvoyStatusPort,
		ExitOnZeroActiveConnections: a.cfg.ExitOnZeroActiveConnections,
		XDSRootCert:                 a.cfg.XDSRootCerts,
		MetadataDiscovery:           a.cfg.MetadataDiscovery,
		EnvoySkipDeprecatedLogs:     a.cfg.EnvoySkipDeprecatedLogs,
		WorkloadIdentitySocketFile:  a.cfg.WorkloadIdentitySocketFile,
	})
}

func (a *Agent) initializeEnvoyAgent(_ context.Context) error {
	node, err := a.generateNodeMetadata()
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
			Node:             node,
			CompliancePolicy: common_features.CompliancePolicy,
			LogAsJSON:        a.envoyOpts.LogAsJSON,
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
	a.envoyOpts.Concurrency = a.proxyConfig.Concurrency.GetValue()
	a.envoyOpts.SkipDeprecatedLogs = a.cfg.EnvoySkipDeprecatedLogs

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
	return nil
}

func (a *Agent) SkipDrain() {
	a.envoyAgent.DisableDraining()
}

// Run is a non-blocking call which returns either an error or a function to await for completion.
func (a *Agent) Run(ctx context.Context) (func(), error) {
	var err error
	if err = a.initLocalDNSServer(); err != nil {
		return nil, fmt.Errorf("failed to start local DNS server: %v", err)
	}

	// There are a couple of things we have to do here
	//
	// 1. Use a custom SDS workload socket if one is found+healthy at the configured path.
	//      If we do find one, we will still bind a local SDS server, but it will be used only to serve file certificates.
	// 2. Error out if a custom SDS socket path is configured but no socket is found there.
	// 3. Do NOT error out, but just start and use the default Istio SDS server, if no socket
	// is found AND no custom SDS socket path is configured.
	//
	//
	// We do that like so
	// 1. compare the (hardcoded) Istio default SDS server path with the (configurable) client path.
	// 2. if they are different, do not start the Istio default SDS server.
	// 3. if they are different, and no socket exists at that location, error out.
	// 4. if they are not different, start the Istio default SDS server and use it.

	// Correctness check - we do not want people sneaking paths into this
	if a.cfg.WorkloadIdentitySocketFile != filepath.Base(a.cfg.WorkloadIdentitySocketFile) {
		return nil, fmt.Errorf("workload identity socket file override must be a filename, not a path: %s", a.cfg.WorkloadIdentitySocketFile)
	}

	configuredAgentSocketPath := security.GetWorkloadSDSSocketListenPath(a.cfg.WorkloadIdentitySocketFile)

	// Are we listening to the Istio default SDS server socket, or something else?
	isIstioSDS := configuredAgentSocketPath == security.GetIstioSDSServerSocketPath()

	socketExists, err := checkSocket(ctx, configuredAgentSocketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check SDS socket: %v", err)
	}
	if socketExists {
		log.Infof("Existing workload SDS socket found at %s. Default Istio SDS Server will only serve files", configuredAgentSocketPath)
		a.secOpts.ServeOnlyFiles = true
	} else if !isIstioSDS {
		// If we are configured to use something other than the default Istio SDS server and we can't find a socket at that path, error out.
		return nil, fmt.Errorf("agent configured for non-default SDS socket path: %s but no socket found", configuredAgentSocketPath)
	}

	// otherwise we are not configured to listen to something else, so just start the Istio SDS server and use it.
	log.Info("Starting default Istio SDS Server")
	err = a.initSdsServer()
	if err != nil {
		return nil, fmt.Errorf("failed to start default Istio SDS server: %v", err)
	}
	a.xdsProxy, err = initXdsProxy(a)
	if err != nil {
		return nil, fmt.Errorf("failed to start xds proxy: %v", err)
	}

	if a.cfg.GRPCBootstrapPath != "" {
		if err := a.generateGRPCBootstrap(); err != nil {
			return nil, fmt.Errorf("failed generating gRPC XDS bootstrap: %v", err)
		}
	}
	if a.proxyConfig.ControlPlaneAuthPolicy != mesh.AuthenticationPolicy_NONE {
		rootCAForXDS, err := a.FindRootCAForXDS()
		if err != nil {
			return nil, fmt.Errorf("failed to find root XDS CA: %v", err)
		}
		go a.startFileWatcher(ctx, rootCAForXDS, func() {
			if err := a.xdsProxy.initIstiodDialOptions(a); err != nil {
				log.Warnf("Failed to init xds proxy dial options")
			}
		})
	}

	if !a.EnvoyDisabled() {
		err = a.initializeEnvoyAgent(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize envoy agent: %v", err)
		}

		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
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

	// If proxy is using file mounted certs, we do not have to connect to CA.
	// It can also be explicitly disabled, used when we have an external SDS server for mTLS certs, but still need the file manager.
	createCaClient := !a.secOpts.FileMountedCerts && !a.secOpts.ServeOnlyFiles
	a.secretCache, err = a.newSecretManager(createCaClient)
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
		a.sdsServer = a.cfg.SDSFactory(a.secOpts, a.secretCache, pkpConf)
		a.secretCache.RegisterSecretHandler(a.sdsServer.OnSecretUpdate)
	}

	return nil
}

// getWorkloadCerts will attempt to get a cert, with infinite exponential backoff
// It will not return until both workload cert and root cert are generated.
//
// TODO: evaluate replacing the STS server with a file data source, to simplify Envoy config
// TODO: Fix this method with unused return value
// nolint: unparam
func (a *Agent) getWorkloadCerts(st *cache.SecretManagerClient) (sk *security.SecretItem, err error) {
	b := backoff.NewExponentialBackOff(backoff.DefaultOption())
	// This will loop forever until success
	err = b.RetryWithContext(context.TODO(), func() error {
		sk, err = st.GenerateSecret(security.WorkloadKeyCertResourceName)
		if err == nil {
			return nil
		}
		log.Warnf("failed to get certificate: %v", err)
		return err
	})
	if err != nil {
		return nil, err
	}
	err = b.RetryWithContext(context.TODO(), func() error {
		_, err := st.GenerateSecret(security.RootCertReqResourceName)
		if err == nil {
			return nil
		}
		log.Warnf("failed to get root certificate: %v", err)
		return err
	})
	return sk, err
}

func (a *Agent) startFileWatcher(ctx context.Context, filePath string, handler func()) {
	if err := a.fileWatcher.Add(filePath); err != nil {
		log.Warnf("Failed to add file watcher %s", filePath)
		return
	}

	log.Debugf("Add file %s watcher", filePath)
	for {
		select {
		case gotEvent := <-a.fileWatcher.Events(filePath):
			log.Debugf("Receive file %s event %v", filePath, gotEvent)
			handler()
		case err := <-a.fileWatcher.Errors(filePath):
			log.Warnf("Watch file %s error: %v", filePath, err)
		case <-ctx.Done():
			return
		}
	}
}

func (a *Agent) initLocalDNSServer() (err error) {
	if a.isDNSServerEnabled() {
		if a.localDNSServer, err = dnsClient.NewLocalDNSServer(a.cfg.ProxyNamespace, a.cfg.ProxyDomain, a.cfg.DNSAddr,
			a.cfg.DNSForwardParallel); err != nil {
			return err
		}
		a.localDNSServer.StartDNS()
	}
	return nil
}

func (a *Agent) generateGRPCBootstrap() error {
	// generate metadata
	node, err := a.generateNodeMetadata()
	if err != nil {
		return fmt.Errorf("failed generating node metadata: %v", err)
	}

	// GRPC bootstrap requires this. Original implementation injected this via env variable, but
	// this interfere with envoy, we should be able to use both envoy for TCP/HTTP and proxyless.
	node.Metadata.Generator = "grpc"

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
	if a.isDNSServerEnabled() {
		if !a.localDNSServer.IsReady() {
			return errors.New("istio DNS capture is turned ON and DNS lookup table is not ready yet")
		}
	}
	return nil
}

func (a *Agent) isDNSServerEnabled() bool {
	// Enable DNS capture if the proxy is a sidecar and the feature is enabled.
	// At Gateways, we generally do not need DNS capture. But in some cases, we may want to use DNS proxy
	// if we want Envoy to resolve multi-cluster DNS queries.
	return (a.cfg.DNSCapture && a.cfg.ProxyType == model.SidecarProxy) || a.cfg.DNSAtGateway
}

// GetDNSTable builds DNS table used in debugging interface.
func (a *Agent) GetDNSTable() *dnsProto.NameTable {
	if a.localDNSServer != nil && a.localDNSServer.NameTable() != nil {
		nt := a.localDNSServer.NameTable()
		nt = protomarshal.Clone(nt)
		a.localDNSServer.BuildAlternateHosts(nt, func(althosts map[string]struct{}, ipv4 []netip.Addr, ipv6 []netip.Addr, _ []string) {
			for host := range althosts {
				if _, exists := nt.Table[host]; !exists {
					addresses := make([]string, 0, len(ipv4)+len(ipv6))
					for _, addr := range ipv4 {
						addresses = append(addresses, addr.String())
					}
					for _, addr := range ipv6 {
						addresses = append(addresses, addr.String())
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

func (a *Agent) Close() {
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
	if a.fileWatcher != nil {
		_ = a.fileWatcher.Close()
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
	log := log.WithLabels("path", socketPath)
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
	err = conn.Close()
	if err != nil {
		log.Infof("connection is not closed: %v", err)
	}

	return nil
}

// FindRootCAForCA Find the root CA to use when connecting to the CA (Istiod or external).
func (a *Agent) FindRootCAForCA() (string, error) {
	var rootCAPath string

	if a.cfg.CARootCerts == security.SystemRootCerts {
		return "", nil
	} else if a.cfg.CARootCerts != "" {
		rootCAPath = a.cfg.CARootCerts
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

// GetKeyCertsForXDS return the key cert files path for connecting with CA server.
func (a *Agent) GetKeyCertsForCA() (string, string) {
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
func (a *Agent) newSecretManager(createCaClient bool) (*cache.SecretManagerClient, error) {
	if !createCaClient {
		log.Info("Workload is using file mounted certificates. Skipping connecting to CA")
		return cache.NewSecretManagerClient(nil, a.secOpts)
	}
	log.Infof("CA Endpoint %s, provider %s", a.secOpts.CAEndpoint, a.secOpts.CAProviderName)

	caClient, err := createCAClient(a.secOpts, a)
	if err != nil {
		return nil, err
	}
	return cache.NewSecretManagerClient(caClient, a.secOpts)
}

// GRPCBootstrapPath returns the most recently generated gRPC bootstrap or nil if there is none.
func (a *Agent) GRPCBootstrapPath() string {
	return a.cfg.GRPCBootstrapPath
}

func (a *Agent) DrainNow() {
	a.envoyAgent.DrainNow()
}
