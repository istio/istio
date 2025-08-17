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

package bootstrap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	grpcprom "github.com/grpc-ecosystem/go-grpc-prometheus"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/controllers/ipallocate"
	"istio.io/istio/pilot/pkg/controllers/untaint"
	kubecredentials "istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/features"
	istiogrpc "istio.io/istio/pilot/pkg/grpc"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	sec_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/server"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pilot/pkg/status"
	tb "istio.io/istio/pilot/pkg/trustbundle"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/h2c"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	xdspkg "istio.io/istio/pkg/xds"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/ra"
	caserver "istio.io/istio/security/pkg/server/ca"
	"istio.io/istio/security/pkg/server/ca/authenticate"
	"istio.io/istio/security/pkg/server/ca/authenticate/kubeauth"
)

const (
	// debounce file watcher events to minimize noise in logs
	watchDebounceDelay = 100 * time.Millisecond
)

func init() {
	// Disable gRPC tracing. It has performance impacts (See https://github.com/grpc/grpc-go/issues/695)
	grpc.EnableTracing = false
}

// readinessProbe defines a function that will be used indicate whether a server is ready.
type readinessProbe func() bool

// Server contains the runtime configuration for the Pilot discovery service.
type Server struct {
	XDSServer *xds.DiscoveryServer

	clusterID   cluster.ID
	environment *model.Environment

	kubeClient kubelib.Client

	multiclusterController *multicluster.Controller

	configController       model.ConfigStoreController
	ConfigStores           []model.ConfigStoreController
	serviceEntryController *serviceentry.Controller

	httpServer  *http.Server // debug, monitoring and readiness Server.
	httpAddr    string
	httpsServer *http.Server // webhooks HTTPS Server.
	httpsAddr   string

	grpcServer        *grpc.Server
	grpcAddress       string
	secureGrpcServer  *grpc.Server
	secureGrpcAddress string

	// monitoringMux listens on monitoringAddr(:15014).
	// Currently runs prometheus monitoring and debug (if enabled).
	monitoringMux *http.ServeMux
	// internalDebugMux is a mux for *internal* calls to the debug interface. That is, authentication is disabled.
	internalDebugMux *http.ServeMux

	metricsExporter http.Handler

	// httpMux listens on the httpAddr (8080).
	// If a Gateway is used in front and https is off it is also multiplexing
	// the rest of the features if their port is empty.
	// Currently runs readiness and debug (if enabled)
	httpMux *http.ServeMux

	// httpsMux listens on the httpsAddr(15017), handling webhooks
	// If the address os empty, the webhooks will be set on the default httpPort.
	httpsMux *http.ServeMux // webhooks

	// fileWatcher used to watch mesh config, networks and certificates.
	fileWatcher filewatcher.FileWatcher

	// certWatcher watches the certificates for changes and triggers a notification to Istiod.
	cacertsWatcher *fsnotify.Watcher
	dnsNames       []string

	CA       *ca.IstioCA
	RA       ra.RegistrationAuthority
	caServer *caserver.Server

	// TrustAnchors for workload to workload mTLS and proxy to istiod TLS
	// Only initiated when `ISTIO_MULTIROOT_MESH` = true
	workloadTrustBundle *tb.TrustBundle
	certMu              sync.RWMutex
	istiodCert          *tls.Certificate

	// istiodCertBundleWatche provides callbacks when the Istiod certs or roots are changed.
	// The roots are used by the namespace controller to update Istiod roots and patch webhooks.
	// The certs are used to refresh Istiod credentials.
	istiodCertBundleWatcher *keycertbundle.Watcher
	server                  server.Instance

	readinessProbes map[string]readinessProbe
	readinessFlags  *readinessFlags

	// duration used for graceful shutdown.
	shutdownDuration time.Duration

	// internalStop is closed when the server is shutdown. This should be avoided as much as possible, in
	// favor of AddStartFunc. This is only required if we *must* start something outside of this process.
	// For example, everything depends on mesh config, so we use it there rather than trying to sequence everything
	// in AddStartFunc
	internalStop chan struct{}

	webhookInfo *webhookInfo

	statusManager *status.Manager
	// RWConfigStore is the configstore which allows updates, particularly for status.
	RWConfigStore model.ConfigStoreController

	krtDebugger *krt.DebugHandler
}

type readinessFlags struct {
	sidecarInjectorReady  atomic.Bool
	configValidationReady atomic.Bool
}

type webhookInfo struct {
	mu sync.RWMutex
	wh *inject.Webhook
}

func (w *webhookInfo) GetTemplates() map[string]string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.wh != nil {
		return w.wh.Config.RawTemplates
	}
	return map[string]string{}
}

func (w *webhookInfo) getWebhookConfig() inject.WebhookConfig {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.wh != nil {
		return w.wh.GetConfig()
	}
	return inject.WebhookConfig{}
}

func (w *webhookInfo) addHandler(fn func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.wh != nil {
		w.wh.MultiCast.AddHandler(func(c *inject.Config, s string) error {
			fn()
			return nil
		})
	}
}

