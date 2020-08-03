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

package server

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"contrib.go.opencensus.io/exporter/prometheus"
	ot "github.com/opentracing/opentracing-go"
	oprometheus "github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats/view"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	accesslog "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v2"
	authz "github.com/envoyproxy/go-control-plane/envoy/service/auth/v2"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/api"
	"istio.io/istio/mixer/pkg/checkcache"
	"istio.io/istio/mixer/pkg/config"
	"istio.io/istio/mixer/pkg/config/crd"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/loadshedding"
	"istio.io/istio/mixer/pkg/runtime"
	runtimeconfig "istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/mixer/pkg/runtime/dispatcher"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/tracing"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/log"
	"istio.io/pkg/pool"
	"istio.io/pkg/probe"
)

// Server is an in-memory Mixer service.

type Server struct {
	shutdown  chan error
	server    *grpc.Server
	gp        *pool.GoroutinePool
	adapterGP *pool.GoroutinePool
	listener  net.Listener
	monitor   *monitor
	tracer    io.Closer

	checkCache *checkcache.Cache
	dispatcher dispatcher.Dispatcher
	controlZ   *ctrlz.Server

	// probes
	livenessProbe  probe.Controller
	readinessProbe probe.Controller
	*probe.Probe
	configStore store.Store
}

type listenFunc func(network string, address string) (net.Listener, error)

// replaceable set of functions for fault injection
type patchTable struct {
	newRuntime func(s store.Store, templates map[string]*template.Info, adapters map[string]*adapter.Info,
		defaultConfigNamespace string, executorPool *pool.GoroutinePool,
		handlerPool *pool.GoroutinePool, enableTracing bool,
		namespaces []string) *runtime.Runtime
	configTracing func(serviceName string, options *tracing.Options) (io.Closer, error)
	startMonitor  func(port uint16, enableProfiling bool, lf listenFunc) (*monitor, error)
	listen        listenFunc
	configLog     func(options *log.Options) error
	runtimeListen func(runtime *runtime.Runtime) error
	remove        func(name string) error

	// monitoring-related setup
	newOpenCensusExporter   func() (view.Exporter, error)
	registerOpenCensusViews func(...*view.View) error
}

// New instantiates a fully functional Mixer server, ready for traffic.
func New(a *Args) (*Server, error) {
	return newServer(a, newPatchTable())
}

func newPatchTable() *patchTable {
	return &patchTable{
		newRuntime:    runtime.New,
		configTracing: tracing.Configure,
		startMonitor:  startMonitor,
		listen:        net.Listen,
		configLog:     log.Configure,
		runtimeListen: func(rt *runtime.Runtime) error { return rt.StartListening() },
		remove:        os.Remove,
		newOpenCensusExporter: func() (view.Exporter, error) {
			return prometheus.NewExporter(prometheus.Options{Registry: oprometheus.DefaultRegisterer.(*oprometheus.Registry)})
		},
		registerOpenCensusViews: view.Register,
	}
}

