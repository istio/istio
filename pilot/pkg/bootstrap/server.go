// Copyright 2017 Istio Authors
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
	"fmt"
	"net"
	"net/http"
	"path"
	"strconv"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	prom "github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	"istio.io/pkg/ctrlz"
	"istio.io/pkg/env"
	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"
	"istio.io/pkg/version"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	envoyv2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	securityModel "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/dns"
	"istio.io/istio/pkg/jwt"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/security/pkg/k8s/chiron"
	"istio.io/istio/security/pkg/pki/ca"
)

var (
	// FilepathWalkInterval dictates how often the file system is walked for config
	FilepathWalkInterval = 100 * time.Millisecond

	// PilotCertDir is the default location for mTLS certificates used by pilot
	// Visible for tests - at runtime can be set by PILOT_CERT_DIR environment variable.
	PilotCertDir = "/etc/certs/"

	// DefaultPlugins is the default list of plugins to enable, when no plugin(s)
	// is specified through the command line
	DefaultPlugins = []string{
		plugin.Authn,
		plugin.Authz,
		plugin.Health,
		plugin.Mixer,
	}

	enableElection = env.RegisterBoolVar("MASTER_ELECTION",
		true,
		"Enable master election")
)

func init() {
	// get the grpc server wired up
	// This should only be set before any RPCs are sent or received by this program.
	grpc.EnableTracing = true

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

// Server contains the runtime configuration for the Pilot discovery service.
type Server struct {
	MonitorListeningAddr net.Addr

	// TODO(nmittler): Consider alternatives to exposing these directly
	EnvoyXdsServer *envoyv2.DiscoveryServer

	clusterID   string
	environment *model.Environment

	kubeConfig       *rest.Config
	configController model.ConfigStoreCache
	kubeClient       kubernetes.Interface
	metadataClient   metadata.Interface

	startFuncs          []startFunc
	multicluster        *kubecontroller.Multicluster
	httpServer          *http.Server // debug
	httpsServer         *http.Server // webhooks
	httpsReadyClient    *http.Client
	grpcServer          *grpc.Server
	secureGRPCServerDNS *grpc.Server
	mux                 *http.ServeMux // debug
	httpsMux            *http.ServeMux // webhooks
	kubeRegistry        *kubecontroller.Controller
	certController      *chiron.WebhookController
	ca                  *ca.IstioCA
	// path to the caBundle that signs the DNS certs. This should be agnostic to provider.
	caBundlePath string

	ConfigStores []model.ConfigStoreCache

	serviceEntryStore *external.ServiceEntryStore

	HTTPListener    net.Listener
	GRPCListener    net.Listener
	GRPCDNSListener net.Listener
	DNSListener     net.Listener

	// for test
	forceStop bool

	// nil if injection disabled
	injectionWebhook *inject.Webhook

	leaderElection *leaderelection.LeaderElection

	webhookCertMu sync.Mutex
	webhookCert   *tls.Certificate
	jwtPath       string

	// requiredTerminations keeps track of components that should block server exit if they are not stopped
	// This allows important cleanup tasks to be completed.
	// Note: this is still best effort; a process can die at any time.
	requiredTerminations sync.WaitGroup
}

// NewServer creates a new Server instance based on the provided arguments.
func NewServer(args *PilotArgs) (*Server, error) {
	e := &model.Environment{
		ServiceDiscovery: aggregate.NewController(),
		PushContext:      model.NewPushContext(),
	}

	s := &Server{
		clusterID:      getClusterID(args),
		environment:    e,
		EnvoyXdsServer: envoyv2.NewDiscoveryServer(e, args.Plugins),
		forceStop:      args.ForceStop,
		mux:            http.NewServeMux(),
	}

	log.Infof("Primary Cluster name: %s", s.clusterID)

	prometheus.EnableHandlingTimeHistogram()

	// Apply the arguments to the configuration.
	if err := s.initKubeClient(args); err != nil {
		return nil, fmt.Errorf("kube client: %v", err)
	}
	s.initLeaderElection(args)
	fileWatcher := filewatcher.NewWatcher()
	if err := s.initMeshConfiguration(args, fileWatcher); err != nil {
		return nil, fmt.Errorf("mesh: %v", err)
	}
	s.initMeshNetworks(args, fileWatcher)
	// Certificate controller is created before MCP
	// controller in case MCP server pod waits to mount a certificate
	// to be provisioned by the certificate controller.
	if err := s.initCertController(args); err != nil {
		return nil, fmt.Errorf("certificate controller: %v", err)
	}
	if err := s.initConfigController(args); err != nil {
		return nil, fmt.Errorf("config controller: %v", err)
	}
	if err := s.initServiceControllers(args); err != nil {
		return nil, fmt.Errorf("service controllers: %v", err)
	}

	// Options based on the current 'defaults' in istio.
	// If adjustments are needed - env or mesh.config ( if of general interest ).
	caOpts := &CAOptions{
		TrustDomain: s.environment.Mesh().TrustDomain,
		Namespace:   args.Namespace,
	}

	// CA signing certificate must be created first.
	if features.JwtPolicy.Get() == jwt.JWTPolicyThirdPartyJWT {
		log.Info("JWT policy is third-party-jwt")
		s.jwtPath = ThirdPartyJWTPath
	} else if features.JwtPolicy.Get() == jwt.JWTPolicyFirstPartyJWT {
		log.Info("JWT policy is first-party-jwt")
		s.jwtPath = securityModel.K8sSAJwtFileName
	} else {
		err := fmt.Errorf("invalid JWT policy %v", features.JwtPolicy.Get())
		log.Errorf("%v", err)
		return nil, err
	}
	if s.EnableCA() {
		var err error
		var corev1 v1.CoreV1Interface
		if s.kubeClient != nil {
			corev1 = s.kubeClient.CoreV1()
		}
		// May return nil, if the CA is missing required configs.
		// This is not an error.
		s.ca, err = s.createCA(corev1, caOpts)
		if err != nil {
			return nil, fmt.Errorf("enableCA: %v", err)
		}
		err = s.initPublicKey()
		if err != nil {
			return nil, fmt.Errorf("init public key: %v", err)
		}
	}

	// initDNSListener() must be called after the createCA()
	// because initDNSListener() may use a Citadel generated cert.
	if err := s.initDNSListener(args); err != nil {
		return nil, fmt.Errorf("grpcDNS: %v", err)
	}
	if err := s.initDiscoveryService(args); err != nil {
		return nil, fmt.Errorf("discovery service: %v", err)
	}
	if err := s.initMonitor(args.DiscoveryOptions.MonitoringAddr); err != nil {
		return nil, fmt.Errorf("monitor: %v", err)
	}
	if err := s.initClusterRegistries(args); err != nil {
		return nil, fmt.Errorf("cluster registries: %v", err)
	}

	// common https server for webhooks (e.g. injection, validation)
	if err := s.initHTTPSWebhookServer(args); err != nil {
		return nil, fmt.Errorf("injectionWebhook server: %v", err)
	}

	// Will run the sidecar injector in pilot.
	// Only operates if /var/lib/istio/inject exists
	if err := s.initSidecarInjector(args); err != nil {
		return nil, fmt.Errorf("sidecar injector: %v", err)
	}

	// Will run the config validater in pilot.
	// Only operates if /var/lib/istio/validation exists
	if err := s.initConfigValidation(args); err != nil {
		return nil, fmt.Errorf("config validation: %v", err)
	}

	if dns.DNSAddr.Get() != "" {
		if err := s.initDNSTLSListener(dns.DNSAddr.Get()); err != nil {
			log.Warna("Failed to start DNS-over-TLS listener ", err)
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

	// Run the SDS signing server.
	// RunCA() must be called after createCA() and initDNSListener()
	// because it depends on the following conditions:
	// 1) CA certificate has been created.
	// 2) grpc server has been generated.
	s.addStartFunc(func(stop <-chan struct{}) error {
		if s.ca != nil {
			s.RunCA(s.secureGRPCServerDNS, s.ca, caOpts, stop)
		}
		return nil
	})

	if s.leaderElection != nil && enableElection.Get() {
		s.addStartFunc(func(stop <-chan struct{}) error {
			// We mark this as a required termination as an optimization. Without this, when we exit the lock is
			// still held for some time (30-60s or so). If we allow time for a graceful exit, then we can immediately drop the lock.
			s.requiredTerminations.Add(1)
			go func() {
				s.leaderElection.Run(stop)
				s.requiredTerminations.Done()
			}()
			return nil
		})
	}

	// TODO: don't run this if galley is started, one ctlz is enough
	if args.CtrlZOptions != nil {
		_, _ = ctrlz.Run(args.CtrlZOptions, nil)
	}

	return s, nil
}

func getClusterID(args *PilotArgs) string {
	clusterID := args.Config.ControllerOptions.ClusterID
	if clusterID == "" {
		for _, registry := range args.Service.Registries {
			if registry == string(serviceregistry.Kubernetes) {
				clusterID = string(serviceregistry.Kubernetes)
				break
			}
		}
	}

	return clusterID
}

// Start starts all components of the Pilot discovery service on the port specified in DiscoveryServiceOptions.
// If Port == 0, a port number is automatically chosen. Content serving is started by this method,
// but is executed asynchronously. Serving can be canceled at any time by closing the provided stop channel.
func (s *Server) Start(stop <-chan struct{}) error {
	// Now start all of the components.
	for _, fn := range s.startFuncs {
		if err := fn(stop); err != nil {
			return err
		}
	}
	// Race condition - if waitForCache is too fast and we run this as a startup function,
	// the grpc server would be started before CA is registered. Listening should be last.
	if s.GRPCDNSListener != nil {
		go func() {
			if !s.waitForCacheSync(stop) {
				return
			}
			log.Infof("starting secure (DNS) gRPC discovery service at %s", s.GRPCDNSListener.Addr())
			if err := s.secureGRPCServerDNS.Serve(s.GRPCDNSListener); err != nil {
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

	// Trigger a push, so that the global push context is updated with the new config and Pilot's local Envoy
	// also is updated with new config.
	log.Infof("All caches have been synced up, triggering a push")
	s.EnvoyXdsServer.Push(&model.PushRequest{Full: true})

	// At this point we are ready - start Http Listener so that it can respond to readiness events.
	go func() {
		log.Infof("starting Http service at %s", s.HTTPListener.Addr())
		if err := s.httpServer.Serve(s.HTTPListener); err != nil {
			log.Warna(err)
		}
	}()

	if s.httpsServer != nil {
		go func() {
			if err := s.httpsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Warna(err)
			}
		}()
	}

	s.cleanupOnStop(stop)

	return nil
}

// WaitUntilCompletion waits for everything marked as a "required termination" to complete.
// This should be called before exiting.
func (s *Server) WaitUntilCompletion() {
	s.requiredTerminations.Wait()
}

// initKubeClient creates the k8s client if running in an k8s environment.
func (s *Server) initKubeClient(args *PilotArgs) error {
	if hasKubeRegistry(args.Service.Registries) {
		var err error
		// Used by validation
		s.kubeConfig, err = kubelib.BuildClientConfig(args.Config.KubeConfig, "")
		if err != nil {
			return fmt.Errorf("failed creating kube config: %v", err)
		}
		s.kubeClient, err = kubelib.CreateClientset(args.Config.KubeConfig, "", func(config *rest.Config) {
			config.QPS = 20
			config.Burst = 40
		})
		if err != nil {
			return fmt.Errorf("failed creating kube client: %v", err)
		}

		s.metadataClient, err = kubelib.CreateMetadataClient(args.Config.KubeConfig, "")
		if err != nil {
			return fmt.Errorf("failed creating kube metadata client: %v", err)
		}
	}

	return nil
}

// A single container can't have two readiness probes. Piggyback the https server readiness
// onto the http server readiness check. The "http" portion of the readiness check is satisfied
// by the fact we've started listening on this handler and everything has already initialized.
func (s *Server) httpServerReadyHandler(w http.ResponseWriter, _ *http.Request) {
	if features.IstiodService.Get() != "" {
		if status := s.checkHTTPSWebhookServerReadiness(); status != http.StatusOK {
			log.Warnf("https webhook server not ready: %v", status)
			w.WriteHeader(status)
			return
		}
	}

	// TODO check readiness of other secure gRPC and HTTP servers.

	w.WriteHeader(http.StatusOK)
}

func (s *Server) initDiscoveryService(args *PilotArgs) error {
	s.mux.HandleFunc("/ready", s.httpServerReadyHandler)

	s.EnvoyXdsServer.InitDebug(s.mux, s.ServiceController(), args.DiscoveryOptions.EnableProfiling, s.injectionWebhook)

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

	if err := s.initEventHandlers(); err != nil {
		return err
	}

	// Implement EnvoyXdsServer grace shutdown
	s.addStartFunc(func(stop <-chan struct{}) error {
		s.EnvoyXdsServer.Start(stop)
		return nil
	})

	// create grpc/http server
	s.initGrpcServer(args.KeepaliveOptions)
	s.httpServer = &http.Server{
		Addr:    args.DiscoveryOptions.HTTPAddr,
		Handler: s.mux,
	}

	// create http listener
	listener, err := net.Listen("tcp", args.DiscoveryOptions.HTTPAddr)
	if err != nil {
		return err
	}
	s.HTTPListener = listener

	// create grpc listener
	grpcListener, err := net.Listen("tcp", args.DiscoveryOptions.GrpcAddr)
	if err != nil {
		return err
	}
	s.GRPCListener = grpcListener

	return nil
}

// Wait for the stop, and do cleanups
func (s *Server) cleanupOnStop(stop <-chan struct{}) {
	go func() {
		<-stop
		model.JwtKeyResolver.Close()

		if s.forceStop {
			s.grpcServer.Stop()
			_ = s.httpServer.Close()
			if features.IstiodService.Get() != "" {
				_ = s.httpsServer.Close()
			}
		} else {
			s.grpcServer.GracefulStop()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.httpServer.Shutdown(ctx); err != nil {
				log.Warna(err)
			}
			if features.IstiodService.Get() != "" {
				if err := s.httpsServer.Shutdown(ctx); err != nil {
					log.Warna(err)
				}
			}
		}
	}()
}

func (s *Server) initGrpcServer(options *istiokeepalive.Options) {
	grpcOptions := s.grpcServerOptions(options)
	s.grpcServer = grpc.NewServer(grpcOptions...)
	s.EnvoyXdsServer.Register(s.grpcServer)
}

// initialize DNS server listener - uses the same certs as gRPC
func (s *Server) initDNSTLSListener(dns string) error {
	if dns == "" {
		return nil
	}
	certDir := dnsCertDir

	key := path.Join(certDir, constants.KeyFilename)
	cert := path.Join(certDir, constants.CertChainFilename)

	certP, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return err
	}

	cp := x509.NewCertPool()
	rootCertBytes := s.ca.GetCAKeyCertBundle().GetRootCertPem()
	cp.AppendCertsFromPEM(rootCertBytes)

	// TODO: check if client certs can be used with coredns or others.
	// If yes - we may require or optionally use them
	cfg := &tls.Config{
		Certificates: []tls.Certificate{certP},
		ClientAuth:   tls.NoClientCert,
		ClientCAs:    cp,
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

// initialize secureGRPCServer - using DNS certs
func (s *Server) initSecureGrpcServerDNS(port string, keepalive *istiokeepalive.Options) error {
	certDir := dnsCertDir

	key := path.Join(certDir, constants.KeyFilename)
	cert := path.Join(certDir, constants.CertChainFilename)

	certP, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return err
	}

	cp := x509.NewCertPool()
	rootCertBytes := s.ca.GetCAKeyCertBundle().GetRootCertPem()
	cp.AppendCertsFromPEM(rootCertBytes)

	cfg := &tls.Config{
		Certificates: []tls.Certificate{certP},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    cp,
	}

	tlsCreds := credentials.NewTLS(cfg)
	// certs not ready yet.
	if err != nil {
		return err
	}

	// Default is 15012 - istio-agent relies on this as a default to distinguish what cert auth to expect
	dnsGrpc := fmt.Sprintf(":%s", port)

	// create secure grpc listener
	l, err := net.Listen("tcp", dnsGrpc)
	if err != nil {
		return err
	}
	s.GRPCDNSListener = l

	opts := s.grpcServerOptions(keepalive)
	opts = append(opts, grpc.Creds(tlsCreds))

	s.secureGRPCServerDNS = grpc.NewServer(opts...)
	s.EnvoyXdsServer.Register(s.secureGRPCServerDNS)

	s.addStartFunc(func(stop <-chan struct{}) error {
		go func() {
			<-stop
			s.secureGRPCServerDNS.Stop()
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

func (s *Server) addStartFunc(fn startFunc) {
	s.startFuncs = append(s.startFuncs, fn)
}

func (s *Server) waitForCacheSync(stop <-chan struct{}) bool {
	// TODO: remove dependency on k8s lib
	// TODO: set a limit, panic otherwise ( to not hide the error )
	if !cache.WaitForCacheSync(stop, func() bool {
		if s.kubeRegistry != nil {
			if !s.kubeRegistry.HasSynced() {
				return false
			}
		}
		if !s.configController.HasSynced() {
			return false
		}
		return true
	}) {
		log.Errorf("Failed waiting for cache sync")
		return false
	}

	return true
}

// initEventHandlers sets up event handlers for config and service updates
func (s *Server) initEventHandlers() error {
	// Flush cached discovery responses whenever services configuration change.
	serviceHandler := func(svc *model.Service, _ model.Event) {
		pushReq := &model.PushRequest{
			Full:              true,
			NamespacesUpdated: map[string]struct{}{svc.Attributes.Namespace: {}},
			ConfigsUpdated:    map[resource.GroupVersionKind]map[string]struct{}{model.ServiceEntryKind: {}},
			Reason:            []model.TriggerReason{model.ServiceUpdate},
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
			Full:              true,
			NamespacesUpdated: map[string]struct{}{si.Service.Attributes.Namespace: {}},
			// TODO: extend and set service instance type, so no need re-init push context
			ConfigsUpdated: map[resource.GroupVersionKind]map[string]struct{}{model.ServiceEntryKind: {}},
			Reason:         []model.TriggerReason{model.ServiceUpdate},
		})
	}
	if err := s.ServiceController().AppendInstanceHandler(instanceHandler); err != nil {
		return fmt.Errorf("append instance handler failed: %v", err)
	}

	// TODO(Nino-k): remove this case once incrementalUpdate is default
	if s.configController != nil {
		// TODO: changes should not trigger a full recompute of LDS/RDS/CDS/EDS
		// (especially mixerclient HTTP and quota)
		configHandler := func(old, curr model.Config, _ model.Event) {
			pushReq := &model.PushRequest{
				Full:           true,
				ConfigsUpdated: map[resource.GroupVersionKind]map[string]struct{}{curr.GroupVersionKind(): {}},
				Reason:         []model.TriggerReason{model.ConfigUpdate},
			}
			s.EnvoyXdsServer.ConfigUpdate(pushReq)
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

			s.configController.RegisterEventHandler(schema.Resource().GroupVersionKind(), configHandler)
		}
	}

	return nil
}

// add a GRPC listener using DNS-based certificates. Will be used for Galley, injection and CA signing.
func (s *Server) initDNSListener(args *PilotArgs) error {
	istiodAddr := features.IstiodService.Get()
	if istiodAddr == "" {
		// Feature disabled
		return nil
	}
	if s.ca == nil {
		// Running locally without configured certs - no TLS mode
		return nil
	}

	// validate
	host, port, err := net.SplitHostPort(istiodAddr)
	if err != nil {
		return fmt.Errorf("invalid ISTIOD_ADDR(%s): %v", istiodAddr, err)
	}
	if _, err := strconv.Atoi(port); err != nil {
		return fmt.Errorf("invalid ISTIOD_ADDR(%s): %v", istiodAddr, err)
	}

	// Create DNS certificates. This allows injector, validation to work without Citadel, and
	// allows secure SDS connections to Istiod.
	err = s.initDNSCerts(host, args.Namespace)
	if err != nil {
		return err
	}

	// run secure grpc server for Istiod - using DNS-based certs from K8S
	err = s.initSecureGrpcServerDNS(port, args.KeepaliveOptions)
	if err != nil {
		return err
	}

	return nil
}

func (s *Server) initLeaderElection(args *PilotArgs) {
	if s.kubeClient != nil {
		s.leaderElection = leaderelection.NewLeaderElection(args.Namespace, args.PodName, s.kubeClient)
	}
}