// NewServer creates a new Server instance based on the provided arguments.
func NewServer(args *PilotArgs, initFuncs ...func(*Server)) (*Server, error) {
	e := model.NewEnvironment()
	e.DomainSuffix = args.RegistryOptions.KubeOptions.DomainSuffix

	ac := aggregate.NewController(aggregate.Options{
		MeshHolder:      e,
		ConfigClusterID: getClusterID(args),
	})
	e.ServiceDiscovery = ac

	exporter, err := monitoring.RegisterPrometheusExporter(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("could not set up prometheus exporter: %v", err)
	}
	s := &Server{
		clusterID:               getClusterID(args),
		environment:             e,
		fileWatcher:             filewatcher.NewWatcher(),
		httpMux:                 http.NewServeMux(),
		monitoringMux:           http.NewServeMux(),
		readinessProbes:         make(map[string]readinessProbe),
		readinessFlags:          &readinessFlags{},
		server:                  server.New(),
		shutdownDuration:        args.ShutdownDuration,
		internalStop:            make(chan struct{}),
		istiodCertBundleWatcher: keycertbundle.NewWatcher(),
		webhookInfo:             &webhookInfo{},
		metricsExporter:         exporter,
		krtDebugger:             args.KrtDebugger,
	}

	// Apply custom initialization functions.
	for _, fn := range initFuncs {
		fn(s)
	}
	// Initialize workload Trust Bundle before XDS Server
	s.XDSServer = xds.NewDiscoveryServer(e, args.RegistryOptions.KubeOptions.ClusterAliases, args.KrtDebugger)
	configGen := core.NewConfigGenerator(s.XDSServer.Cache)

	grpcprom.EnableHandlingTimeHistogram()

	// make sure we have a readiness probe before serving HTTP to avoid marking ready too soon
	s.initReadinessProbes()

	s.initServers(args)
	if err := s.initIstiodAdminServer(args, s.webhookInfo.GetTemplates); err != nil {
		return nil, fmt.Errorf("error initializing debug server: %v", err)
	}
	if err := s.serveHTTP(); err != nil {
		return nil, fmt.Errorf("error serving http: %v", err)
	}

	// Apply the arguments to the configuration.
	if err := s.initKubeClient(args); err != nil {
		return nil, fmt.Errorf("error initializing kube client: %v", err)
	}

	s.initMeshConfiguration(args, s.fileWatcher)
	// Setup Kubernetes watch filters
	// Because this relies on meshconfig, it needs to be outside initKubeClient
	if s.kubeClient != nil {
		// Build a namespace watcher. This must have no filter, since this is our input to the filter itself.
		namespaces := kclient.New[*corev1.Namespace](s.kubeClient)
		filter := namespace.NewDiscoveryNamespacesFilter(namespaces, s.environment.Watcher, s.internalStop)
		s.kubeClient = kubelib.SetObjectFilter(s.kubeClient, filter)
	}

	s.initMeshNetworks(args, s.fileWatcher)
	s.initMeshHandlers(configGen.MeshConfigChanged)
	s.environment.Init()
	if err := s.environment.InitNetworksManager(s.XDSServer); err != nil {
		return nil, err
	}

	if features.MultiRootMesh {
		// Initialize trust bundle after mesh config which it depends on
		s.workloadTrustBundle = tb.NewTrustBundle(nil, e.Watcher)
		e.TrustBundle = s.workloadTrustBundle
	}

	// Options based on the current 'defaults' in istio.
	caOpts := &caOptions{
		TrustDomain:      s.environment.Mesh().TrustDomain,
		Namespace:        args.Namespace,
		ExternalCAType:   ra.CaExternalType(externalCaType),
		CertSignerDomain: features.CertSignerDomain,
	}

	if caOpts.ExternalCAType == ra.ExtCAK8s {
		// Older environment variable preserved for backward compatibility
		caOpts.ExternalCASigner = k8sSigner
	}
	// CA signing certificate must be created first if needed.
	if err := s.maybeCreateCA(caOpts); err != nil {
		return nil, err
	}

	if err := s.initControllers(args); err != nil {
		return nil, err
	}

	InitGenerators(s.XDSServer, configGen, args.Namespace, s.clusterID, s.internalDebugMux)

	// Initialize workloadTrustBundle after CA has been initialized
	if err := s.initWorkloadTrustBundle(args); err != nil {
		return nil, err
	}

	// Parse and validate Istiod Address.
	istiodHost, _, err := e.GetDiscoveryAddress()
	if err != nil {
		return nil, err
	}

	// Create Istiod certs and setup watches.
	if err := s.initIstiodCerts(args, string(istiodHost)); err != nil {
		return nil, err
	}

	// Secure gRPC Server must be initialized after CA is created as may use a Citadel generated cert.
	if err := s.initSecureDiscoveryService(args, s.environment.Mesh().GetTrustDomain()); err != nil {
		return nil, fmt.Errorf("error initializing secure gRPC Listener: %v", err)
	}

	// common https server for webhooks (e.g. injection, validation)
	if s.kubeClient != nil {
		s.initSecureWebhookServer(args)
		wh, err := s.initSidecarInjector(args)
		if err != nil {
			return nil, fmt.Errorf("error initializing sidecar injector: %v", err)
		}
		s.readinessFlags.sidecarInjectorReady.Store(true)
		s.webhookInfo.mu.Lock()
		s.webhookInfo.wh = wh
		s.webhookInfo.mu.Unlock()
		if err := s.initConfigValidation(args); err != nil {
			return nil, fmt.Errorf("error initializing config validator: %v", err)
		}
	}

	// This should be called only after controllers are initialized.
	s.initRegistryEventHandlers()

	s.initDiscoveryService()

	// Notice that the order of authenticators matters, since at runtime
	// authenticators are activated sequentially and the first successful attempt
	// is used as the authentication result.
	authenticators := []security.Authenticator{
		&authenticate.ClientCertAuthenticator{},
	}
	if args.JwtRule != "" {
		jwtAuthn, err := initOIDC(args, s.environment.Watcher)
		if err != nil {
			return nil, fmt.Errorf("error initializing OIDC: %v", err)
		}
		if jwtAuthn == nil {
			return nil, fmt.Errorf("JWT authenticator is nil")
		}
		authenticators = append(authenticators, jwtAuthn)
	}
	// The k8s JWT authenticator requires the multicluster registry to be initialized,
	// so we build it later.
	if s.kubeClient != nil {
		authenticators = append(authenticators,
			kubeauth.NewKubeJWTAuthenticator(
				s.environment.Watcher,
				s.kubeClient.Kube(),
				s.clusterID,
				args.RegistryOptions.KubeOptions.ClusterAliases,
				s.multiclusterController))
	}
	if len(features.TrustedGatewayCIDR) > 0 {
		authenticators = append(authenticators, &authenticate.XfccAuthenticator{})
	}
	if features.XDSAuth {
		s.XDSServer.Authenticators = authenticators
	}
	caOpts.Authenticators = authenticators

	// Start CA or RA server. This should be called after CA and Istiod certs have been created.
	s.startCA(caOpts)

	// TODO: don't run this if galley is started, one ctlz is enough
	if args.CtrlZOptions != nil {
		_, _ = ctrlz.Run(args.CtrlZOptions, nil)
	}

	// This must be last, otherwise we will not know which informers to register
	if s.kubeClient != nil {
		s.addStartFunc("kube client", func(stop <-chan struct{}) error {
			s.kubeClient.RunAndWait(stop)
			return nil
		})
	}

	return s, nil
}

