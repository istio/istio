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
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	prom "github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"k8s.io/client-go/rest"

	"istio.io/api/security/v1beta1"
	kubecredentials "istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/features"
	istiogrpc "istio.io/istio/pilot/pkg/grpc"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/server"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pilot/pkg/status/distribution"
	tb "istio.io/istio/pilot/pkg/trustbundle"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/security/pkg/k8s/chiron"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/ra"
	"istio.io/istio/security/pkg/server/ca/authenticate"
	"istio.io/istio/security/pkg/server/ca/authenticate/kubeauth"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	// debounce file watcher events to minimize noise in logs
	watchDebounceDelay = 100 * time.Millisecond
)

func init() {
	// Disable gRPC tracing. It has performance impacts (See https://github.com/grpc/grpc-go/issues/695)
	grpc.EnableTracing = false

	// Export pilot version as metric for fleet analytics.
	pilotVersion := prom.NewGaugeVec(prom.GaugeOpts{
		Name: "pilot_info",
		Help: "Pilot version and build information.",
	}, []string{"version"})
	prom.MustRegister(pilotVersion)
	pilotVersion.With(prom.Labels{"version": version.Info.String()}).Set(1)
}

// readinessProbe defines a function that will be used indicate whether a server is ready.
type readinessProbe func() (bool, error)

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

	httpServer       *http.Server // debug, monitoring and readiness Server.
	httpAddr         string
	httpsServer      *http.Server // webhooks HTTPS Server.
	httpsReadyClient *http.Client

	grpcServer        *grpc.Server
	grpcAddress       string
	secureGrpcServer  *grpc.Server
	secureGrpcAddress string

	// monitoringMux listens on monitoringAddr(:15014).
	// Currently runs prometheus monitoring and debug (if enabled).
	monitoringMux *http.ServeMux
	// internalDebugMux is a mux for *internal* calls to the debug interface. That is, authentication is disabled.
	internalDebugMux *http.ServeMux

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

	certController *chiron.WebhookController
	CA             *ca.IstioCA
	RA             ra.RegistrationAuthority

	// TrustAnchors for workload to workload mTLS
	workloadTrustBundle     *tb.TrustBundle
	certMu                  sync.RWMutex
	istiodCert              *tls.Certificate
	istiodCertBundleWatcher *keycertbundle.Watcher
	server                  server.Instance

	readinessProbes map[string]readinessProbe

	// duration used for graceful shutdown.
	shutdownDuration time.Duration

	// internalStop is closed when the server is shutdown. This should be avoided as much as possible, in
	// favor of AddStartFunc. This is only required if we *must* start something outside of this process.
	// For example, everything depends on mesh config, so we use it there rather than trying to sequence everything
	// in AddStartFunc
	internalStop chan struct{}

	statusReporter *distribution.Reporter
	statusManager  *status.Manager
	// RWConfigStore is the configstore which allows updates, particularly for status.
	RWConfigStore model.ConfigStoreController
}

