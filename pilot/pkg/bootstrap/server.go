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
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"sync"
	"time"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/soheilhy/cmux"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	kubesecrets "istio.io/istio/pilot/pkg/secrets/kube"
	"istio.io/istio/pilot/pkg/server"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pilot/pkg/status"
	tb "istio.io/istio/pilot/pkg/trustbundle"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
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

// DefaultPlugins is the default list of plugins to enable, when no plugin(s)
// is specified through the command line
var DefaultPlugins = []string{
	plugin.AuthzCustom,
	plugin.Authn,
	plugin.Authz,
}

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

	clusterID   string
	environment *model.Environment

	kubeRestConfig *rest.Config
	kubeClient     kubelib.Client

	multicluster *kubecontroller.Multicluster

	configController  model.ConfigStoreCache
	ConfigStores      []model.ConfigStoreCache
	serviceEntryStore *serviceentry.ServiceEntryStore

	httpServer       *http.Server // debug, monitoring and readiness Server.
	httpsServer      *http.Server // webhooks HTTPS Server.
	httpsReadyClient *http.Client

	grpcServer       *grpc.Server
	secureGrpcServer *grpc.Server

	// monitoringMux listens on monitoringAddr(:15014).
	// Currently runs prometheus monitoring and debug (if enabled).
	monitoringMux *http.ServeMux

	// httpMux listens on the httpAddr (8080).
	// If a Gateway is used in front and https is off it is also multiplexing
	// the rest of the features if their port is empty.
	// Currently runs readiness and debug (if enabled)
	httpMux *http.ServeMux

	// httpsMux listens on the httpsAddr(15017), handling webhooks
	// If the address os empty, the webhooks will be set on the default httpPort.
	httpsMux *http.ServeMux // webhooks

	HTTPListener       net.Listener
	HTTP2Listener      net.Listener
	GRPCListener       net.Listener
	SecureGrpcListener net.Listener

	// fileWatcher used to watch mesh config, networks and certificates.
	fileWatcher filewatcher.FileWatcher

	certController *chiron.WebhookController
	CA             *ca.IstioCA
	RA             ra.RegistrationAuthority

	// TrustAnchors for workload to workload mTLS
	workloadTrustBundle *tb.TrustBundle
	// path to the caBundle that signs the DNS certs. This should be agnostic to provider.
	caBundlePath string
	certMu       sync.RWMutex
	istiodCert   *tls.Certificate
	server       server.Instance

	// requiredTerminations keeps track of components that should block server exit
	// if they are not stopped. This allows important cleanup tasks to be completed.
	// Note: this is still best effort; a process can die at any time.
	readinessProbes map[string]readinessProbe

	// duration used for graceful shutdown.
	shutdownDuration time.Duration

	statusReporter *status.Reporter
	// RWConfigStore is the configstore which allows updates, particularly for status.
	RWConfigStore model.ConfigStoreCache
}