func initOIDC(args *PilotArgs, meshWatcher mesh.Watcher) (security.Authenticator, error) {
	// JWTRule is from the JWT_RULE environment variable.
	// An example of json string for JWTRule is:
	// `{"issuer": "foo", "jwks_uri": "baz", "audiences": ["aud1", "aud2"]}`.
	jwtRule := &v1beta1.JWTRule{}
	err := json.Unmarshal([]byte(args.JwtRule), jwtRule)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JWT rule: %v", err)
	}
	log.Infof("Istiod authenticating using JWTRule: %v", jwtRule)
	jwtAuthn, err := authenticate.NewJwtAuthenticator(jwtRule, meshWatcher)
	if err != nil {
		return nil, fmt.Errorf("failed to create the JWT authenticator: %v", err)
	}
	return jwtAuthn, nil
}

func getClusterID(args *PilotArgs) cluster.ID {
	clusterID := args.RegistryOptions.KubeOptions.ClusterID
	if clusterID == "" {
		if hasKubeRegistry(args.RegistryOptions.Registries) {
			clusterID = cluster.ID(provider.Kubernetes)
		}
	}
	return clusterID
}

// Start starts all components of the error serving tap http serverPilot discovery service on the port specified in DiscoveryServerOptions.
// If Port == 0, a port number is automatically chosen. Content serving is started by this method,
// but is executed asynchronously. Serving can be canceled at any time by closing the provided stop channel.
func (s *Server) Start(stop <-chan struct{}) error {
	log.Infof("Starting Istiod Server with primary cluster %s", s.clusterID)

	if features.UnsafeFeaturesEnabled() {
		log.Warn("Server is starting with unsafe features enabled")
	}

	// Now start all of the components.
	if err := s.server.Start(stop); err != nil {
		return err
	}
	if !s.waitForCacheSync(stop) {
		return fmt.Errorf("failed to sync cache")
	}
	// Inform Discovery Server so that it can start accepting connections.
	s.XDSServer.CachesSynced()

	// Race condition - if waitForCache is too fast and we run this as a startup function,
	// the grpc server would be started before CA is registered. Listening should be last.
	if s.secureGrpcAddress != "" {
		grpcListener, err := net.Listen("tcp", s.secureGrpcAddress)
		if err != nil {
			return err
		}
		go func() {
			log.Infof("starting secure gRPC discovery service at %s", grpcListener.Addr())
			if err := s.secureGrpcServer.Serve(grpcListener); err != nil {
				log.Errorf("error serving secure GRPC server: %v", err)
			}
		}()
	}

	if s.grpcAddress != "" {
		grpcListener, err := net.Listen("tcp", s.grpcAddress)
		if err != nil {
			return err
		}
		go func() {
			log.Infof("starting gRPC discovery service at %s", grpcListener.Addr())
			if err := s.grpcServer.Serve(grpcListener); err != nil {
				log.Errorf("error serving GRPC server: %v", err)
			}
		}()
	}

	if s.httpsServer != nil {
		httpsListener, err := net.Listen("tcp", s.httpsServer.Addr)
		if err != nil {
			return err
		}
		go func() {
			log.Infof("starting webhook service at %s", httpsListener.Addr())
			if err := s.httpsServer.ServeTLS(httpsListener, "", ""); network.IsUnexpectedListenerError(err) {
				log.Errorf("error serving https server: %v", err)
			}
		}()
		s.httpsAddr = httpsListener.Addr().String()
	}

	s.waitForShutdown(stop)

	return nil
}

// WaitUntilCompletion waits for everything marked as a "required termination" to complete.
// This should be called before exiting.
func (s *Server) WaitUntilCompletion() {
	s.server.Wait()
}

// initSDSServer starts the SDS server
func (s *Server) initSDSServer() {
	if s.kubeClient == nil {
		return
	}
	if !features.EnableXDSIdentityCheck {
		// Make sure we have security
		log.Warnf("skipping Kubernetes credential reader; PILOT_ENABLE_XDS_IDENTITY_CHECK must be set to true for this feature.")
	} else {
		creds := kubecredentials.NewMulticluster(s.clusterID, s.multiclusterController)
		creds.AddSecretHandler(func(k kind.Kind, name string, namespace string) {
			s.XDSServer.ConfigUpdate(&model.PushRequest{
				Full:           false,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: k, Name: name, Namespace: namespace}),
				Reason:         model.NewReasonStats(model.SecretTrigger),
			})
		})
		s.environment.CredentialsController = creds
	}
}

// initKubeClient creates the k8s client if running in a k8s environment.
// This is determined by the presence of a kube registry, which
// uses in-context k8s, or a config source of type k8s.
func (s *Server) initKubeClient(args *PilotArgs) error {
	if s.kubeClient != nil {
		// Already initialized by startup arguments
		return nil
	}
	hasK8SConfigStore := false
	if args.RegistryOptions.FileDir == "" {
		// If file dir is set - config controller will just use file.
		if _, err := os.Stat(args.MeshConfigFile); !os.IsNotExist(err) {
			meshConfig, err := mesh.ReadMeshConfig(args.MeshConfigFile)
			if err != nil {
				return fmt.Errorf("failed reading mesh config: %v", err)
			}
			if len(meshConfig.ConfigSources) == 0 && args.RegistryOptions.KubeConfig != "" {
				hasK8SConfigStore = true
			}
			for _, cs := range meshConfig.ConfigSources {
				if cs.Address == string(Kubernetes)+"://" {
					hasK8SConfigStore = true
					break
				}
			}
		} else if args.RegistryOptions.KubeConfig != "" {
			hasK8SConfigStore = true
		}
	}

	if hasK8SConfigStore || hasKubeRegistry(args.RegistryOptions.Registries) {
		// Used by validation
		kubeRestConfig, err := kubelib.DefaultRestConfig(args.RegistryOptions.KubeConfig, "", func(config *rest.Config) {
			config.QPS = args.RegistryOptions.KubeOptions.KubernetesAPIQPS
			config.Burst = args.RegistryOptions.KubeOptions.KubernetesAPIBurst
		})
		if err != nil {
			return fmt.Errorf("failed creating kube config: %v", err)
		}

		s.kubeClient, err = kubelib.NewClient(kubelib.NewClientConfigForRestConfig(kubeRestConfig), s.clusterID)
		if err != nil {
			return fmt.Errorf("failed creating kube client: %v", err)
		}
		s.kubeClient = kubelib.EnableCrdWatcher(s.kubeClient)
	}

	return nil
}