// NewServer creates a new Server instance based on the provided arguments.
func NewServer(args *PilotArgs, initFuncs ...func(*Server)) (*Server, error) {
	e := model.NewEnvironment()
	e.DomainSuffix = args.RegistryOptions.KubeOptions.DomainSuffix
	e.SetLedger(buildLedger(args.RegistryOptions))

	ac := aggregate.NewController(aggregate.Options{
		MeshHolder: e,
	})
	e.ServiceDiscovery = ac

	s := &Server{
		clusterID:               getClusterID(args),
		environment:             e,
		fileWatcher:             filewatcher.NewWatcher(),
		httpMux:                 http.NewServeMux(),
		monitoringMux:           http.NewServeMux(),
		readinessProbes:         make(map[string]readinessProbe),
		workloadTrustBundle:     tb.NewTrustBundle(nil),
		server:                  server.New(),
		shutdownDuration:        args.ShutdownDuration,
		internalStop:            make(chan struct{}),
		istiodCertBundleWatcher: keycertbundle.NewWatcher(),
	}

	// Used for readiness, monitoring and debug handlers.
	var (
		whMu sync.RWMutex
		wh   *inject.Webhook
	)
	whc := func() map[string]string {
		whMu.RLock()
		defer whMu.RUnlock()
		if wh != nil {
			return wh.Config.RawTemplates
		}
		return map[string]string{}
	}

	// Apply custom initialization functions.
	for _, fn := range initFuncs {
		fn(s)
	}
	// Initialize workload Trust Bundle before XDS Server
	e.TrustBundle = s.workloadTrustBundle
	s.XDSServer = xds.NewDiscoveryServer(e, args.PodName, args.RegistryOptions.KubeOptions.ClusterAliases)

	prometheus.EnableHandlingTimeHistogram()

	// make sure we have a readiness probe before serving HTTP to avoid marking ready too soon
	s.addReadinessProbe("discovery", func() (bool, error) {
		return s.XDSServer.IsServerReady(), nil
	})
	s.initServers(args)
	if err := s.initIstiodAdminServer(args, whc); err != nil {
		return nil, fmt.Errorf("error initializing debug server: %v", err)
	}
	if err := s.serveHTTP(); err != nil {
		return nil, fmt.Errorf("error serving http: %v", err)
	}

	// Apply the arguments to the configuration.
	if err := s.initKubeClient(args); err != nil {
		return nil, fmt.Errorf("error initializing kube client: %v", err)
	}

	// used for both initKubeRegistry and initClusterRegistries
	args.RegistryOptions.KubeOptions.EndpointMode = kubecontroller.DetectEndpointMode(s.kubeClient)

	s.initMeshConfiguration(args, s.fileWatcher)
	spiffe.SetTrustDomain(s.environment.Mesh().GetTrustDomain())

	s.initMeshNetworks(args, s.fileWatcher)
	s.initMeshHandlers()
	s.environment.Init()
	if err := s.environment.InitNetworksManager(s.XDSServer); err != nil {
		return nil, err
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

	s.XDSServer.InitGenerators(e, args.Namespace, s.internalDebugMux)

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
	if err := s.initSecureDiscoveryService(args); err != nil {
		return nil, fmt.Errorf("error initializing secure gRPC Listener: %v", err)
	}

	// common https server for webhooks (e.g. injection, validation)
	if s.kubeClient != nil {
		s.initSecureWebhookServer(args)
		whMu.Lock()
		wh, err = s.initSidecarInjector(args)
		whMu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("error initializing sidecar injector: %v", err)
		}
		if err := s.initConfigValidation(args); err != nil {
			return nil, fmt.Errorf("error initializing config validator: %v", err)
		}
	}

	// This should be called only after controllers are initialized.
	s.initRegistryEventHandlers()

	s.initDiscoveryService()

	s.initSDSServer()

	// Notice that the order of authenticators matters, since at runtime
	// authenticators are activated sequentially and the first successful attempt
	// is used as the authentication result.
	authenticators := []security.Authenticator{
		&authenticate.ClientCertAuthenticator{},
	}
	if args.JwtRule != "" {
		jwtAuthn, err := initOIDC(args, s.environment.Mesh().TrustDomain)
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
			kubeauth.NewKubeJWTAuthenticator(s.environment.Watcher, s.kubeClient.Kube(), s.clusterID, s.multiclusterController.GetRemoteKubeClient, features.JwtPolicy))
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
		s.addStartFunc(func(stop <-chan struct{}) error {
			s.kubeClient.RunAndWait(stop)
			return nil
		})
	}

	return s, nil
}

func initOIDC(args *PilotArgs, trustDomain string) (security.Authenticator, error) {
	// JWTRule is from the JWT_RULE environment variable.
	// An example of json string for JWTRule is:
	// `{"issuer": "foo", "jwks_uri": "baz", "audiences": ["aud1", "aud2"]}`.
	jwtRule := &v1beta1.JWTRule{}
	err := json.Unmarshal([]byte(args.JwtRule), jwtRule)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JWT rule: %v", err)
	}
	log.Infof("Istiod authenticating using JWTRule: %v", jwtRule)
	jwtAuthn, err := authenticate.NewJwtAuthenticator(jwtRule, trustDomain)
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

func isUnexpectedListenerError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return false
	}
	if errors.Is(err, http.ErrServerClosed) {
		return false
	}
	return true
}

