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
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"path"
	"sync"
	"time"

	"google.golang.org/grpc/reflection"

	"istio.io/istio/pilot/pkg/status"

	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/networking/apigen"
	"istio.io/istio/pilot/pkg/networking/grpcgen"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	prom "github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	"istio.io/pkg/ctrlz"
	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"
	"istio.io/pkg/version"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	securityModel "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pilot/pkg/xds"
	v2 "istio.io/istio/pilot/pkg/xds/v2"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/dns"
	"istio.io/istio/pkg/jwt"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/security/pkg/k8s/chiron"
	"istio.io/istio/security/pkg/pki/ca"
)

var (
	// DefaultPlugins is the default list of plugins to enable, when no plugin(s)
	// is specified through the command line
	DefaultPlugins = []string{
		plugin.Authn,
		plugin.Authz,
		plugin.Health,
		plugin.Mixer,
	}
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

// startFunc defines a function that will be used to start one or more components of the Pilot discovery service.
type startFunc func(stop <-chan struct{}) error

// readinessProbe defines a function that will be used indicate whether a server is ready.
type readinessProbe func() (bool, error)

// Server contains the runtime configuration for the Pilot discovery service.
type Server struct {
	MonitorListeningAddr net.Addr

	// TODO(nmittler): Consider alternatives to exposing these directly
	EnvoyXdsServer *xds.DiscoveryServer

	clusterID   string
	environment *model.Environment

	kubeRestConfig *rest.Config
	kubeClient     kubelib.Client
	kubeRegistry   *kubecontroller.Controller
	multicluster   *kubecontroller.Multicluster

	configController  model.ConfigStoreCache
	ConfigStores      []model.ConfigStoreCache
	serviceEntryStore *serviceentry.ServiceEntryStore

	httpServer       *http.Server // debug, monitoring and readiness Server.
	httpsServer      *http.Server // webhooks HTTPS Server.
	httpsReadyClient *http.Client

	grpcServer       *grpc.Server
	secureGrpcServer *grpc.Server

	httpMux  *http.ServeMux // debug, monitoring and readiness.
	httpsMux *http.ServeMux // webhooks

	HTTPListener       net.Listener
	GRPCListener       net.Listener
	SecureGrpcListener net.Listener

	DNSListener    net.Listener
	IstioDNSServer *dns.IstioDNS

	// fileWatcher used to watch mesh config, networks and certificates.
	fileWatcher filewatcher.FileWatcher

	certController *chiron.WebhookController
	CA             *ca.IstioCA
	// path to the caBundle that signs the DNS certs. This should be agnostic to provider.
	caBundlePath string
	certMu       sync.Mutex
	istiodCert   *tls.Certificate
	jwtPath      string

	// startFuncs keeps track of functions that need to be executed when Istiod starts.
	startFuncs []startFunc
	// requiredTerminations keeps track of components that should block server exit
	// if they are not stopped. This allows important cleanup tasks to be completed.
	// Note: this is still best effort; a process can die at any time.
	requiredTerminations sync.WaitGroup
	statusReporter       *status.Reporter
	readinessProbes      map[string]readinessProbe

	// duration used for graceful shutdown.
	shutdownDuration time.Duration

	// The SPIFFE based cert verifier
	peerCertVerifier *spiffe.PeerCertVerifier
}

// NewServer creates a new Server instance based on the provided arguments.
func NewServer(args *PilotArgs) (*Server, error) {
	e := &model.Environment{
		ServiceDiscovery: aggregate.NewController(),
		PushContext:      model.NewPushContext(),
		DomainSuffix:     args.RegistryOptions.KubeOptions.DomainSuffix,
	}

	s := &Server{
		clusterID:       getClusterID(args),
		environment:     e,
		EnvoyXdsServer:  xds.NewDiscoveryServer(e, args.Plugins),
		fileWatcher:     filewatcher.NewWatcher(),
		httpMux:         http.NewServeMux(),
		readinessProbes: make(map[string]readinessProbe),
	}

	if args.ShutdownDuration == 0 {
		s.shutdownDuration = 10 * time.Second // If not specified set to 10 seconds.
	}

	if args.RegistryOptions.KubeOptions.WatchedNamespaces != "" {
		// Add the control-plane namespace to the list of watched namespaces.
		args.RegistryOptions.KubeOptions.WatchedNamespaces = fmt.Sprintf("%s,%s",
			args.RegistryOptions.KubeOptions.WatchedNamespaces,
			args.Namespace,
		)
	}

	prometheus.EnableHandlingTimeHistogram()

	s.initMeshConfiguration(args, s.fileWatcher)

	// Apply the arguments to the configuration.
	if err := s.initKubeClient(args); err != nil {
		return nil, fmt.Errorf("error initializing kube client: %v", err)
	}

	s.initMeshNetworks(args, s.fileWatcher)
	s.initMeshHandlers()

	// Parse and validate Istiod Address.
	istiodHost, _, err := e.GetDiscoveryAddress()
	if err != nil {
		return nil, err
	}

	if err := s.initControllers(args); err != nil {
		return nil, err
	}

	s.initGenerators()
	s.initJwtPolicy()

	// Options based on the current 'defaults' in istio.
	caOpts := &CAOptions{
		TrustDomain: s.environment.Mesh().TrustDomain,
		Namespace:   args.Namespace,
	}

	// CA signing certificate must be created first if needed.
	if err := s.maybeCreateCA(caOpts); err != nil {
		return nil, err
	}

	// Create Istiod certs and setup watches.
	if err := s.initIstiodCerts(args, string(istiodHost)); err != nil {
		return nil, err
	}

	// Initialize the SPIFFE peer cert verifier.
	if err := s.setPeerCertVerifier(args.ServerOptions.TLSOptions); err != nil {
		return nil, err
	}

	// Secure gRPC Server must be initialized after CA is created as may use a Citadel generated cert.
	if err := s.initSecureDiscoveryService(args); err != nil {
		return nil, fmt.Errorf("error initializing secure gRPC Listener: %v", err)
	}

	// common https server for webhooks (e.g. injection, validation)
	s.initSecureWebhookServer(args)

	wh, err := s.initSidecarInjector(args)
	if err != nil {
		return nil, fmt.Errorf("error initializing sidecar injector: %v", err)
	}
	if err := s.initConfigValidation(args); err != nil {
		return nil, fmt.Errorf("error initializing config validator: %v", err)
	}
	// Used for readiness, monitoring and debug handlers.
	if err := s.initIstiodAdminServer(args, wh); err != nil {
		return nil, fmt.Errorf("error initializing debug server: %v", err)
	}
	// This should be called only after controllers are initialized.
	if err := s.initRegistryEventHandlers(); err != nil {
		return nil, fmt.Errorf("error initializing handlers: %v", err)
	}
	if err := s.initDiscoveryService(args); err != nil {
		return nil, fmt.Errorf("error initializing discovery service: %v", err)
	}

	// TODO(irisdingbj):add integration test after centralIstiod finished
	args.RegistryOptions.KubeOptions.FetchCaRoot = nil
	args.RegistryOptions.KubeOptions.CABundlePath = s.caBundlePath
	if features.CentralIstioD && s.CA != nil && s.CA.GetCAKeyCertBundle() != nil {
		args.RegistryOptions.KubeOptions.FetchCaRoot = s.fetchCARoot
	}

	if err := s.initClusterRegistries(args); err != nil {
		return nil, fmt.Errorf("error initializing cluster registries: %v", err)
	}

	s.initDNSServer(args)

	// Start CA. This should be called after CA and Istiod certs have been created.
	s.startCA(caOpts)

	s.initNamespaceController(args)

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

	s.addReadinessProbe("discovery", func() (bool, error) {
		return s.EnvoyXdsServer.IsServerReady(), nil
	})

	return s, nil
}

func getClusterID(args *PilotArgs) string {
	clusterID := args.RegistryOptions.KubeOptions.ClusterID
	if clusterID == "" {
		if hasKubeRegistry(args.RegistryOptions.Registries) {
			clusterID = string(serviceregistry.Kubernetes)
		}
	}
	return clusterID
}

// Start starts all components of the Pilot discovery service on the port specified in DiscoveryServerOptions.
// If Port == 0, a port number is automatically chosen. Content serving is started by this method,
// but is executed asynchronously. Serving can be canceled at any time by closing the provided stop channel.
func (s *Server) Start(stop <-chan struct{}) error {
	log.Infof("Staring Istiod Server with primary cluster %s", s.clusterID)

	// Now start all of the components.
	for _, fn := range s.startFuncs {
		if err := fn(stop); err != nil {
			return err
		}
	}
	// Race condition - if waitForCache is too fast and we run this as a startup function,
	// the grpc server would be started before CA is registered. Listening should be last.
	if s.SecureGrpcListener != nil {
		go func() {
			if !s.waitForCacheSync(stop) {
				return
			}
			log.Infof("starting secure gRPC discovery service at %s", s.SecureGrpcListener.Addr())
			if err := s.secureGrpcServer.Serve(s.SecureGrpcListener); err != nil {
				log.Errorf("error from GRPC server: %v", err)
			}
		}()
	}

	// grpcServer is shared by Galley, CA, XDS - must Serve at the end, but before 'wait'
	go func() {
		log.Infof("starting gRPC discovery service at %s", s.GRPCListener.Addr())
		if err := s.grpcServer.Serve(s.GRPCListener); err != nil {
			log.Warna(err)
		}
	}()

	if !s.waitForCacheSync(stop) {
		return fmt.Errorf("failed to sync cache")
	}

	// Inform Discovery Server so that it can start accepting connections.
	log.Infof("All caches have been synced up, marking server ready")
	s.EnvoyXdsServer.CachesSynced()

	// At this point we are ready - start Http Listener so that it can respond to readiness events.
	go func() {
		log.Infof("starting Http service at %s", s.HTTPListener.Addr())
		if err := s.httpServer.Serve(s.HTTPListener); err != nil {
			log.Warna(err)
		}
	}()

	if s.httpsServer != nil {
		go func() {
			log.Infof("starting webhook service at %s", s.HTTPListener.Addr())
			if err := s.httpsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Warna(err)
			}
		}()
	}

	s.waitForShutdown(stop)

	return nil
}