// A single container can't have two readiness probes. Make this readiness probe a generic one
// that can handle all istiod related readiness checks including webhook, gRPC etc.
// The "http" portion of the readiness check is satisfied by the fact we've started listening on
// this handler and everything has already initialized.
func (s *Server) istiodReadyHandler(w http.ResponseWriter, _ *http.Request) {
	for name, fn := range s.readinessProbes {
		if ready := fn(); !ready {
			log.Warnf("%s is not ready", name)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

// initServers initializes http and grpc servers
func (s *Server) initServers(args *PilotArgs) {
	s.initGrpcServer(args.KeepaliveOptions)
	multiplexGRPC := false
	if args.ServerOptions.GRPCAddr != "" {
		s.grpcAddress = args.ServerOptions.GRPCAddr
	} else {
		// This happens only if the GRPC port (15010) is disabled. We will multiplex
		// it on the HTTP port. Does not impact the HTTPS gRPC or HTTPS.
		log.Infof("multiplexing gRPC on http addr %v", args.ServerOptions.HTTPAddr)
		multiplexGRPC = true
	}
	h2s := &http2.Server{
		MaxConcurrentStreams: uint32(features.MaxConcurrentStreams),
	}
	multiplexHandler := h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If we detect gRPC, serve using grpcServer
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("content-type"), "application/grpc") {
			s.grpcServer.ServeHTTP(w, r)
			return
		}
		// Otherwise, this is meant for the standard HTTP server
		s.httpMux.ServeHTTP(w, r)
	}), h2s)
	s.httpServer = &http.Server{
		Addr:        args.ServerOptions.HTTPAddr,
		Handler:     s.httpMux,
		IdleTimeout: 90 * time.Second, // matches http.DefaultTransport keep-alive timeout
		ReadTimeout: 30 * time.Second,
	}
	if multiplexGRPC {
		// To allow the gRPC handler to make per-request decision,
		// use ReadHeaderTimeout instead of ReadTimeout.
		s.httpServer.ReadTimeout = 0
		s.httpServer.ReadHeaderTimeout = 30 * time.Second
		s.httpServer.Handler = multiplexHandler
	}

	if args.ServerOptions.MonitoringAddr == "" {
		s.monitoringMux = s.httpMux
		log.Infof("initializing Istiod admin server multiplexed on httpAddr %v", s.httpServer.Addr)
	} else {
		log.Info("initializing Istiod admin server")
	}
}

// initIstiodAdminServer initializes monitoring, debug and readiness end points.
func (s *Server) initIstiodAdminServer(args *PilotArgs, whc func() map[string]string) error {
	// Debug Server.
	internalMux := s.XDSServer.InitDebug(s.monitoringMux, args.ServerOptions.EnableProfiling, whc)
	s.internalDebugMux = internalMux

	// Debug handlers are currently added on monitoring mux and readiness mux.
	// If monitoring addr is empty, the mux is shared and we only add it once on the shared mux .
	if args.ServerOptions.MonitoringAddr != "" {
		s.XDSServer.AddDebugHandlers(s.httpMux, nil, args.ServerOptions.EnableProfiling, whc)
	}

	// Monitoring Server.
	if err := s.initMonitor(args.ServerOptions.MonitoringAddr); err != nil {
		return fmt.Errorf("error initializing monitor: %v", err)
	}

	// Readiness Handler.
	s.httpMux.HandleFunc("/ready", s.istiodReadyHandler)

	return nil
}

// initDiscoveryService initializes discovery server on plain text port.
func (s *Server) initDiscoveryService() {
	log.Infof("starting discovery service")
	// Implement EnvoyXdsServer grace shutdown
	s.addStartFunc("xds server", func(stop <-chan struct{}) error {
		log.Infof("Starting ADS server")
		s.XDSServer.Start(stop)
		return nil
	})
}

// Wait for the stop, and do cleanups
func (s *Server) waitForShutdown(stop <-chan struct{}) {
	go func() {
		<-stop
		close(s.internalStop)
		_ = s.fileWatcher.Close()

		if s.cacertsWatcher != nil {
			_ = s.cacertsWatcher.Close()
		}
		// Stop gRPC services.  If gRPC services fail to stop in the shutdown duration,
		// force stop them. This does not happen normally.
		stopped := make(chan struct{})
		go func() {
			// Some grpcServer implementations do not support GracefulStop. Unfortunately, this is not
			// exposed; they just panic. To avoid this, we will recover and do a standard Stop when its not
			// support.
			defer func() {
				if r := recover(); r != nil {
					s.grpcServer.Stop()
					if s.secureGrpcServer != nil {
						s.secureGrpcServer.Stop()
					}
					close(stopped)
				}
			}()
			s.grpcServer.GracefulStop()
			if s.secureGrpcServer != nil {
				s.secureGrpcServer.GracefulStop()
			}
			close(stopped)
		}()

		t := time.NewTimer(s.shutdownDuration)
		select {
		case <-t.C:
			s.grpcServer.Stop()
			if s.secureGrpcServer != nil {
				s.secureGrpcServer.Stop()
			}
		case <-stopped:
			t.Stop()
		}

		// Stop HTTP services.
		ctx, cancel := context.WithTimeout(context.Background(), s.shutdownDuration)
		defer cancel()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			log.Warn(err)
		}
		if s.httpsServer != nil {
			if err := s.httpsServer.Shutdown(ctx); err != nil {
				log.Warn(err)
			}
		}

		// Shutdown the DiscoveryServer.
		s.XDSServer.Shutdown()
	}()
}

func (s *Server) initGrpcServer(options *istiokeepalive.Options) {
	interceptors := []grpc.UnaryServerInterceptor{
		// setup server prometheus monitoring (as final interceptor in chain)
		grpcprom.UnaryServerInterceptor,
	}
	grpcOptions := istiogrpc.ServerOptions(options, xdspkg.RecordRecvSize, interceptors...)
	s.grpcServer = grpc.NewServer(grpcOptions...)
	s.XDSServer.Register(s.grpcServer)
	reflection.Register(s.grpcServer)
}