// Start starts all components of the Pilot discovery service on the port specified in DiscoveryServerOptions.
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
			if err := s.httpsServer.ServeTLS(httpsListener, "", ""); isUnexpectedListenerError(err) {
				log.Errorf("error serving https server: %v", err)
			}
		}()
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
		creds := kubecredentials.NewMulticluster(s.clusterID)
		creds.AddSecretHandler(func(name string, namespace string) {
			s.XDSServer.ConfigUpdate(&model.PushRequest{
				Full: false,
				ConfigsUpdated: map[model.ConfigKey]struct{}{
					{
						Kind:      kind.Secret,
						Name:      name,
						Namespace: namespace,
					}: {},
				},
				Reason: []model.TriggerReason{model.SecretTrigger},
			})
		})
		s.XDSServer.Generators[v3.SecretType] = xds.NewSecretGen(creds, s.XDSServer.Cache, s.clusterID, s.environment.Mesh())
		s.multiclusterController.AddHandler(creds)
		if ecdsGen, found := s.XDSServer.Generators[v3.ExtensionConfigurationType]; found {
			ecdsGen.(*xds.EcdsGenerator).SetCredController(creds)
		}
	}
}

// initKubeClient creates the k8s client if running in an k8s environment.
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

		s.kubeClient, err = kubelib.NewClient(kubelib.NewClientConfigForRestConfig(kubeRestConfig))
		if err != nil {
			return fmt.Errorf("failed creating kube client: %v", err)
		}
	}

	return nil
}