// WaitUntilCompletion waits for everything marked as a "required termination" to complete.
// This should be called before exiting.
func (s *Server) WaitUntilCompletion() {
	s.requiredTerminations.Wait()
}

// initKubeClient creates the k8s client if running in an k8s environment.
// This is determined by the presence of a kube registry, which
// uses in-context k8s, or a config source of type k8s.
func (s *Server) initKubeClient(args *PilotArgs) error {
	hasK8SConfigStore := false
	if args.RegistryOptions.FileDir == "" {
		// If file dir is set - config controller will just use file.
		meshConfig := s.environment.Mesh()
		if meshConfig != nil && len(meshConfig.ConfigSources) > 0 {
			for _, cs := range meshConfig.ConfigSources {
				if cs.Address == "k8s://" {
					hasK8SConfigStore = true
				}
			}
		}
	}

	if hasK8SConfigStore || hasKubeRegistry(args.RegistryOptions.Registries) {
		var err error
		// Used by validation
		s.kubeRestConfig, err = kubelib.DefaultRestConfig(args.RegistryOptions.KubeConfig, "", func(config *rest.Config) {
			config.QPS = 20
			config.Burst = 40
		})
		if err != nil {
			return fmt.Errorf("failed creating kube config: %v", err)
		}

		s.kubeClient, err = kubelib.NewClient(kubelib.NewClientConfigForRestConfig(s.kubeRestConfig))
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
	// TODO check readiness of other secure gRPC and HTTP servers.
	w.WriteHeader(http.StatusOK)
}

// initIstiodHTTPServer initializes monitoring, debug and readiness end points.
func (s *Server) initIstiodAdminServer(args *PilotArgs, wh *inject.Webhook) error {
	log.Info("initializing Istiod admin server")
	s.httpServer = &http.Server{
		Addr:    args.ServerOptions.HTTPAddr,
		Handler: s.httpMux,
	}

	// create http listener
	listener, err := net.Listen("tcp", args.ServerOptions.HTTPAddr)
	if err != nil {
		return err
	}

	// Debug Server.
	s.EnvoyXdsServer.InitDebug(s.httpMux, s.ServiceController(), args.ServerOptions.EnableProfiling, wh)

	// Monitoring Server.
	if err := s.initMonitor(args.ServerOptions.MonitoringAddr); err != nil {
		return fmt.Errorf("error initializing monitor: %v", err)
	}

	// Readiness Handler.
	s.httpMux.HandleFunc("/ready", s.istiodReadyHandler)

	s.HTTPListener = listener
	return nil
}

// initDiscoveryService intializes discovery server on plain text port.
func (s *Server) initDiscoveryService(args *PilotArgs) error {
	log.Infof("starting discovery service")
	// Implement EnvoyXdsServer grace shutdown
	s.addStartFunc(func(stop <-chan struct{}) error {
		s.EnvoyXdsServer.Start(stop)
		return nil
	})

	s.initGrpcServer(args.KeepaliveOptions)
	grpcListener, err := net.Listen("tcp", args.ServerOptions.GRPCAddr)
	if err != nil {
		return err
	}
	s.GRPCListener = grpcListener

	return nil
}

// Wait for the stop, and do cleanups
func (s *Server) waitForShutdown(stop <-chan struct{}) {
	go func() {
		<-stop
		s.fileWatcher.Close()
		model.GetJwtKeyResolver().Close()

		// Stop gRPC services.  If gRPC services fail to stop in the shutdown duration,
		// force stop them. This does not happen normally.
		stopped := make(chan struct{})
		go func() {
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
			log.Warna(err)
		}
		if s.httpsServer != nil {
			if err := s.httpsServer.Shutdown(ctx); err != nil {
				log.Warna(err)
			}
		}

		// Stop DNS Server.
		if s.IstioDNSServer != nil {
			s.IstioDNSServer.Close()
		}
	}()
}

func (s *Server) initGrpcServer(options *istiokeepalive.Options) {
	grpcOptions := s.grpcServerOptions(options)
	s.grpcServer = grpc.NewServer(grpcOptions...)
	s.EnvoyXdsServer.Register(s.grpcServer)
	reflection.Register(s.grpcServer)
}

// initDNSServer initializes gRPC DNS Server for DNS resolutions.
func (s *Server) initDNSServer(args *PilotArgs) {
	if dns.DNSAddr.Get() != "" {
		log.Info("initializing DNS server")
		if err := s.initDNSTLSListener(dns.DNSAddr.Get(), args.ServerOptions.TLSOptions); err != nil {
			log.Warna("error initializing DNS-over-TLS listener ", err)
		}

		// Respond to CoreDNS gRPC queries.
		s.addStartFunc(func(stop <-chan struct{}) error {
			if s.DNSListener != nil {
				dnsSvc := dns.InitDNS()
				dnsSvc.StartDNS(dns.DNSAddr.Get(), s.DNSListener)
			}
			return nil
		})
	}
}

// initialize DNS server listener - uses the same certs as gRPC
func (s *Server) initDNSTLSListener(dns string, tlsOptions TLSOptions) error {
	if dns == "" {
		return nil
	}
	// Mainly for tests.
	if !hasCustomTLSCerts(tlsOptions) && s.CA == nil {
		return nil
	}

	// TODO: check if client certs can be used with coredns or others.
	// If yes - we may require or optionally use them
	cfg := &tls.Config{
		GetCertificate: s.getIstiodCertificate,
		ClientAuth:     tls.NoClientCert,
	}

	// create secure grpc listener
	l, err := net.Listen("tcp", dns)
	if err != nil {
		return err
	}

	tl := tls.NewListener(l, cfg)
	s.DNSListener = tl

	return nil
}

// initialize secureGRPCServer.
func (s *Server) initSecureDiscoveryService(args *PilotArgs) error {
	if s.peerCertVerifier == nil {
		// Running locally without configured certs - no TLS mode
		log.Warnf("The secure discovery service is disabled")
		return nil
	}
	log.Info("initializing secure discovery service")

	cfg := &tls.Config{
		GetCertificate: s.getIstiodCertificate,
		ClientAuth:     tls.VerifyClientCertIfGiven,
		ClientCAs:      s.peerCertVerifier.GetGeneralCertPool(),
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			err := s.peerCertVerifier.VerifyPeerCert(rawCerts, verifiedChains)
			if err != nil {
				log.Infof("Could not verify certificate: %v", err)
			}
			return err
		},
	}

	tlsCreds := credentials.NewTLS(cfg)

	// Default is 15012 - istio-agent relies on this as a default to distinguish what cert auth to expect.
	// TODO(ramaraochavali): clean up istio-agent startup to remove the dependency of "15012" port.

	// create secure grpc listener
	l, err := net.Listen("tcp", args.ServerOptions.SecureGRPCAddr)
	if err != nil {
		return err
	}
	s.SecureGrpcListener = l

	opts := s.grpcServerOptions(args.KeepaliveOptions)
	opts = append(opts, grpc.Creds(tlsCreds))

	s.secureGrpcServer = grpc.NewServer(opts...)
	s.EnvoyXdsServer.Register(s.secureGrpcServer)
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

func (s *Server) grpcServerOptions(options *istiokeepalive.Options) []grpc.ServerOption {
	interceptors := []grpc.UnaryServerInterceptor{
		// setup server prometheus monitoring (as final interceptor in chain)
		prometheus.UnaryServerInterceptor,
	}

	// Temp setting, default should be enough for most supported environments. Can be used for testing
	// envoy with lower values.
	maxStreams := features.MaxConcurrentStreams
	maxRecvMsgSize := features.MaxRecvMsgSize

	grpcOptions := []grpc.ServerOption{
		grpc.UnaryInterceptor(middleware.ChainUnaryServer(interceptors...)),
		grpc.MaxConcurrentStreams(uint32(maxStreams)),
		grpc.MaxRecvMsgSize(maxRecvMsgSize),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:                  options.Time,
			Timeout:               options.Timeout,
			MaxConnectionAge:      options.MaxServerConnectionAge,
			MaxConnectionAgeGrace: options.MaxServerConnectionAgeGrace,
		}),
	}

	return grpcOptions
}