// initialize secureGRPCServer.
func (s *Server) initSecureDiscoveryService(args *PilotArgs, trustDomain string) error {
	if args.ServerOptions.SecureGRPCAddr == "" {
		log.Info("The secure discovery port is disabled, multiplexing on httpAddr ")
		return nil
	}

	peerCertVerifier, err := s.createPeerCertVerifier(args.ServerOptions.TLSOptions, trustDomain)
	if err != nil {
		return err
	}
	if peerCertVerifier == nil {
		// Running locally without configured certs - no TLS mode
		log.Warnf("The secure discovery service is disabled")
		return nil
	}
	log.Info("initializing secure discovery service")

	cfg := &tls.Config{
		GetCertificate: s.getIstiodCertificate,
		ClientAuth:     tls.VerifyClientCertIfGiven,
		ClientCAs:      peerCertVerifier.GetGeneralCertPool(),
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			err := peerCertVerifier.VerifyPeerCert(rawCerts, verifiedChains)
			if err != nil {
				log.Infof("Could not verify certificate: %v", err)
			}
			return err
		},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: args.ServerOptions.TLSOptions.CipherSuits,
	}
	// Compliance for xDS server TLS.
	sec_model.EnforceGoCompliance(cfg)

	tlsCreds := credentials.NewTLS(cfg)

	s.secureGrpcAddress = args.ServerOptions.SecureGRPCAddr

	interceptors := []grpc.UnaryServerInterceptor{
		// setup server prometheus monitoring (as final interceptor in chain)
		grpcprom.UnaryServerInterceptor,
	}
	opts := istiogrpc.ServerOptions(args.KeepaliveOptions, xdspkg.RecordRecvSize, interceptors...)
	opts = append(opts, grpc.Creds(tlsCreds))

	s.secureGrpcServer = grpc.NewServer(opts...)
	s.XDSServer.Register(s.secureGrpcServer)
	reflection.Register(s.secureGrpcServer)

	s.addStartFunc("secure gRPC", func(stop <-chan struct{}) error {
		go func() {
			<-stop
			s.secureGrpcServer.Stop()
		}()
		return nil
	})

	return nil
}

// addStartFunc appends a function to be run. These are run synchronously in order,
// so the function should start a go routine if it needs to do anything blocking
func (s *Server) addStartFunc(name string, fn server.Component) {
	s.server.RunComponent(name, fn)
}

// adds a readiness probe for Istiod Server.
func (s *Server) addReadinessProbe(name string, fn readinessProbe) {
	s.readinessProbes[name] = fn
}

// addTerminatingStartFunc adds a function that should terminate before the serve shuts down
// This is useful to do cleanup activities
// This is does not guarantee they will terminate gracefully - best effort only
// Function should be synchronous; once it returns it is considered "done"
func (s *Server) addTerminatingStartFunc(name string, fn server.Component) {
	s.server.RunComponentAsyncAndWait(name, fn)
}

func (s *Server) waitForCacheSync(stop <-chan struct{}) bool {
	start := time.Now()
	log.Info("Waiting for caches to be synced")
	if !kubelib.WaitForCacheSync("server", stop, s.cachesSynced) {
		log.Errorf("Failed waiting for cache sync")
		return false
	}
	log.Infof("All controller caches have been synced up in %v", time.Since(start))

	// At this point, we know that all update events of the initial state-of-the-world have been
	// received. We wait to ensure we have committed at least this many updates. This avoids a race
	// condition where we are marked ready prior to updating the push context, leading to incomplete
	// pushes.
	expected := s.XDSServer.InboundUpdates.Load()
	return kubelib.WaitForCacheSync("push context", stop, func() bool { return s.pushContextReady(expected) })
}

// pushContextReady indicates whether pushcontext has processed all inbound config updates.
func (s *Server) pushContextReady(expected int64) bool {
	committed := s.XDSServer.CommittedUpdates.Load()
	if committed < expected {
		log.Debugf("Waiting for pushcontext to process inbound updates, inbound: %v, committed : %v", expected, committed)
		return false
	}
	return true
}

// cachesSynced checks whether caches have been synced.
func (s *Server) cachesSynced() bool {
	if s.multiclusterController != nil && !s.multiclusterController.HasSynced() {
		return false
	}
	if !s.ServiceController().HasSynced() {
		return false
	}
	if !s.configController.HasSynced() {
		return false
	}
	return true
}

// initRegistryEventHandlers sets up event handlers for config and service updates
func (s *Server) initRegistryEventHandlers() {
	log.Info("initializing registry event handlers")
	// Flush cached discovery responses whenever services configuration change.
	serviceHandler := func(prev, curr *model.Service, event model.Event) {
		pushReq := &model.PushRequest{
			Full:           true,
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: string(curr.Hostname), Namespace: curr.Attributes.Namespace}),
			Reason:         model.NewReasonStats(model.ServiceUpdate),
		}
		s.XDSServer.ConfigUpdate(pushReq)
	}
	s.ServiceController().AppendServiceHandler(serviceHandler)

	if s.configController != nil {
		configHandler := func(prev config.Config, curr config.Config, event model.Event) {
			log.Debugf("Handle event %s for configuration %s", event, curr.Key())
			// For update events, trigger push only if spec has changed.
			if event == model.EventUpdate && !needsPush(prev, curr) {
				log.Debugf("skipping push for %s as spec has not changed", prev.Key())
				return
			}
			pushReq := &model.PushRequest{
				Full:           true,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: gvk.MustToKind(curr.GroupVersionKind), Name: curr.Name, Namespace: curr.Namespace}),
				Reason:         model.NewReasonStats(model.ConfigUpdate),
			}
			s.XDSServer.ConfigUpdate(pushReq)
		}
		schemas := collections.Pilot.All()
		if features.EnableGatewayAPI {
			schemas = collections.PilotGatewayAPI().All()
		}
		for _, schema := range schemas {
			// This resource type was handled in external/servicediscovery.go, no need to rehandle here.
			if schema.GroupVersionKind() == gvk.ServiceEntry {
				continue
			}
			if schema.GroupVersionKind() == gvk.WorkloadEntry {
				continue
			}
			if schema.GroupVersionKind() == gvk.WorkloadGroup {
				continue
			}
			// Already handled by gateway controller
			if schema.GroupVersionKind().Group == gvk.KubernetesGateway.Group {
				continue
			}

			s.configController.RegisterEventHandler(schema.GroupVersionKind(), configHandler)
		}
	}
}

