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
	"io/ioutil"
	"net"
	"os"
	"path"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
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
)

// Server is an in-memory Mixer service.
type Server struct {
	shutdown  chan error
	server    *grpc.Server
	gp        *pool.GoroutinePool
	adapterGP *pool.GoroutinePool
	listener  net.Listener
	monitor   *monitor
	tracer    *mixerTracer
	configDir string

	dispatcher mixerRuntime.Dispatcher
}

// replaceable set of functions for fault injection
type patchTable struct {
	newILEvaluator func(cacheSize int) (*evaluator.IL, error)
	newStore2      func(r2 *store.Registry2, configURL string) (store.Store2, error)
	newRuntime     func(eval expr.Evaluator, typeChecker expr.TypeChecker, vocab mixerRuntime.VocabularyChangeListener,
		gp *pool.GoroutinePool, handlerPool *pool.GoroutinePool,
		identityAttribute string, defaultConfigNamespace string, s store.Store2, adapterInfo map[string]*adapter.Info,
		templateInfo map[string]template.Info) (mixerRuntime.Dispatcher, error)
	startTracer  func(zipkinURL string, jaegerURL string, logTraceSpans bool) (*mixerTracer, grpc.UnaryServerInterceptor, error)
	startMonitor func(port uint16) (*monitor, error)
	listen       func(network string, address string) (net.Listener, error)
}

// New instantiates a fully functional Mixer server, ready for traffic.
func New(a *Args) (*Server, error) {
	return newServer(a, newPatchTable())
}

func newPatchTable() *patchTable {
	return &patchTable{
		newILEvaluator: evaluator.NewILEvaluator,
		newStore2:      func(r2 *store.Registry2, configURL string) (store.Store2, error) { return r2.NewStore2(configURL) },
		newRuntime:     mixerRuntime.New,
		startTracer:    startTracer,
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

	// construct the gRPC options

	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(uint32(a.MaxConcurrentStreams)))
	grpcOptions = append(grpcOptions, grpc.MaxMsgSize(int(a.MaxMessageSize)))

	var interceptors []grpc.UnaryServerInterceptor

	if a.EnableTracing() {
		var interceptor grpc.UnaryServerInterceptor

		if s.tracer, interceptor, err = p.startTracer(a.ZipkinURL, a.JaegerURL, a.LogTraceSpans); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("unable to setup ZipKin: %v", err)
		}

		if interceptor != nil {
			interceptors = append(interceptors, interceptor)
		}
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

	configStore2URL := a.ConfigStore2URL
	if configStore2URL == "" {
		configStore2URL = "k8s://"
	}

	if a.ServiceConfig != "" || a.GlobalConfig != "" {
		if s.configDir, err = serializeConfigs(a.GlobalConfig, a.ServiceConfig); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("unable to serialize supplied configuration state: %v", err)
		}
		configStore2URL = "fs://" + s.configDir
	}

	reg2 := store.NewRegistry2(config.Store2Inventory()...)
	store2, err := p.newStore2(reg2, configStore2URL)
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("unable to connect to the configuration server: %v", err)
	}

	var dispatcher mixerRuntime.Dispatcher
	if dispatcher, err = p.newRuntime(eval, evaluator.NewTypeChecker(), eval, s.gp, s.adapterGP,
		a.ConfigIdentityAttribute, a.ConfigDefaultNamespace, store2, adapterMap, a.Templates); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("unable to create runtime dispatcherForTesting: %v", err)
	}
	s.dispatcher = dispatcher

	// get the grpc server wired up
	grpc.EnableTracing = a.EnableGRPCTracing
	s.server = grpc.NewServer(grpcOptions...)
	mixerpb.RegisterMixerServer(s.server, api.NewGRPCServer(dispatcher, s.gp))

	return s, nil
}

// Takes the string-based configs and creates a directory with config files from it.
func serializeConfigs(globalConfig string, serviceConfig string) (string, error) {
	configDir, err := ioutil.TempDir("", "mixer")
	if err == nil {
		s := path.Join(configDir, "service.yaml")
		if err = ioutil.WriteFile(s, []byte(serviceConfig), 0666); err == nil {
			g := path.Join(configDir, "global.yaml")
			if err = ioutil.WriteFile(g, []byte(globalConfig), 0666); err == nil {
				return configDir, nil
			}
		}

		_ = os.RemoveAll(configDir)
	}

	return "", err
}

// Run enables Mixer to start receiving gRPC requests on its main API port.
func (s *Server) Run() {
	s.shutdown = make(chan error, 1)
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
		s.Wait()
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