// addStartFunc appends a function to be run. These are run synchronously in order,
// so the function should start a go routine if it needs to do anything blocking
func (s *Server) addStartFunc(fn startFunc) {
	s.startFuncs = append(s.startFuncs, fn)
}

// adds a readiness probe for Istiod Server.
func (s *Server) addReadinessProbe(name string, fn readinessProbe) {
	s.readinessProbes[name] = fn
}

// addRequireStartFunc adds a function that should terminate before the serve shuts down
// This is useful to do cleanup activities
// This is does not guarantee they will terminate gracefully - best effort only
// Function should be synchronous; once it returns it is considered "done"
func (s *Server) addTerminatingStartFunc(fn startFunc) {
	s.addStartFunc(func(stop <-chan struct{}) error {
		// We mark this as a required termination as an optimization. Without this, when we exit the lock is
		// still held for some time (30-60s or so). If we allow time for a graceful exit, then we can immediately drop the lock.
		s.requiredTerminations.Add(1)
		go func() {
			err := fn(stop)
			if err != nil {
				log.Errorf("failure in startup function: %v", err)
			}
			s.requiredTerminations.Done()
		}()
		return nil
	})
}

func (s *Server) waitForCacheSync(stop <-chan struct{}) bool {
	if !cache.WaitForCacheSync(stop, s.cachesSynced) {
		log.Errorf("Failed waiting for cache sync")
		return false
	}

	return true
}

