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

package server

import (
	"fmt"
	"io"
	"net"
	"os"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	ot "github.com/opentracing/opentracing-go"
	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/api"
	"istio.io/istio/mixer/pkg/config"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/il/evaluator"
	"istio.io/istio/mixer/pkg/pool"
	mixerRuntime "istio.io/istio/mixer/pkg/runtime"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/pkg/tracing"
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
	configDir string

	dispatcher mixerRuntime.Dispatcher

	// probes
	livenessProbe  probe.Controller
	readinessProbe probe.Controller
	*probe.Probe
}

// replaceable set of functions for fault injection
type patchTable struct {
	newILEvaluator func(cacheSize int) (*evaluator.IL, error)
	newStore       func(r2 *store.Registry, configURL string) (store.Store, error)
	newRuntime     func(eval expr.Evaluator, typeChecker expr.TypeChecker, vocab mixerRuntime.VocabularyChangeListener,
		gp *pool.GoroutinePool, handlerPool *pool.GoroutinePool,
		identityAttribute string, defaultConfigNamespace string, s store.Store, adapterInfo map[string]*adapter.Info,
		templateInfo map[string]template.Info) (mixerRuntime.Dispatcher, error)
	configTracing func(serviceName string, options *tracing.Options) (io.Closer, error)
	startMonitor  func(port uint16) (*monitor, error)
	listen        func(network string, address string) (net.Listener, error)
}

// New instantiates a fully functional Mixer server, ready for traffic.
func New(a *Args) (*Server, error) {
	return newServer(a, newPatchTable())
}

func newPatchTable() *patchTable {
	return &patchTable{
		newILEvaluator: evaluator.NewILEvaluator,
		newStore:       func(r2 *store.Registry, configURL string) (store.Store, error) { return r2.NewStore(configURL) },
		newRuntime:     mixerRuntime.New,
		configTracing:  tracing.Configure,
		startMonitor:   startMonitor,
		listen:         net.Listen,
	}
}

func newServer(a *Args, p *patchTable) (*Server, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}

	if err := log.Configure(a.LoggingOptions); err != nil {
		return nil, err
	}

	eval, err := p.newILEvaluator(a.ExpressionEvalCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create IL expression evaluator with cache size %d: %v", a.ExpressionEvalCacheSize, err)
	}

	apiPoolSize := a.APIWorkerPoolSize
	adapterPoolSize := a.AdapterWorkerPoolSize

	s := &Server{}
	s.gp = pool.NewGoroutinePool(apiPoolSize, a.SingleThreaded)
	s.gp.AddWorkers(apiPoolSize)

	s.adapterGP = pool.NewGoroutinePool(adapterPoolSize, a.SingleThreaded)
	s.adapterGP.AddWorkers(adapterPoolSize)

	tmplRepo := template.NewRepository(a.Templates)
	adapterMap := config.AdapterInfoMap(a.Adapters, tmplRepo.SupportsTemplate)

	s.Probe = probe.NewProbe()

	// construct the gRPC options

	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(uint32(a.MaxConcurrentStreams)))
	grpcOptions = append(grpcOptions, grpc.MaxMsgSize(int(a.MaxMessageSize)))

	var interceptors []grpc.UnaryServerInterceptor

	if a.TracingOptions.TracingEnabled() {
		s.tracer, err = p.configTracing("istio-mixer", a.TracingOptions)
		if err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("unable to setup tracing")
		}
		interceptors = append(interceptors, otgrpc.OpenTracingServerInterceptor(ot.GlobalTracer()))
	}

	// setup server prometheus monitoring (as final interceptor in chain)
	interceptors = append(interceptors, grpc_prometheus.UnaryServerInterceptor)
	grpc_prometheus.EnableHandlingTimeHistogram()
	grpcOptions = append(grpcOptions, grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(interceptors...)))

	if s.monitor, err = p.startMonitor(a.MonitoringPort); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("unable to setup monitoring: %v", err)
	}

	// get the network stuff setup
	if s.listener, err = p.listen("tcp", fmt.Sprintf(":%d", a.APIPort)); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("unable to listen on socket: %v", err)
	}

	st := a.ConfigStore
	if st != nil && a.ConfigStoreURL != "" {
		return nil, fmt.Errorf("invalid arguments: both ConfigStore and ConfigStoreURL are specified")
	}
	if st == nil {
		configStoreURL := a.ConfigStoreURL
		if configStoreURL == "" {
			configStoreURL = "k8s://"
		}

		reg := store.NewRegistry(config.StoreInventory()...)
		if st, err = p.newStore(reg, configStoreURL); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("unable to connect to the configuration server: %v", err)
		}
	}

	var dispatcher mixerRuntime.Dispatcher
	if dispatcher, err = p.newRuntime(eval, evaluator.NewTypeChecker(), eval, s.gp, s.adapterGP,
		a.ConfigIdentityAttribute, a.ConfigDefaultNamespace, st, adapterMap, a.Templates); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("unable to create runtime dispatcherForTesting: %v", err)
	}
	s.dispatcher = dispatcher

	// get the grpc server wired up
	grpc.EnableTracing = a.EnableGRPCTracing
	s.server = grpc.NewServer(grpcOptions...)
	mixerpb.RegisterMixerServer(s.server, api.NewGRPCServer(dispatcher, s.gp))

	if a.LivenessProbeOptions.IsValid() {
		s.livenessProbe = probe.NewFileController(a.LivenessProbeOptions)
		s.RegisterProbe(s.livenessProbe, "server")
		s.livenessProbe.Start()
	}
	if a.ReadinessProbeOptions.IsValid() {
		s.readinessProbe = probe.NewFileController(a.ReadinessProbeOptions)
		if e, ok := s.dispatcher.(probe.SupportsProbe); ok {
			e.RegisterProbe(s.readinessProbe, "dispatcher")
		}
		st.RegisterProbe(s.readinessProbe, "store")
		s.readinessProbe.Start()
	}

	return s, nil
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
	if s.shutdown != nil {
		s.server.GracefulStop()
		_ = s.Wait()
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

	if s.configDir != "" {
		_ = os.RemoveAll(s.configDir)
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
func (s *Server) Dispatcher() mixerRuntime.Dispatcher {
	return s.dispatcher
}