// initIstiodCertLoader will make sure istiodCertBundleWatcher is updating
// the certs and updates Server.istiodCert - which is returned on all TLS requests.
func (s *Server) initIstiodCertLoader() error {
	if err := s.loadIstiodCert(); err != nil {
		return fmt.Errorf("first time load IstiodCert failed: %v", err)
	}
	_, watchCh := s.istiodCertBundleWatcher.AddWatcher()
	s.addStartFunc("reload certs", func(stop <-chan struct{}) error {
		go s.reloadIstiodCert(watchCh, stop)
		return nil
	})
	return nil
}

// initIstiodCerts creates Istiod certificates to be used by gRPC and webhook TLS servers
// and also sets up watches to them. It also detects and sets the root certificates
// for replication.
//
// This will also detect the root CAs (mesh trust) and set it up for Istiod as well as
// namespace replication, if the controller is enabled.
//
// Will prefer local certificates, and fallback to using the CA to sign a fresh, temporary
// certificate.
func (s *Server) initIstiodCerts(args *PilotArgs, host string) error {
	// Skip all certificates
	var err error

	s.dnsNames = getDNSNames(args, host)
	if hasCustomCertArgsOrWellKnown, tlsCertPath, tlsKeyPath, caCertPath := hasCustomTLSCerts(args.ServerOptions.TLSOptions); hasCustomCertArgsOrWellKnown {
		// Use the DNS certificate provided via args or in well known location.
		err = s.initFileCertificateWatches(TLSOptions{
			CaCertFile: caCertPath,
			KeyFile:    tlsKeyPath,
			CertFile:   tlsCertPath,
		})
		if err != nil {
			// Not crashing istiod - This typically happens if certs are missing and in tests.
			log.Errorf("error initializing certificate watches: %v", err)
			return nil
		}
	} else if features.EnableCAServer && features.PilotCertProvider == constants.CertProviderIstiod {
		log.Infof("initializing Istiod DNS certificates host: %s, custom host: %s", host, features.IstiodServiceCustomHost)
		err = s.initDNSCertsIstiod()
	} else if features.PilotCertProvider == constants.CertProviderKubernetes {
		// This will not work - better to fail Istiod startup so it can be detected early.
		// Unlike other failures that we can recover from, this has no mitigation, user must
		// choose a different source.
		// The feature didn't work for few releases, but a skip-version upgrade may still
		// encounter it.
		log.Fatal("PILOT_CERT_PROVIDER=kubernetes is no longer supported by upstream K8S")
	} else if strings.HasPrefix(features.PilotCertProvider, constants.CertProviderKubernetesSignerPrefix) {
		log.Infof("initializing Istiod DNS certificates using K8S RA:%s  host: %s, custom host: %s", features.PilotCertProvider,
			host, features.IstiodServiceCustomHost)
		err = s.initDNSCertsK8SRA()
	} else {
		log.Warnf("PILOT_CERT_PROVIDER=%s is not implemented", features.PilotCertProvider)

		// In Istio 1.22 - we return nil here - the old code in s.initDNSCerts used to have
		// an 'else' to handle the unknown providers by not initializing the TLS certs but
		// still seting the root from /etc/certs/root-cert.pem for distribution in the
		// namespace controller.
		// The new behavior appears safer - IMO we may also do a fatal unless provider is
		// set to "none" because it is not clear what the user intends.

		// Skip invoking initIstiodCertLoader - we have no cert.
		return nil
	}

	if err == nil {
		err = s.initIstiodCertLoader()
	}

	return err
}

func getDNSNames(args *PilotArgs, host string) []string {
	// Append custom hostname if there is any
	customHost := features.IstiodServiceCustomHost
	var cHosts []string

	if customHost != "" {
		cHosts = strings.Split(customHost, ",")
	}
	sans := sets.New(cHosts...)
	sans.Insert(host)
	// The first is the recommended one, also used by Apiserver for webhooks.
	// add a few known hostnames
	knownHosts := []string{"istiod", "istiod-remote", "istio-pilot"}
	// In some conditions, pilot address for sds is different from other xds,
	// like multi-cluster primary-remote mode with revision.
	if args.Revision != "" && args.Revision != "default" {
		knownHosts = append(knownHosts, "istiod"+"-"+args.Revision)
	}
	knownSans := make([]string, 0, 2*len(knownHosts))
	for _, altName := range knownHosts {
		knownSans = append(knownSans,
			fmt.Sprintf("%s.%s.svc", altName, args.Namespace))
	}
	sans.InsertAll(knownSans...)
	dnsNames := sets.SortedList(sans)
	log.Infof("Discover server subject alt names: %v", dnsNames)
	return dnsNames
}

// createPeerCertVerifier creates a SPIFFE certificate verifier with the current istiod configuration.
func (s *Server) createPeerCertVerifier(tlsOptions TLSOptions, trustDomain string) (*spiffe.PeerCertVerifier, error) {
	customTLSCertsExists, _, _, caCertPath := hasCustomTLSCerts(tlsOptions)
	if !customTLSCertsExists && s.CA == nil && !s.isK8SSigning() {
		// Running locally without configured certs - no TLS mode
		return nil, nil
	}
	peerCertVerifier := spiffe.NewPeerCertVerifier()
	var rootCertBytes []byte
	var err error
	if caCertPath != "" {
		if rootCertBytes, err = os.ReadFile(caCertPath); err != nil {
			return nil, err
		}
	} else {
		if s.RA != nil {
			if strings.HasPrefix(features.PilotCertProvider, constants.CertProviderKubernetesSignerPrefix) {
				signerName := strings.TrimPrefix(features.PilotCertProvider, constants.CertProviderKubernetesSignerPrefix)
				caBundle, _ := s.RA.GetRootCertFromMeshConfig(signerName)
				rootCertBytes = append(rootCertBytes, caBundle...)
			} else {
				rootCertBytes = append(rootCertBytes, s.RA.GetCAKeyCertBundle().GetRootCertPem()...)
			}
		}
		if s.CA != nil {
			rootCertBytes = append(rootCertBytes, s.CA.GetCAKeyCertBundle().GetRootCertPem()...)
		}
	}

	if len(rootCertBytes) != 0 {
		// TODO: trustDomain here is static and will not update if it dynamically changes in mesh config
		err := peerCertVerifier.AddMappingFromPEM(trustDomain, rootCertBytes)
		if err != nil {
			return nil, fmt.Errorf("add root CAs into peerCertVerifier failed: %v", err)
		}
	}

	return peerCertVerifier, nil
}