// cachesSynced checks whether caches have been synced.
func (s *Server) cachesSynced() bool {
	if s.multicluster != nil && !s.multicluster.HasSynced() {
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
func (s *Server) initRegistryEventHandlers() error {
	log.Info("initializing registry event handlers")
	// Flush cached discovery responses whenever services configuration change.
	serviceHandler := func(svc *model.Service, _ model.Event) {
		pushReq := &model.PushRequest{
			Full: true,
			ConfigsUpdated: map[model.ConfigKey]struct{}{{
				Kind:      gvk.ServiceEntry,
				Name:      string(svc.Hostname),
				Namespace: svc.Attributes.Namespace,
			}: {}},
			Reason: []model.TriggerReason{model.ServiceUpdate},
		}
		s.EnvoyXdsServer.ConfigUpdate(pushReq)
	}
	if err := s.ServiceController().AppendServiceHandler(serviceHandler); err != nil {
		return fmt.Errorf("append service handler failed: %v", err)
	}

	instanceHandler := func(si *model.ServiceInstance, _ model.Event) {
		// TODO: This is an incomplete code. This code path is called for consul, etc.
		// In all cases, this is simply an instance update and not a config update. So, we need to update
		// EDS in all proxies, and do a full config push for the instance that just changed (add/update only).
		s.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{
			Full: true,
			ConfigsUpdated: map[model.ConfigKey]struct{}{{
				Kind:      gvk.ServiceEntry,
				Name:      string(si.Service.Hostname),
				Namespace: si.Service.Attributes.Namespace,
			}: {}},
			Reason: []model.TriggerReason{model.ServiceUpdate},
		})
	}
	for _, registry := range s.ServiceController().GetRegistries() {
		// Skip kubernetes and external registries as they are handled separately
		if registry.Provider() == serviceregistry.Kubernetes ||
			registry.Provider() == serviceregistry.External {
			continue
		}
		if err := registry.AppendInstanceHandler(instanceHandler); err != nil {
			return fmt.Errorf("append instance handler to registry %s failed: %v", registry.Provider(), err)
		}
	}

	if s.configController != nil {
		configHandler := func(_, curr model.Config, event model.Event) {
			pushReq := &model.PushRequest{
				Full: true,
				ConfigsUpdated: map[model.ConfigKey]struct{}{{
					Kind:      curr.GroupVersionKind,
					Name:      curr.Name,
					Namespace: curr.Namespace,
				}: {}},
				Reason: []model.TriggerReason{model.ConfigUpdate},
			}
			s.EnvoyXdsServer.ConfigUpdate(pushReq)
			if features.EnableStatus {
				if event != model.EventDelete {
					s.statusReporter.AddInProgressResource(curr)
				} else {
					s.statusReporter.DeleteInProgressResource(curr)
				}
			}
		}
		schemas := collections.Pilot.All()
		if features.EnableServiceApis {
			schemas = collections.PilotServiceApi.All()
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

			s.configController.RegisterEventHandler(schema.Resource().GroupVersionKind(), configHandler)
		}
	}

	return nil
}

// initIstiodCerts creates Istiod certificates and also sets up watches to them.
func (s *Server) initIstiodCerts(args *PilotArgs, host string) error {
	if err := s.maybeInitDNSCerts(args, host); err != nil {
		return fmt.Errorf("error initializing DNS certs: %v", err)
	}

	// setup watches for certs
	if err := s.initCertificateWatches(args.ServerOptions.TLSOptions); err != nil {
		// Not crashing istiod - This typically happens if certs are missing and in tests.
		log.Errorf("error initializing certificate watches: %v", err)
	}
	return nil
}

// maybeInitDNSCerts initializes DNS certs if needed.
func (s *Server) maybeInitDNSCerts(args *PilotArgs, host string) error {
	// Generate DNS certificates only if custom certs are not provided via args.
	if !hasCustomTLSCerts(args.ServerOptions.TLSOptions) && s.EnableCA() {
		// Create DNS certificates. This allows injector, validation to work without Citadel, and
		// allows secure SDS connections to Istiod.
		log.Infof("initializing Istiod DNS certificates host: %s, custom host: %s", host, features.IstiodServiceCustomHost.Get())
		if err := s.initDNSCerts(host, features.IstiodServiceCustomHost.Get(), args.Namespace); err != nil {
			return err
		}
	}
	return nil
}

// initCertificateWatches sets up  watches for the certs.
func (s *Server) initCertificateWatches(tlsOptions TLSOptions) error {
	// load the cert/key and setup a persistent watch for updates.
	cert, err := s.getCertKeyPair(tlsOptions)
	if err != nil {
		return err
	}
	s.istiodCert = &cert
	// TODO: Setup watcher for root and restart server if it changes.
	keyFile, certFile := s.getCertKeyPaths(tlsOptions)
	for _, file := range []string{certFile, keyFile} {
		log.Infof("adding watcher for certificate %s", file)
		if err := s.fileWatcher.Add(file); err != nil {
			return fmt.Errorf("could not watch %v: %v", file, err)
		}
	}
	s.addStartFunc(func(stop <-chan struct{}) error {
		go func() {
			var keyCertTimerC <-chan time.Time
			for {
				select {
				case <-keyCertTimerC:
					keyCertTimerC = nil
					// Reload the certificates from the paths.
					cert, err := s.getCertKeyPair(tlsOptions)
					if err != nil {
						log.Errorf("error in reloading certs, %v", err)
						// TODO: Add metrics?
						break
					}
					s.certMu.Lock()
					s.istiodCert = &cert
					s.certMu.Unlock()

					var cnum int
					log.Info("Istiod certificates are reloaded")
					for _, c := range cert.Certificate {
						if x509Cert, err := x509.ParseCertificates(c); err != nil {
							log.Infof("x509 cert [%v] - ParseCertificates() error: %v\n", cnum, err)
							cnum++
						} else {
							for _, c := range x509Cert {
								log.Infof("x509 cert [%v] - Issuer: %q, Subject: %q, SN: %x, NotBefore: %q, NotAfter: %q\n",
									cnum, c.Issuer, c.Subject, c.SerialNumber,
									c.NotBefore.Format(time.RFC3339), c.NotAfter.Format(time.RFC3339))
								cnum++
							}
						}
					}

				case <-s.fileWatcher.Events(certFile):
					if keyCertTimerC == nil {
						keyCertTimerC = time.After(watchDebounceDelay)
					}
				case <-s.fileWatcher.Events(keyFile):
					if keyCertTimerC == nil {
						keyCertTimerC = time.After(watchDebounceDelay)
					}
				case <-s.fileWatcher.Errors(certFile):
					log.Errorf("error watching %v: %v", certFile, err)
				case <-s.fileWatcher.Errors(keyFile):
					log.Errorf("error watching %v: %v", keyFile, err)
				case <-stop:
					return
				}
			}
		}()
		return nil
	})
	return nil
}

// getCertKeyPair returns cert and key loaded in tls.Certificate.
func (s *Server) getCertKeyPair(tlsOptions TLSOptions) (tls.Certificate, error) {
	key, cert := s.getCertKeyPaths(tlsOptions)
	keyPair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return keyPair, nil
}

// getCertKeyPaths returns the paths for key and cert.
func (s *Server) getCertKeyPaths(tlsOptions TLSOptions) (string, string) {
	certDir := dnsCertDir
	key := model.GetOrDefault(tlsOptions.KeyFile, path.Join(certDir, constants.KeyFilename))
	cert := model.GetOrDefault(tlsOptions.CertFile, path.Join(certDir, constants.CertChainFilename))
	return key, cert
}

// setPeerCertVerifier sets up a SPIFFE certificate verifier with the current istiod configuration.
func (s *Server) setPeerCertVerifier(tlsOptions TLSOptions) error {
	if tlsOptions.CaCertFile == "" && s.CA == nil && features.SpiffeBundleEndpoints == "" {
		// Running locally without configured certs - no TLS mode
		return nil
	}
	s.peerCertVerifier = spiffe.NewPeerCertVerifier()
	var rootCertBytes []byte
	var err error
	if tlsOptions.CaCertFile != "" {
		if rootCertBytes, err = ioutil.ReadFile(tlsOptions.CaCertFile); err != nil {
			return err
		}
	} else if s.CA != nil {
		rootCertBytes = s.CA.GetCAKeyCertBundle().GetRootCertPem()
	}

	if len(rootCertBytes) != 0 {
		block, _ := pem.Decode(rootCertBytes)
		if block == nil {
			return fmt.Errorf("failed to decode root cert PEM")
		}
		rootCert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse certificate: %v", err)
		}
		s.peerCertVerifier.AddMapping(spiffe.GetTrustDomain(), []*x509.Certificate{rootCert})
	}

	if features.SpiffeBundleEndpoints != "" {
		certMap, err := spiffe.RetrieveSpiffeBundleRootCertsFromStringInput(
			features.SpiffeBundleEndpoints, []*x509.Certificate{})
		if err != nil {
			return err
		}
		s.peerCertVerifier.AddMappings(certMap)
	}

	return nil
}