func newServer(a *Args, p *patchTable) (server *Server, err error) {
	if err := a.validate(); err != nil {
		return nil, err
	}

	if err := p.configLog(a.LoggingOptions); err != nil {
		return nil, err
	}

	s := &Server{}

	defer func() {
		// If return with error, need to close the server.
		if err != nil {
			_ = s.Close()
		}
	}()

	s.gp = pool.NewGoroutinePool(a.APIWorkerPoolSize, a.SingleThreaded)
	s.gp.AddWorkers(a.APIWorkerPoolSize)

	s.adapterGP = pool.NewGoroutinePool(a.AdapterWorkerPoolSize, a.SingleThreaded)
	s.adapterGP.AddWorkers(a.AdapterWorkerPoolSize)

	tmplRepo := template.NewRepository(a.Templates)
	adapterMap := config.AdapterInfoMap(a.Adapters, tmplRepo.SupportsTemplate)

	s.Probe = probe.NewProbe()
	if a.LivenessProbeOptions.IsValid() {
		s.livenessProbe = probe.NewFileController(a.LivenessProbeOptions)
		s.RegisterProbe(s.livenessProbe, "server")
		s.livenessProbe.Start()
	}

	// construct the gRPC options

	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(uint32(a.MaxConcurrentStreams)), grpc.MaxRecvMsgSize(int(a.MaxMessageSize)))

	if a.TracingOptions.TracingEnabled() {
		s.tracer, err = p.configTracing("istio-mixer", a.TracingOptions)
		if err != nil {
			return nil, fmt.Errorf("unable to setup tracing")
		}
		grpcOptions = append(grpcOptions, grpc.UnaryInterceptor(TracingServerInterceptor(ot.GlobalTracer())))
	}

	// get the network stuff setup
	network, address := extractNetAddress(a.APIPort, a.APIAddress)

	if network == "unix" {
		// remove Unix socket before use.
		if err = p.remove(address); err != nil && !os.IsNotExist(err) {
			// Anything other than "file not found" is an error.
			return nil, fmt.Errorf("unable to remove unix://%s: %v", address, err)
		}
	}

	if s.listener, err = p.listen(network, address); err != nil {
		return nil, fmt.Errorf("unable to listen: %v", err)
	}

	st := a.ConfigStore
	if st == nil {
		configStoreURL := a.ConfigStoreURL
		if configStoreURL == "" {
			configStoreURL = "k8s://"
		}

		reg := store.NewRegistry(config.StoreInventory()...)
		groupVersion := &schema.GroupVersion{Group: crd.ConfigAPIGroup, Version: crd.ConfigAPIVersion}
		if st, err = reg.NewStore(configStoreURL, groupVersion, a.CredentialOptions, runtimeconfig.CriticalKinds()); err != nil {
			return nil, fmt.Errorf("unable to connect to the configuration server: %v", err)
		}
	}

	templateMap := make(map[string]*template.Info, len(a.Templates))
	for k, v := range a.Templates {
		t := v // Make a local copy, otherwise we end up capturing the location of the last entry
		templateMap[k] = &t
	}

	var configAdapterMap map[string]*adapter.Info
	if a.UseAdapterCRDs {
		configAdapterMap = adapterMap
	}
	var configTemplateMap map[string]*template.Info
	if a.UseTemplateCRDs {
		configTemplateMap = templateMap
	}

	kinds := runtimeconfig.KindMap(configAdapterMap, configTemplateMap)

	if err := st.Init(kinds); err != nil {
		return nil, fmt.Errorf("unable to initialize config store: %v", err)
	}

	var namespaces []string

	if a.WatchedNamespaces != "" {
		namespaces = strings.Split(a.WatchedNamespaces, ",")
		namespaces = append(namespaces, a.ConfigDefaultNamespace)
	} else {
		namespaces = []string{metav1.NamespaceAll}
	}

	// block wait for the config store to sync
	log.Info("Awaiting for config store sync...")
	if err := st.WaitForSynced(a.ConfigWaitTimeout); err != nil {
		return nil, err
	}
	s.configStore = st
	log.Info("Starting runtime config watch...")
	rt := p.newRuntime(st, templateMap, adapterMap, a.ConfigDefaultNamespace,
		s.gp, s.adapterGP, a.TracingOptions.TracingEnabled(), namespaces)

	if err = p.runtimeListen(rt); err != nil {
		return nil, fmt.Errorf("unable to listen: %v", err)
	}

	s.dispatcher = rt.Dispatcher()

	// see issue https://github.com/istio/istio/issues/9596
	a.NumCheckCacheEntries = 0

	if a.NumCheckCacheEntries > 0 {
		s.checkCache = checkcache.New(a.NumCheckCacheEntries)
	}

	// get the grpc server wired up
	grpc.EnableTracing = a.EnableGRPCTracing

	exporter, err := p.newOpenCensusExporter()
	if err != nil {
		return nil, fmt.Errorf("could not build opencensus exporter: %v", err)
	}
	view.RegisterExporter(exporter)

	// Register the views to collect server request count.
	if err := p.registerOpenCensusViews(ocgrpc.DefaultServerViews...); err != nil {
		return nil, fmt.Errorf("could not register default server views: %v", err)
	}

	throttler := loadshedding.NewThrottler(a.LoadSheddingOptions)
	if eval := throttler.Evaluator(loadshedding.GRPCLatencyEvaluatorName); eval != nil {
		grpcOptions = append(grpcOptions, grpc.StatsHandler(newMultiStatsHandler(&ocgrpc.ServerHandler{}, eval.(*loadshedding.GRPCLatencyEvaluator))))
	} else {
		grpcOptions = append(grpcOptions, grpc.StatsHandler(&ocgrpc.ServerHandler{}))
	}

	s.server = grpc.NewServer(grpcOptions...)
	mixerpb.RegisterMixerServer(s.server, api.NewGRPCServer(s.dispatcher, s.gp, s.checkCache, throttler))
	envoyServer := api.NewGRPCServerEnvoy(s.dispatcher, s.gp, s.checkCache, throttler)
	authz.RegisterAuthorizationServer(s.server, envoyServer)
	accesslog.RegisterAccessLogServiceServer(s.server, envoyServer)

	if a.ReadinessProbeOptions.IsValid() {
		s.readinessProbe = probe.NewFileController(a.ReadinessProbeOptions)
		rt.RegisterProbe(s.readinessProbe, "dispatcher")
		st.RegisterProbe(s.readinessProbe, "store")
		s.readinessProbe.Start()
	}

	log.Info("Starting monitor server...")
	if s.monitor, err = p.startMonitor(a.MonitoringPort, a.EnableProfiling, p.listen); err != nil {
		return nil, fmt.Errorf("unable to setup monitoring: %v", err)
	}

	s.controlZ, _ = ctrlz.Run(a.IntrospectionOptions, nil)

	return s, nil
}