func checkPathsExist(paths ...string) bool {
	for _, path := range paths {
		fInfo, err := os.Stat(path)

		if err != nil || fInfo.IsDir() {
			return false
		}
	}
	return true
}

// hasCustomTLSCerts returns the tls cert paths, used both if custom TLS certificates are configured via args or by mounting in well known.
// while tls args should still take precedence the aim is to encourage loading the DNS tls cert in the well known path locations.
func hasCustomTLSCerts(tlsOptions TLSOptions) (ok bool, tlsCertPath, tlsKeyPath, caCertPath string) {
	// load from tls args as priority
	if hasCustomTLSCertArgs(tlsOptions) {
		return true, tlsOptions.CertFile, tlsOptions.KeyFile, tlsOptions.CaCertFile
	}

	if ok = checkPathsExist(constants.DefaultPilotTLSCert, constants.DefaultPilotTLSKey, constants.DefaultPilotTLSCaCert); ok {
		tlsCertPath = constants.DefaultPilotTLSCert
		tlsKeyPath = constants.DefaultPilotTLSKey
		caCertPath = constants.DefaultPilotTLSCaCert
		return
	}

	if ok = checkPathsExist(constants.DefaultPilotTLSCert, constants.DefaultPilotTLSKey, constants.DefaultPilotTLSCaCertAlternatePath); ok {
		tlsCertPath = constants.DefaultPilotTLSCert
		tlsKeyPath = constants.DefaultPilotTLSKey
		caCertPath = constants.DefaultPilotTLSCaCertAlternatePath
		return
	}

	return
}

// hasCustomTLSCerts returns true if custom TLS certificates are configured via args.
func hasCustomTLSCertArgs(tlsOptions TLSOptions) bool {
	return tlsOptions.CaCertFile != "" && tlsOptions.CertFile != "" && tlsOptions.KeyFile != ""
}

// getIstiodCertificate returns the istiod certificate, used in GetCertificate hook.
func (s *Server) getIstiodCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.certMu.RLock()
	defer s.certMu.RUnlock()
	if s.istiodCert != nil {
		return s.istiodCert, nil
	}
	return nil, fmt.Errorf("cert not initialized")
}

// initControllers initializes the controllers.
func (s *Server) initControllers(args *PilotArgs) error {
	log.Info("initializing controllers")
	s.initMulticluster(args)

	s.initSDSServer()

	if features.EnableNodeUntaintControllers {
		s.initNodeUntaintController(args)
	}

	if features.EnableIPAutoallocate {
		// validate the IP autoallocate CIDR prefixes for IPv4 and IPv6
		if _, err := netip.ParsePrefix(features.IPAutoallocateIPv4Prefix); err != nil {
			return fmt.Errorf("invalid IPv4 prefix %s: %v", features.IPAutoallocateIPv4Prefix, err)
		}
		if _, err := netip.ParsePrefix(features.IPAutoallocateIPv6Prefix); err != nil {
			return fmt.Errorf("invalid IPv6 prefix %s: %v", features.IPAutoallocateIPv6Prefix, err)
		}
		s.initIPAutoallocateController(args)
	}

	if err := s.initConfigController(args); err != nil {
		return fmt.Errorf("error initializing config controller: %v", err)
	}
	if err := s.initServiceControllers(args); err != nil {
		return fmt.Errorf("error initializing service controllers: %v", err)
	}
	return nil
}

func (s *Server) initNodeUntaintController(args *PilotArgs) {
	if s.kubeClient == nil {
		return
	}
	s.addStartFunc("nodeUntainter controller", func(stop <-chan struct{}) error {
		go leaderelection.
			NewLeaderElection(args.Namespace, args.PodName, leaderelection.NodeUntaintController, args.Revision, s.kubeClient).
			AddRunFunction(func(leaderStop <-chan struct{}) {
				nodeUntainter := untaint.NewNodeUntainter(leaderStop, s.kubeClient, args.CniNamespace, args.Namespace, args.KrtDebugger)
				nodeUntainter.Run(leaderStop)
			}).Run(stop)
		return nil
	})
}

func (s *Server) initIPAutoallocateController(args *PilotArgs) {
	if s.kubeClient == nil {
		return
	}
	s.addStartFunc("ip autoallocate controller", func(stop <-chan struct{}) error {
		go leaderelection.
			NewLeaderElection(args.Namespace, args.PodName, leaderelection.IPAutoallocateController, args.Revision, s.kubeClient).
			AddRunFunction(func(leaderStop <-chan struct{}) {
				ipallocate := ipallocate.NewIPAllocator(leaderStop, s.kubeClient)
				ipallocate.Run(leaderStop)
			}).Run(stop)
		return nil
	})
}

func (s *Server) initMulticluster(args *PilotArgs) {
	if s.kubeClient == nil {
		return
	}
	s.multiclusterController = multicluster.NewController(s.kubeClient, args.Namespace, s.clusterID, s.environment.Watcher, func(r *rest.Config) {
		r.QPS = args.RegistryOptions.KubeOptions.KubernetesAPIQPS
		r.Burst = args.RegistryOptions.KubeOptions.KubernetesAPIBurst
	})
	s.XDSServer.ListRemoteClusters = s.multiclusterController.ListRemoteClusters
	s.addStartFunc("multicluster controller", func(stop <-chan struct{}) error {
		return s.multiclusterController.Run(stop)
	})
}

// maybeCreateCA creates and initializes the built-in CA if needed.
func (s *Server) maybeCreateCA(caOpts *caOptions) error {
	// CA signing certificate must be created only if CA is enabled.
	if features.EnableCAServer {
		log.Info("creating CA and initializing public key")
		var err error
		if useRemoteCerts.Get() {
			if err = s.loadCACerts(caOpts, LocalCertDir.Get()); err != nil {
				return fmt.Errorf("failed to load remote CA certs: %v", err)
			}
		}
		// May return nil, if the CA is missing required configs - This is not an error.
		// This is currently only used for K8S signing.
		if caOpts.ExternalCAType != "" {
			if s.RA, err = s.createIstioRA(caOpts); err != nil {
				return fmt.Errorf("failed to create RA: %v", err)
			}
		}
		// If K8S signs - we don't need to use the built-in istio CA.
		if !s.isK8SSigning() {
			if s.CA, err = s.createIstioCA(caOpts); err != nil {
				return fmt.Errorf("failed to create CA: %v", err)
			}
		}
	}
	return nil
}