// NewServer creates a new Server instance based on the provided arguments.
func NewServer(args *PilotArgs) (*Server, error) {
	e := &model.Environment{
		PushContext:  model.NewPushContext(),
		DomainSuffix: args.RegistryOptions.KubeOptions.DomainSuffix,
	}
	e.SetLedger(buildLedger(args.RegistryOptions))

	ac := aggregate.NewController(aggregate.Options{
		MeshHolder: e,
	})
	e.ServiceDiscovery = ac

	s := &Server{
		clusterID:           getClusterID(args),
		environment:         e,
		fileWatcher:         filewatcher.NewWatcher(),
		httpMux:             http.NewServeMux(),
		monitoringMux:       http.NewServeMux(),
		readinessProbes:     make(map[string]readinessProbe),
		workloadTrustBundle: tb.NewTrustBundle(nil),
		server:              server.New(),
	}
	// Initialize workload Trust Bundle before XDS Server
	e.TrustBundle = s.workloadTrustBundle
	s.XDSServer = xds.NewDiscoveryServer(e, args.Plugins, args.PodName, args.Namespace)

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

	// used for both initKubeRegistry and initClusterRegistreis
	if features.EnableEndpointSliceController {
		args.RegistryOptions.KubeOptions.EndpointMode = kubecontroller.EndpointSliceOnly
	} else {
		args.RegistryOptions.KubeOptions.EndpointMode = kubecontroller.EndpointsOnly
	}

	prometheus.EnableHandlingTimeHistogram()

	// Apply the arguments to the configuration.
	if err := s.initKubeClient(args); err != nil {
		return nil, fmt.Errorf("error initializing kube client: %v", err)
	}

	s.initMeshConfiguration(args, s.fileWatcher)
	spiffe.SetTrustDomain(s.environment.Mesh().GetTrustDomain())

	s.initMeshNetworks(args, s.fileWatcher)
	s.initMeshHandlers()
	s.environment.Init()

	// Options based on the current 'defaults' in istio.
	caOpts := &caOptions{
		TrustDomain:    s.environment.Mesh().TrustDomain,
		Namespace:      args.Namespace,
		ExternalCAType: ra.CaExternalType(externalCaType),
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
	s.initRegistryEventHandlers()

	s.initDiscoveryService(args)

	s.initSDSServer(args)

	// Notice that the order of authenticators matters, since at runtime
	// authenticators are activated sequentially and the first successful attempt
	// is used as the authentication result.
	// The JWT authenticator requires the multicluster registry to be initialized, so we build this later
	authenticators := []security.Authenticator{
		&authenticate.ClientCertAuthenticator{},
		kubeauth.NewKubeJWTAuthenticator(s.kubeClient, s.clusterID, s.multicluster.GetRemoteKubeClient, spiffe.GetTrustDomain(), features.JwtPolicy.Get()),
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

	s.addReadinessProbe("discovery", func() (bool, error) {
		return s.XDSServer.IsServerReady(), nil
	})

	return s, nil
}

func initOIDC(args *PilotArgs, trustDomain string) (security.Authenticator, error) {
	// JWTRule is from the JWT_RULE environment variable.
	// An example of json string for JWTRule is:
	//`{"issuer": "foo", "jwks_uri": "baz", "audiences": ["aud1", "aud2"]}`.
	jwtRule := v1beta1.JWTRule{}
	err := json.Unmarshal([]byte(args.JwtRule), &jwtRule)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JWT rule: %v", err)
	}
	log.Infof("Istiod authenticating using JWTRule: %v", jwtRule)
	jwtAuthn, err := authenticate.NewJwtAuthenticator(&jwtRule, trustDomain)
	if err != nil {
		return nil, fmt.Errorf("failed to create the JWT authenticator: %v", err)
	}
	return jwtAuthn, nil
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
	if errors.Is(err, cmux.ErrListenerClosed) {
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
	if s.SecureGrpcListener != nil {
		go func() {
			log.Infof("starting secure gRPC discovery service at %s", s.SecureGrpcListener.Addr())
			if err := s.secureGrpcServer.Serve(s.SecureGrpcListener); err != nil {
				log.Errorf("error serving secure GRPC server: %v", err)
			}
		}()
	}

	if s.GRPCListener != nil {
		go func() {
			log.Infof("starting gRPC discovery service at %s", s.GRPCListener.Addr())
			if err := s.grpcServer.Serve(s.GRPCListener); err != nil {
				log.Errorf("error serving GRPC server: %v", err)
			}
		}()
	}

	// At this point we are ready - start Http Listener so that it can respond to readiness events.
	go func() {
		log.Infof("starting Http service at %s", s.HTTPListener.Addr())
		if err := s.httpServer.Serve(s.HTTPListener); isUnexpectedListenerError(err) {
			log.Errorf("error serving http server: %v", err)
		}
	}()

	if s.HTTP2Listener != nil {
		go func() {
			log.Infof("starting Http2 muxed service at %s", s.HTTP2Listener.Addr())
			h2s := &http2.Server{}
			h1s := &http.Server{
				Addr:    ":8080",
				Handler: h2c.NewHandler(s.httpMux, h2s),
			}
			if err := h1s.Serve(s.HTTP2Listener); isUnexpectedListenerError(err) {
				log.Errorf("error serving http server: %v", err)
			}
		}()
	}

	if s.httpsServer != nil {
		go func() {
			log.Infof("starting webhook service at %s", s.HTTPListener.Addr())
			if err := s.httpsServer.ListenAndServeTLS("", ""); isUnexpectedListenerError(err) {
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
func (s *Server) initSDSServer(args *PilotArgs) {
	if s.kubeClient != nil {
		if !features.EnableXDSIdentityCheck {
			// Make sure we have security
			log.Warnf("skipping Kubernetes credential reader; PILOT_ENABLE_XDS_IDENTITY_CHECK must be set to true for this feature.")
		} else {
			// TODO move this to a startup function and pass stop
			sc := kubesecrets.NewMulticluster(s.kubeClient, s.clusterID, args.RegistryOptions.ClusterRegistriesNamespace, make(chan struct{}))
			sc.AddEventHandler(func(name, namespace string) {
				s.XDSServer.ConfigUpdate(&model.PushRequest{
					Full: false,
					ConfigsUpdated: map[model.ConfigKey]struct{}{
						{
							Kind:      gvk.Secret,
							Name:      name,
							Namespace: namespace,
						}: {},
					},
					Reason: []model.TriggerReason{model.SecretTrigger},
				})
			})
			s.XDSServer.Generators[v3.SecretType] = xds.NewSecretGen(sc, s.XDSServer.Cache)
		}
	}
}

// initKubeClient creates the k8s client if running in an k8s environment.
// This is determined by the presence of a kube registry, which
// uses in-context k8s, or a config source of type k8s.
func (s *Server) initKubeClient(args *PilotArgs) error {
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
				}
			}
		} else if args.RegistryOptions.KubeConfig != "" {
			hasK8SConfigStore = true
		}
	}

	if hasK8SConfigStore || hasKubeRegistry(args.RegistryOptions.Registries) {
		var err error
		// Used by validation
		s.kubeRestConfig, err = kubelib.DefaultRestConfig(args.RegistryOptions.KubeConfig, "", func(config *rest.Config) {
			config.QPS = args.RegistryOptions.KubeOptions.KubernetesAPIQPS
			config.Burst = args.RegistryOptions.KubeOptions.KubernetesAPIBurst
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
	w.WriteHeader(http.StatusOK)
}

// initIstiodAdminServer initializes monitoring, debug and readiness end points.
func (s *Server) initIstiodAdminServer(args *PilotArgs, wh *inject.Webhook) error {
	s.httpServer = &http.Server{
		Addr:    args.ServerOptions.HTTPAddr,
		Handler: s.httpMux,
	}

	// create http listener
	listener, err := net.Listen("tcp", args.ServerOptions.HTTPAddr)
	if err != nil {
		return err
	}

	shouldMultiplex := args.ServerOptions.MonitoringAddr == ""

	if shouldMultiplex {
		s.monitoringMux = s.httpMux
		log.Info("initializing Istiod admin server multiplexed on httpAddr ", listener.Addr())
	} else {
		log.Info("initializing Istiod admin server")
	}

	whc := func() map[string]string {
		return wh.Config.Templates
	}

	// Debug Server.
	s.XDSServer.InitDebug(s.monitoringMux, s.ServiceController(), args.ServerOptions.EnableProfiling, whc)

	// Debug handlers are currently added on monitoring mux and readiness mux.
	// If monitoring addr is empty, the mux is shared and we only add it once on the shared mux .
	if !shouldMultiplex {
		s.XDSServer.AddDebugHandlers(s.httpMux, args.ServerOptions.EnableProfiling, whc)
	}

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
func (s *Server) initDiscoveryService(args *PilotArgs) {
	log.Infof("starting discovery service")
	// Implement EnvoyXdsServer grace shutdown
	s.addStartFunc(func(stop <-chan struct{}) error {
		log.Infof("Starting ADS server")
		s.XDSServer.Start(stop)
		return nil
	})

	s.initGrpcServer(args.KeepaliveOptions)

	if args.ServerOptions.GRPCAddr != "" {
		grpcListener, err := net.Listen("tcp", args.ServerOptions.GRPCAddr)
		if err != nil {
			log.Warnf("Failed to listen on gRPC port %v", err)
		}
		s.GRPCListener = grpcListener
	} else if s.GRPCListener == nil {
		// This happens only if the GRPC port (15010) is disabled. We will multiplex
		// it on the HTTP port. Does not impact the HTTPS gRPC or HTTPS.
		log.Info("multiplexing gRPC on http port ", s.HTTPListener.Addr())
		m := cmux.New(s.HTTPListener)
		s.GRPCListener = m.Match(cmux.HTTP2HeaderField("content-type", "application/grpc"))
		s.HTTP2Listener = m.Match(cmux.HTTP2())
		s.HTTPListener = m.Match(cmux.Any())
		go func() {
			if err := m.Serve(); isUnexpectedListenerError(err) {
				log.Warnf("Failed to listen on multiplexed port %v", err)
			}
		}()

	}
}

// Wait for the stop, and do cleanups
func (s *Server) waitForShutdown(stop <-chan struct{}) {
	go func() {
		<-stop
		s.fileWatcher.Close()

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
	grpcOptions := s.grpcServerOptions(options)
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
	}

	tlsCreds := credentials.NewTLS(cfg)

	// create secure grpc listener
	l, err := net.Listen("tcp", args.ServerOptions.SecureGRPCAddr)
	if err != nil {
		return err
	}
	s.SecureGrpcListener = l

	opts := s.grpcServerOptions(args.KeepaliveOptions)
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

func (s *Server) grpcServerOptions(options *istiokeepalive.Options) []grpc.ServerOption {
	interceptors := []grpc.UnaryServerInterceptor{
		// setup server prometheus monitoring (as final interceptor in chain)
		prometheus.UnaryServerInterceptor,
	}

	// Temp setting, default should be enough for most supported environments. Can be used for testing
	// envoy with lower values.
	maxStreams := features.MaxConcurrentStreams
	maxRecvMsgSize := features.MaxRecvMsgSize

	log.Infof("using max conn age of %v", options.MaxServerConnectionAge)
	grpcOptions := []grpc.ServerOption{
		grpc.UnaryInterceptor(middleware.ChainUnaryServer(interceptors...)),
		grpc.MaxConcurrentStreams(uint32(maxStreams)),
		grpc.MaxRecvMsgSize(maxRecvMsgSize),
		// Ensure we allow clients sufficient ability to send keep alives. If this is higher than client
		// keep alive setting, it will prematurely get a GOAWAY sent.
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime: options.Time / 2,
		}),
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
	if !cache.WaitForCacheSync(stop, s.cachesSynced) {
		log.Errorf("Failed waiting for cache sync")
		return false
	}
	// At this point, we know that all update events of the initial state-of-the-world have been
	// received. Capture how many updates there are
	expected := s.XDSServer.InboundUpdates.Load()
	// Now, we wait to ensure we have committed at least this many updates. This avoids a race
	// condition where we are marked ready prior to updating the push context, leading to incomplete
	// pushes.
	if !cache.WaitForCacheSync(stop, func() bool {
		return s.XDSServer.CommittedUpdates.Load() >= expected
	}) {
		log.Errorf("Failed waiting for push context initialization")
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
func (s *Server) initRegistryEventHandlers() {
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
		s.XDSServer.ConfigUpdate(pushReq)
	}
	s.ServiceController().AppendServiceHandler(serviceHandler)

	if s.configController != nil {
		configHandler := func(old config.Config, curr config.Config, event model.Event) {
			pushReq := &model.PushRequest{
				Full: true,
				ConfigsUpdated: map[model.ConfigKey]struct{}{{
					Kind:      curr.GroupVersionKind,
					Name:      curr.Name,
					Namespace: curr.Namespace,
				}: {}},
				Reason: []model.TriggerReason{model.ConfigUpdate},
			}
			s.XDSServer.ConfigUpdate(pushReq)
			if event != model.EventDelete {
				s.statusReporter.AddInProgressResource(curr)
			} else {
				s.statusReporter.DeleteInProgressResource(curr)
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
			if schema.Resource().GroupVersionKind() == collections.IstioNetworkingV1Alpha3Workloadgroups.
				Resource().GroupVersionKind() {
				continue
			}

			s.configController.RegisterEventHandler(schema.Resource().GroupVersionKind(), configHandler)
		}
	}
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
	if hasCustomTLSCerts(args.ServerOptions.TLSOptions) {
		// Use the DNS certificate provided via args.
		// This allows injector, validation to work without Citadel, and
		// allows secure SDS connections to Istiod.
		return nil
	}
	if !s.EnableCA() && features.PilotCertProvider.Get() == IstiodCAProvider {
		// If CA functionality is disabled, istiod cannot sign the DNS certificates.
		return nil
	}
	log.Infof("initializing Istiod DNS certificates host: %s, custom host: %s", host, features.IstiodServiceCustomHost.Get())
	if err := s.initDNSCerts(host, features.IstiodServiceCustomHost.Get(), args.Namespace); err != nil {
		return err
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

// createPeerCertVerifier creates a SPIFFE certificate verifier with the current istiod configuration.
func (s *Server) createPeerCertVerifier(tlsOptions TLSOptions) (*spiffe.PeerCertVerifier, error) {
	if tlsOptions.CaCertFile == "" && s.CA == nil && features.SpiffeBundleEndpoints == "" {
		// Running locally without configured certs - no TLS mode
		return nil, nil
	}
	peerCertVerifier := spiffe.NewPeerCertVerifier()
	var rootCertBytes []byte
	var err error
	if tlsOptions.CaCertFile != "" {
		if rootCertBytes, err = ioutil.ReadFile(tlsOptions.CaCertFile); err != nil {
			return nil, err
		}
	} else {
		if s.RA != nil {
			rootCertBytes = append(rootCertBytes, s.RA.GetCAKeyCertBundle().GetRootCertPem()...)
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
func (s *Server) getIstiodCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.certMu.RLock()
	defer s.certMu.RUnlock()
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

// maybeCreateCA creates and initializes CA Key if needed.
func (s *Server) maybeCreateCA(caOpts *caOptions) error {
	// CA signing certificate must be created only if CA is enabled.
	if s.EnableCA() {
		log.Info("creating CA and initializing public key")
		var err error
		var corev1 v1.CoreV1Interface
		if s.kubeClient != nil {
			corev1 = s.kubeClient.CoreV1()
		}
		if useRemoteCerts.Get() {
			if err = s.loadRemoteCACerts(caOpts, LocalCertDir.Get()); err != nil {
				return fmt.Errorf("failed to load remote CA certs: %v", err)
			}
		}
		// May return nil, if the CA is missing required configs - This is not an error.

		// TODO: Issue #27606 If External CA is configured, use that to sign DNS Certs as well. IstioCA need not be initialized
		if s.CA, err = s.createIstioCA(corev1, caOpts); err != nil {
			return fmt.Errorf("failed to create CA: %v", err)
		}
		if caOpts.ExternalCAType != "" {
			if s.RA, err = s.createIstioRA(s.kubeClient, caOpts); err != nil {
				return fmt.Errorf("failed to create RA: %v", err)
			}
		}
		if err = s.initPublicKey(); err != nil {
			return fmt.Errorf("error initializing public key: %v", err)
		}
	}
	return nil
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

func (s *Server) fetchCARoot() map[string]string {
	if s.CA == nil {
		return nil
	}
	return map[string]string{
		constants.CACertNamespaceConfigMapDataName: string(s.CA.GetCAKeyCertBundle().GetRootCertPem()),
	}
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
	s.environment.AddNetworksHandler(func() {
		s.XDSServer.ConfigUpdate(&model.PushRequest{
			Full:   true,
			Reason: []model.TriggerReason{model.GlobalUpdate},
		})
	})
}

func (s *Server) addIstioCAToTrustBundle(args *PilotArgs) error {
	// TODO: unify with CreateIstioCA instead of mirroring it

	var err error
	if s.CA != nil {
		// If IstioCA is setup, derive trustAnchor directly from CA
		rootCerts := []string{string(s.CA.GetCAKeyCertBundle().GetRootCertPem())}
		err = s.workloadTrustBundle.UpdateTrustAnchor(&tb.TrustAnchorUpdate{
			TrustAnchorConfig: tb.TrustAnchorConfig{Certs: rootCerts},
			Source:            tb.SourceIstioCA,
		})
		if err != nil {
			log.Errorf("unable to add CA root as trustAnchor")
			return err
		}
		return nil
	}

	// If CA is not running, derive root certificates from the configured CA secrets
	signingKeyFile := path.Join(LocalCertDir.Get(), "ca-key.pem")
	if _, err := os.Stat(signingKeyFile); err != nil && s.kubeClient != nil {
		// Fetch self signed certificates
		caSecret, err := s.kubeClient.CoreV1().Secrets(args.Namespace).
			Get(context.TODO(), ca.CASecret, metav1.GetOptions{})
		if err != nil {
			log.Errorf("unable to retrieve self signed CA secret: %v", err)
			return err
		}
		rootCertBytes, ok := caSecret.Data[ca.RootCertID]
		if !ok {
			rootCertBytes = caSecret.Data[ca.CaCertID]
		}
		err = s.workloadTrustBundle.UpdateTrustAnchor(&tb.TrustAnchorUpdate{
			TrustAnchorConfig: tb.TrustAnchorConfig{Certs: []string{string(rootCertBytes)}},
			Source:            tb.SourceIstioCA,
		})
		if err != nil {
			log.Errorf("unable to update trustbundle with self signed CA root: %v", err)
			return err
		}
	} else {
		// If NOT self signed certificates
		rootCertFile := path.Join(LocalCertDir.Get(), "root-cert.pem")
		if _, err := os.Stat(rootCertFile); err != nil {
			rootCertFile = path.Join(LocalCertDir.Get(), "ca-cert.pem")
		}
		certBytes, err := ioutil.ReadFile(rootCertFile)
		if err != nil {
			if s.kubeClient != nil {
				return err
			}
			// TODO: accommodation for unit tests. needs to be removed
			return nil
		}
		err = s.workloadTrustBundle.UpdateTrustAnchor(&tb.TrustAnchorUpdate{
			TrustAnchorConfig: tb.TrustAnchorConfig{Certs: []string{string(certBytes)}},
			Source:            tb.SourceIstioCA,
		})
		if err != nil {
			log.Errorf("unable to update trustbundle with plugin CA root: %v", err)
			return err
		}
	}
	return nil
}

func (s *Server) initWorkloadTrustBundle(args *PilotArgs) error {
	var err error

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