func extractNetAddress(apiPort uint16, apiAddress string) (string, string) {
	if apiAddress != "" {
		idx := strings.Index(apiAddress, "://")
		if idx < 0 {
			return "tcp", apiAddress
		}

		return apiAddress[:idx], apiAddress[idx+3:]
	}

	return "tcp", fmt.Sprintf(":%d", apiPort)
}

// Run enables Mixer to start receiving gRPC requests on its main API port.
func (s *Server) Run() {
	s.shutdown = make(chan error, 1)
	s.SetAvailable(nil)
	go func() {
		// go to work...
		err := s.server.Serve(s.listener)

		// notify closer we're done
		s.shutdown <- err
	}()
}

// Wait waits for the server to exit.
func (s *Server) Wait() error {
	if s.shutdown == nil {
		return fmt.Errorf("server not running")
	}

	err := <-s.shutdown
	s.shutdown = nil
	return err
}

// Close cleans up resources used by the server.
func (s *Server) Close() error {
	log.Info("Close server")

	if s.shutdown != nil {
		s.server.GracefulStop()
		_ = s.Wait()
	}

	if s.controlZ != nil {
		s.controlZ.Close()
	}

	if s.configStore != nil {
		s.configStore.Stop()
		s.configStore = nil
	}

	if s.checkCache != nil {
		_ = s.checkCache.Close()
	}

	if s.listener != nil {
		_ = s.listener.Close()
	}

	if s.tracer != nil {
		_ = s.tracer.Close()
	}

	if s.monitor != nil {
		_ = s.monitor.Close()
	}

	if s.gp != nil {
		_ = s.gp.Close()
	}

	if s.adapterGP != nil {
		_ = s.adapterGP.Close()
	}

	if s.livenessProbe != nil {
		_ = s.livenessProbe.Close()
	}

	if s.readinessProbe != nil {
		_ = s.readinessProbe.Close()
	}

	// final attempt to purge buffered logs
	_ = log.Sync()

	return nil
}

// Addr returns the address of the server's API port, where gRPC requests can be sent.
func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}

// Dispatcher returns the dispatcher that was created during server creation. This should only
// be used for testing purposes only.
func (s *Server) Dispatcher() dispatcher.Dispatcher {
	return s.dispatcher
}