// Returns true to indicate the K8S multicluster controller should enable replication of
// root certificates to config maps in namespaces.
func (s *Server) shouldStartNsController() bool {
	if s.isK8SSigning() {
		// Need to distribute the roots from MeshConfig
		return true
	}
	if s.CA == nil {
		return false
	}

	// For no CA we don't distribute it either, as there is no cert
	if features.PilotCertProvider == constants.CertProviderNone {
		return false
	}

	return true
}

// StartCA starts the CA or RA server if configured.
func (s *Server) startCA(caOpts *caOptions) {
	if s.CA == nil && s.RA == nil {
		return
	}
	// init the RA server if configured, else start init CA server
	if s.RA != nil {
		log.Infof("initializing CA server with RA")
		s.initCAServer(s.RA, caOpts)
	} else if s.CA != nil {
		log.Infof("initializing CA server with IstioD CA")
		s.initCAServer(s.CA, caOpts)
	}
	s.addStartFunc("ca", func(stop <-chan struct{}) error {
		grpcServer := s.secureGrpcServer
		if s.secureGrpcServer == nil {
			grpcServer = s.grpcServer
		}
		log.Infof("starting CA server")
		s.RunCA(grpcServer)
		return nil
	})
}

// initMeshHandlers initializes mesh and network handlers.
func (s *Server) initMeshHandlers(changeHandler func(_ *meshconfig.MeshConfig)) {
	log.Info("initializing mesh handlers")
	// When the mesh config or networks change, do a full push.
	s.environment.AddMeshHandler(func() {
		changeHandler(s.environment.Mesh())
		s.XDSServer.ConfigUpdate(&model.PushRequest{
			Full:   true,
			Reason: model.NewReasonStats(model.GlobalUpdate),
			Forced: true,
		})
	})
}

func (s *Server) addIstioCAToTrustBundle(args *PilotArgs) error {
	var err error
	if s.CA != nil {
		// If IstioCA is setup, derive trustAnchor directly from CA
		rootCerts := []string{string(s.CA.GetCAKeyCertBundle().GetRootCertPem())}
		err = s.workloadTrustBundle.UpdateTrustAnchor(&tb.TrustAnchorUpdate{
			TrustAnchorConfig: tb.TrustAnchorConfig{Certs: rootCerts},
			Source:            tb.SourceIstioCA,
		})
		if err != nil {
			log.Errorf("unable to add CA root from namespace %s as trustAnchor", args.Namespace)
			return err
		}
		return nil
	}
	return nil
}

func (s *Server) initWorkloadTrustBundle(args *PilotArgs) error {
	var err error

	if !features.MultiRootMesh {
		return nil
	}

	s.workloadTrustBundle.UpdateCb(func() {
		pushReq := &model.PushRequest{
			Full:   true,
			Reason: model.NewReasonStats(model.GlobalUpdate),
			Forced: true,
		}
		s.XDSServer.ConfigUpdate(pushReq)
	})

	s.addStartFunc("remote trust anchors", func(stop <-chan struct{}) error {
		go s.workloadTrustBundle.ProcessRemoteTrustAnchors(stop, tb.RemoteDefaultPollPeriod)
		return nil
	})

	// MeshConfig: Add initial roots
	err = s.workloadTrustBundle.AddMeshConfigUpdate(s.environment.Mesh())
	if err != nil {
		return err
	}

	// MeshConfig:Add callback for mesh config update
	s.environment.AddMeshHandler(func() {
		_ = s.workloadTrustBundle.AddMeshConfigUpdate(s.environment.Mesh())
	})

	err = s.addIstioCAToTrustBundle(args)
	if err != nil {
		return err
	}

	// IstioRA: Explicitly add roots corresponding to RA
	if s.RA != nil {
		// Implicitly add the Istio RA certificates to the Workload Trust Bundle
		rootCerts := []string{string(s.RA.GetCAKeyCertBundle().GetRootCertPem())}
		err = s.workloadTrustBundle.UpdateTrustAnchor(&tb.TrustAnchorUpdate{
			TrustAnchorConfig: tb.TrustAnchorConfig{Certs: rootCerts},
			Source:            tb.SourceIstioRA,
		})
		if err != nil {
			log.Errorf("fatal: unable to add RA root as trustAnchor")
			return err
		}
	}
	log.Infof("done initializing workload trustBundle")
	return nil
}

// isK8SSigning returns whether K8S (as a RA) is used to sign certs instead of private keys known by Istiod
func (s *Server) isK8SSigning() bool {
	return s.RA != nil && strings.HasPrefix(features.PilotCertProvider, constants.CertProviderKubernetesSignerPrefix)
}

func (s *Server) initStatusManager(_ *PilotArgs) {
	s.addStartFunc("status manager", func(stop <-chan struct{}) error {
		s.statusManager = status.NewManager(s.RWConfigStore)
		s.statusManager.Start(stop)
		return nil
	})
}

func (s *Server) serveHTTP() error {
	// At this point we are ready - start Http Listener so that it can respond to readiness events.
	httpListener, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	go func() {
		log.Infof("starting HTTP service at %s", httpListener.Addr())
		if err := s.httpServer.Serve(httpListener); network.IsUnexpectedListenerError(err) {
			log.Errorf("error serving http server: %v", err)
		}
	}()
	s.httpAddr = httpListener.Addr().String()
	return nil
}

func (s *Server) initReadinessProbes() {
	probes := map[string]readinessProbe{
		"discovery": func() bool {
			return s.XDSServer.IsServerReady()
		},
		"sidecar injector": func() bool {
			return s.readinessFlags.sidecarInjectorReady.Load()
		},
		"config validation": func() bool {
			return s.readinessFlags.configValidationReady.Load()
		},
	}
	for name, probe := range probes {
		s.addReadinessProbe(name, probe)
	}
}