// A single container can't have two readiness probes. Make this readiness probe a generic one
// that can handle all istiod related readiness checks including webhook, gRPC etc.
// The "http" portion of the readiness check is satisfied by the fact we've started listening on
// this handler and everything has already initialized.
func (s *Server) istiodReadyHandler(w http.ResponseWriter, _ *http.Request) {
	for name, fn := range s.readinessProbes {
		if ready, err := fn(); !ready {
			log.Warnf("%s is not ready: %v", name, err)
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
		log.Info("multiplexing gRPC on http addr ", args.ServerOptions.HTTPAddr)
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
		s.httpServer.Handler = multiplexHandler
	}

	if args.ServerOptions.MonitoringAddr == "" {
		s.monitoringMux = s.httpMux
		log.Info("initializing Istiod admin server multiplexed on httpAddr ", s.httpServer.Addr)
	} else {
		log.Info("initializing Istiod admin server")
	}
}

// initIstiodAdminServer initializes monitoring, debug and readiness end points.
func (s *Server) initIstiodAdminServer(args *PilotArgs, whc func() map[string]string) error {
	// Debug Server.
	internalMux := s.XDSServer.InitDebug(s.monitoringMux, s.ServiceController(), args.ServerOptions.EnableProfiling, whc)
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
	s.addStartFunc(func(stop <-chan struct{}) error {
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
		prometheus.UnaryServerInterceptor,
	}
	grpcOptions := istiogrpc.ServerOptions(options, interceptors...)
	s.grpcServer = grpc.NewServer(grpcOptions...)
	s.XDSServer.Register(s.grpcServer)
	reflection.Register(s.grpcServer)
}

// initialize secureGRPCServer.
func (s *Server) initSecureDiscoveryService(args *PilotArgs) error {
	if args.ServerOptions.SecureGRPCAddr == "" {
		log.Info("The secure discovery port is disabled, multiplexing on httpAddr ")
		return nil
	}

	peerCertVerifier, err := s.createPeerCertVerifier(args.ServerOptions.TLSOptions)
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

	tlsCreds := credentials.NewTLS(cfg)

	s.secureGrpcAddress = args.ServerOptions.SecureGRPCAddr

	interceptors := []grpc.UnaryServerInterceptor{
		// setup server prometheus monitoring (as final interceptor in chain)
		prometheus.UnaryServerInterceptor,
	}
	opts := istiogrpc.ServerOptions(args.KeepaliveOptions, interceptors...)
	opts = append(opts, grpc.Creds(tlsCreds))

	s.secureGrpcServer = grpc.NewServer(opts...)
	s.XDSServer.Register(s.secureGrpcServer)
	reflection.Register(s.secureGrpcServer)

	s.addStartFunc(func(stop <-chan struct{}) error {
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
func (s *Server) addStartFunc(fn server.Component) {
	s.server.RunComponent(fn)
}

// adds a readiness probe for Istiod Server.
func (s *Server) addReadinessProbe(name string, fn readinessProbe) {
	s.readinessProbes[name] = fn
}

// addTerminatingStartFunc adds a function that should terminate before the serve shuts down
// This is useful to do cleanup activities
// This is does not guarantee they will terminate gracefully - best effort only
// Function should be synchronous; once it returns it is considered "done"
func (s *Server) addTerminatingStartFunc(fn server.Component) {
	s.server.RunComponentAsyncAndWait(fn)
}

func (s *Server) waitForCacheSync(stop <-chan struct{}) bool {
	start := time.Now()
	log.Info("Waiting for caches to be synced")
	if !kubelib.WaitForCacheSync(stop, s.cachesSynced) {
		log.Errorf("Failed waiting for cache sync")
		return false
	}
	log.Infof("All controller caches have been synced up in %v", time.Since(start))

	// At this point, we know that all update events of the initial state-of-the-world have been
	// received. We wait to ensure we have committed at least this many updates. This avoids a race
	// condition where we are marked ready prior to updating the push context, leading to incomplete
	// pushes.
	expected := s.XDSServer.InboundUpdates.Load()
	if !kubelib.WaitForCacheSync(stop, func() bool { return s.pushContextReady(expected) }) {
		log.Errorf("Failed waiting for push context initialization")
		return false
	}

	return true
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
	serviceHandler := func(svc *model.Service, _ model.Event) {
		pushReq := &model.PushRequest{
			Full: true,
			ConfigsUpdated: map[model.ConfigKey]struct{}{{
				Kind:      kind.ServiceEntry,
				Name:      string(svc.Hostname),
				Namespace: svc.Attributes.Namespace,
			}: {}},
			Reason: []model.TriggerReason{model.ServiceUpdate},
		}
		s.XDSServer.ConfigUpdate(pushReq)
	}
	s.ServiceController().AppendServiceHandler(serviceHandler)

	if s.configController != nil {
		configHandler := func(prev config.Config, curr config.Config, event model.Event) {
			defer func() {
				if event != model.EventDelete {
					s.statusReporter.AddInProgressResource(curr)
				} else {
					s.statusReporter.DeleteInProgressResource(curr)
				}
			}()
			log.Debugf("Handle event %s for configuration %s", event, curr.Key())
			// For update events, trigger push only if spec has changed.
			if event == model.EventUpdate && !needsPush(prev, curr) {
				log.Debugf("skipping push for %s as spec has not changed", prev.Key())
				return
			}
			pushReq := &model.PushRequest{
				Full: true,
				ConfigsUpdated: map[model.ConfigKey]struct{}{{
					Kind:      kind.FromGvk(curr.GroupVersionKind),
					Name:      curr.Name,
					Namespace: curr.Namespace,
				}: {}},
				Reason: []model.TriggerReason{model.ConfigUpdate},
			}
			s.XDSServer.ConfigUpdate(pushReq)
		}
		schemas := collections.Pilot.All()
		if features.EnableGatewayAPI {
			schemas = collections.PilotGatewayAPI.All()
		}
		for _, schema := range schemas {
			// This resource type was handled in external/servicediscovery.go, no need to rehandle here.
			if schema.Resource().GroupVersionKind() == collections.IstioNetworkingV1Alpha3Serviceentries.
				Resource().GroupVersionKind() {
				continue
			}
			if schema.Resource().GroupVersionKind() == collections.IstioNetworkingV1Alpha3Workloadentries.
				Resource().GroupVersionKind() {
				continue
			}
			if schema.Resource().GroupVersionKind() == collections.IstioNetworkingV1Alpha3Workloadgroups.
				Resource().GroupVersionKind() {
				continue
			}

			s.configController.RegisterEventHandler(schema.Resource().GroupVersionKind(), configHandler)
		}
		if s.environment.GatewayAPIController != nil {
			s.environment.GatewayAPIController.RegisterEventHandler(gvk.Namespace, func(config.Config, config.Config, model.Event) {
				s.XDSServer.ConfigUpdate(&model.PushRequest{
					Full:   true,
					Reason: []model.TriggerReason{model.NamespaceUpdate},
				})
			})
		}
	}
}

func (s *Server) initIstiodCertLoader() error {
	if err := s.loadIstiodCert(); err != nil {
		return fmt.Errorf("first time load IstiodCert failed: %v", err)
	}
	_, watchCh := s.istiodCertBundleWatcher.AddWatcher()
	s.addStartFunc(func(stop <-chan struct{}) error {
		go s.reloadIstiodCert(watchCh, stop)
		return nil
	})
	return nil
}

// initIstiodCerts creates Istiod certificates and also sets up watches to them.
func (s *Server) initIstiodCerts(args *PilotArgs, host string) error {
	// Skip all certificates
	var err error

	s.dnsNames = getDNSNames(args, host)
	if hasCustomTLSCerts(args.ServerOptions.TLSOptions) {
		// Use the DNS certificate provided via args.
		err = s.initCertificateWatches(args.ServerOptions.TLSOptions)
		if err != nil {
			// Not crashing istiod - This typically happens if certs are missing and in tests.
			log.Errorf("error initializing certificate watches: %v", err)
			return nil
		}
	} else if features.EnableCAServer && features.PilotCertProvider == constants.CertProviderIstiod {
		log.Infof("initializing Istiod DNS certificates host: %s, custom host: %s", host, features.IstiodServiceCustomHost)
		err = s.initDNSCerts()
	} else if features.PilotCertProvider == constants.CertProviderKubernetes {
		log.Infof("initializing Istiod DNS certificates host: %s, custom host: %s", host, features.IstiodServiceCustomHost)
		err = s.initDNSCerts()
	} else if strings.HasPrefix(features.PilotCertProvider, constants.CertProviderKubernetesSignerPrefix) {
		log.Infof("initializing Istiod DNS certificates host: %s, custom host: %s", host, features.IstiodServiceCustomHost)
		err = s.initDNSCerts()
	} else {
		return nil
	}

	if err == nil {
		err = s.initIstiodCertLoader()
	}

	return err
}

func getDNSNames(args *PilotArgs, host string) []string {
	dnsNames := []string{host}
	// Append custom hostname if there is any
	customHost := features.IstiodServiceCustomHost
	cHosts := strings.Split(customHost, ",")
	for _, cHost := range cHosts {
		if cHost != "" && cHost != host {
			log.Infof("Adding custom hostname %s", cHost)
			dnsNames = append(dnsNames, cHost)
		}
	}

	// The first is the recommended one, also used by Apiserver for webhooks.
	// add a few known hostnames
	knownHosts := []string{"istiod", "istiod-remote", "istio-pilot"}
	// In some conditions, pilot address for sds is different from other xds,
	// like multi-cluster primary-remote mode with revision.
	if args.Revision != "" && args.Revision != "default" {
		knownHosts = append(knownHosts, "istiod"+"-"+args.Revision)
	}

	for _, altName := range knownHosts {
		name := fmt.Sprintf("%v.%v.svc", altName, args.Namespace)
		exist := false
		for _, cHost := range cHosts {
			if name == host || name == cHost {
				exist = true
			}
		}
		if !exist {
			dnsNames = append(dnsNames, name)
		}
	}

	return dnsNames
}

// createPeerCertVerifier creates a SPIFFE certificate verifier with the current istiod configuration.
func (s *Server) createPeerCertVerifier(tlsOptions TLSOptions) (*spiffe.PeerCertVerifier, error) {
	if tlsOptions.CaCertFile == "" && s.CA == nil && features.SpiffeBundleEndpoints == "" && !s.isCADisabled() {
		// Running locally without configured certs - no TLS mode
		return nil, nil
	}
	peerCertVerifier := spiffe.NewPeerCertVerifier()
	var rootCertBytes []byte
	var err error
	if tlsOptions.CaCertFile != "" {
		if rootCertBytes, err = os.ReadFile(tlsOptions.CaCertFile); err != nil {
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
		err := peerCertVerifier.AddMappingFromPEM(spiffe.GetTrustDomain(), rootCertBytes)
		if err != nil {
			return nil, fmt.Errorf("add root CAs into peerCertVerifier failed: %v", err)
		}
	}

	if features.SpiffeBundleEndpoints != "" {
		certMap, err := spiffe.RetrieveSpiffeBundleRootCertsFromStringInput(
			features.SpiffeBundleEndpoints, []*x509.Certificate{})
		if err != nil {
			return nil, err
		}
		peerCertVerifier.AddMappings(certMap)
	}

	return peerCertVerifier, nil
}

// hasCustomTLSCerts returns true if custom TLS certificates are configured via args.
func hasCustomTLSCerts(tlsOptions TLSOptions) bool {
	return tlsOptions.CaCertFile != "" && tlsOptions.CertFile != "" && tlsOptions.KeyFile != ""
}

// getIstiodCertificate returns the istiod certificate.
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
	// Certificate controller is created before MCP controller in case MCP server pod
	// waits to mount a certificate to be provisioned by the certificate controller.
	if err := s.initCertController(args); err != nil {
		return fmt.Errorf("error initializing certificate controller: %v", err)
	}
	if err := s.initConfigController(args); err != nil {
		return fmt.Errorf("error initializing config controller: %v", err)
	}
	if err := s.initServiceControllers(args); err != nil {
		return fmt.Errorf("error initializing service controllers: %v", err)
	}
	return nil
}

func (s *Server) initMulticluster(args *PilotArgs) {
	if s.kubeClient == nil {
		return
	}
	s.multiclusterController = multicluster.NewController(s.kubeClient, args.Namespace, s.clusterID)
	s.XDSServer.ListRemoteClusters = s.multiclusterController.ListRemoteClusters
	s.addStartFunc(func(stop <-chan struct{}) error {
		return s.multiclusterController.Run(stop)
	})
}

// maybeCreateCA creates and initializes CA Key if needed.
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
		if caOpts.ExternalCAType != "" {
			if s.RA, err = s.createIstioRA(caOpts); err != nil {
				return fmt.Errorf("failed to create RA: %v", err)
			}
		}
		if !s.isCADisabled() {
			if s.CA, err = s.createIstioCA(caOpts); err != nil {
				return fmt.Errorf("failed to create CA: %v", err)
			}
		}
	}
	return nil
}

func (s *Server) shouldStartNsController() bool {
	if s.isCADisabled() {
		return true
	}
	if s.CA == nil {
		return false
	}

	// For Kubernetes CA, we don't distribute it; it is mounted in all pods by Kubernetes.
	if features.PilotCertProvider == constants.CertProviderKubernetes {
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
	s.addStartFunc(func(stop <-chan struct{}) error {
		grpcServer := s.secureGrpcServer
		if s.secureGrpcServer == nil {
			grpcServer = s.grpcServer
		}
		// Start the RA server if configured, else start the CA server
		if s.RA != nil {
			log.Infof("Starting RA")
			s.RunCA(grpcServer, s.RA, caOpts)
		} else if s.CA != nil {
			log.Infof("Starting IstioD CA")
			s.RunCA(grpcServer, s.CA, caOpts)
		}
		return nil
	})
}

// initMeshHandlers initializes mesh and network handlers.
func (s *Server) initMeshHandlers() {
	log.Info("initializing mesh handlers")
	// When the mesh config or networks change, do a full push.
	s.environment.AddMeshHandler(func() {
		spiffe.SetTrustDomain(s.environment.Mesh().GetTrustDomain())
		s.XDSServer.ConfigGenerator.MeshConfigChanged(s.environment.Mesh())
		s.XDSServer.ConfigUpdate(&model.PushRequest{
			Full:   true,
			Reason: []model.TriggerReason{model.GlobalUpdate},
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
			Reason: []model.TriggerReason{model.GlobalUpdate},
		}
		s.XDSServer.ConfigUpdate(pushReq)
	})

	s.addStartFunc(func(stop <-chan struct{}) error {
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

// isCADisabled returns whether CA functionality is disabled in istiod.
// It returns true only if istiod certs is signed by Kubernetes or
// workload certs are signed by external CA
func (s *Server) isCADisabled() bool {
	if s.RA == nil {
		return false
	}
	// do not create CA server if PilotCertProvider is `kubernetes` and RA server exists
	if features.PilotCertProvider == constants.CertProviderKubernetes {
		return true
	}
	// do not create CA server if PilotCertProvider is `k8s.io/*` and RA server exists
	if strings.HasPrefix(features.PilotCertProvider, constants.CertProviderKubernetesSignerPrefix) {
		return true
	}
	return false
}

func (s *Server) initStatusManager(_ *PilotArgs) {
	s.addStartFunc(func(stop <-chan struct{}) error {
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
		if err := s.httpServer.Serve(httpListener); isUnexpectedListenerError(err) {
			log.Errorf("error serving http server: %v", err)
		}
	}()
	s.httpAddr = httpListener.Addr().String()
	return nil
}