// hasCustomTLSCerts returns true if custom TLS certificates are configured via args.
func hasCustomTLSCerts(tlsOptions TLSOptions) bool {
	return tlsOptions.CaCertFile != "" && tlsOptions.CertFile != "" && tlsOptions.KeyFile != ""
}

// getIstiodCertificate returns the istiod certificate.
func (s *Server) getIstiodCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.certMu.Lock()
	defer s.certMu.Unlock()
	return s.istiodCert, nil
}

// initControllers initializes the controllers.
func (s *Server) initControllers(args *PilotArgs) error {
	log.Info("initializing controllers")
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

// initNamespaceController initializes namespace controller to sync config map.
func (s *Server) initNamespaceController(args *PilotArgs) {
	if s.CA != nil && s.kubeClient != nil {
		s.addTerminatingStartFunc(func(stop <-chan struct{}) error {
			leaderelection.
				NewLeaderElection(args.Namespace, args.PodName, leaderelection.NamespaceController, s.kubeClient.Kube()).
				AddRunFunction(func(leaderStop <-chan struct{}) {
					log.Infof("Starting namespace controller")
					nc := kubecontroller.NewNamespaceController(s.fetchCARoot, s.kubeClient)
					// Start informers again. This fixes the case where informers for namespace do not start,
					// as we create them only after acquiring the leader lock
					// Note: stop here should be the overall pilot stop, NOT the leader election stop. We are
					// basically lazy loading the informer, if we stop it when we lose the lock we will never
					// recreate it again.
					s.kubeClient.RunAndWait(stop)
					nc.Run(leaderStop)
				}).
				Run(stop)
			return nil
		})
	}
}

// initGenerators initializes generators to be used by XdsServer.
func (s *Server) initGenerators() {
	s.EnvoyXdsServer.Generators["grpc"] = &grpcgen.GrpcConfigGenerator{}
	epGen := &xds.EdsGenerator{Server: s.EnvoyXdsServer}
	s.EnvoyXdsServer.Generators["grpc/"+v2.EndpointType] = epGen
	s.EnvoyXdsServer.Generators["api"] = &apigen.APIGenerator{}
	s.EnvoyXdsServer.Generators["api/"+v2.EndpointType] = epGen
	s.EnvoyXdsServer.InternalGen = &xds.InternalGen{
		Server: s.EnvoyXdsServer,
	}
	s.EnvoyXdsServer.Generators["api/"+xds.TypeURLConnections] = s.EnvoyXdsServer.InternalGen
	s.EnvoyXdsServer.Generators["event"] = s.EnvoyXdsServer.InternalGen
}

// initJwtPolicy initializes JwtPolicy.
func (s *Server) initJwtPolicy() {
	if features.JwtPolicy.Get() != jwt.PolicyThirdParty {
		log.Infoa("JWT policy is ", features.JwtPolicy.Get())
	}

	switch features.JwtPolicy.Get() {
	case jwt.PolicyThirdParty:
		s.jwtPath = ThirdPartyJWTPath
	case jwt.PolicyFirstParty:
		s.jwtPath = securityModel.K8sSAJwtFileName
	default:
		log.Infof("unknown JWT policy %v, default to certificates ", features.JwtPolicy.Get())
	}
}

// maybeCreateCA creates and initializes CA Key if needed.
func (s *Server) maybeCreateCA(caOpts *CAOptions) error {
	// CA signing certificate must be created only if CA is enabled.
	if s.EnableCA() {
		log.Info("creating CA and initializing public key")
		var err error
		var corev1 v1.CoreV1Interface
		if s.kubeClient != nil {
			corev1 = s.kubeClient.CoreV1()
		}
		// May return nil, if the CA is missing required configs - This is not an error.
		if s.CA, err = s.createIstioCA(corev1, caOpts); err != nil {
			return fmt.Errorf("failed to create CA: %v", err)
		}
		if err = s.initPublicKey(); err != nil {
			return fmt.Errorf("error initializing public key: %v", err)
		}
	}
	return nil
}

// startCA starts the CA server if configured.
func (s *Server) startCA(caOpts *CAOptions) {
	if s.CA != nil {
		s.addStartFunc(func(stop <-chan struct{}) error {
			log.Infof("staring CA")
			s.RunCA(s.secureGrpcServer, s.CA, caOpts)
			return nil
		})
	}
}

func (s *Server) fetchCARoot() map[string]string {
	return map[string]string{
		constants.CACertNamespaceConfigMapDataName: string(s.CA.GetCAKeyCertBundle().GetRootCertPem()),
	}
}

// initMeshHandlers initializes mesh and network handlers.
func (s *Server) initMeshHandlers() {
	log.Info("initializing mesh handlers")
	// When the mesh config or networks change, do a full push.
	s.environment.AddMeshHandler(func() {
		// Inform ConfigGenerator about the mesh config change so that it can rebuild any cached config, before triggering full push.
		s.EnvoyXdsServer.ConfigGenerator.MeshConfigChanged(s.environment.Mesh())
		s.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{
			Full:   true,
			Reason: []model.TriggerReason{model.GlobalUpdate},
		})
	})
	s.environment.AddNetworksHandler(func() {
		s.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{
			Full:   true,
			Reason: []model.TriggerReason{model.GlobalUpdate},
		})
	})
}
