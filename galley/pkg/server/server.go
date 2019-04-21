// Copyright 2018 Istio Authors
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
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/meshconfig"
	"istio.io/istio/galley/pkg/metadata"
	kubeMeta "istio.io/istio/galley/pkg/metadata/kube"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/groups"
	"istio.io/istio/galley/pkg/source/fs"
	kubeSource "istio.io/istio/galley/pkg/source/kube"
	"istio.io/istio/galley/pkg/source/kube/client"
	"istio.io/istio/galley/pkg/source/kube/dynamic/converter"
	"istio.io/istio/galley/pkg/source/kube/schema"
	"istio.io/istio/galley/pkg/source/kube/schema/check"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/ctrlz/fw"
	"istio.io/istio/pkg/log"
	configz "istio.io/istio/pkg/mcp/configz/server"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/istio/pkg/mcp/monitoring"
	mcprate "istio.io/istio/pkg/mcp/rate"
	"istio.io/istio/pkg/mcp/server"
	"istio.io/istio/pkg/mcp/snapshot"
	"istio.io/istio/pkg/mcp/source"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/pkg/version"
)

var scope = log.RegisterScope("server", "Galley server debugging", 0)

// Server is the main entry point into the Galley code.
type Server struct {
	serveWG       sync.WaitGroup
	grpcServer    *grpc.Server
	processor     *runtime.Processor
	mcpSource     *source.Server
	reporter      monitoring.Reporter
	listenerMutex sync.Mutex
	listener      net.Listener
	controlZ      *ctrlz.Server
	stopCh        chan struct{}
	callOut       *callout
}

type patchTable struct {
	newKubeFromConfigFile       func(string) (client.Interfaces, error)
	verifyResourceTypesPresence func(client.Interfaces, []schema.ResourceSpec) ([]schema.ResourceSpec, error)
	findSupportedResources      func(client.Interfaces, []schema.ResourceSpec) ([]schema.ResourceSpec, error)
	newSource                   func(client.Interfaces, time.Duration, *schema.Instance, *converter.Config) (runtime.Source, error)
	netListen                   func(network, address string) (net.Listener, error)
	newMeshConfigCache          func(path string) (meshconfig.Cache, error)
	mcpMetricReporter           func(string) monitoring.Reporter
	fsNew                       func(string, *schema.Instance, *converter.Config) (runtime.Source, error)
}

func defaultPatchTable() patchTable {
	return patchTable{
		newKubeFromConfigFile:       client.NewKubeFromConfigFile,
		verifyResourceTypesPresence: check.ResourceTypesPresence,
		findSupportedResources:      check.FindSupportedResourceSchemas,
		newSource:                   kubeSource.New,
		netListen:                   net.Listen,
		mcpMetricReporter:           func(prefix string) monitoring.Reporter { return monitoring.NewStatsContext(prefix) },
		newMeshConfigCache:          func(path string) (meshconfig.Cache, error) { return meshconfig.NewCacheFromFile(path) },
		fsNew:                       fs.New,
	}
}

// New returns a new instance of a Server.
func New(a *Args) (*Server, error) {
	return newServer(a, defaultPatchTable())
}

func newServer(a *Args, p patchTable) (*Server, error) {
	var err error
	s := &Server{}

	defer func() {
		// If returns with error, need to close the server.
		if err != nil {
			_ = s.Close()
		}
	}()

	mesh, err := p.newMeshConfigCache(a.MeshConfigFile)
	if err != nil {
		return nil, err
	}
	converterCfg := &converter.Config{
		Mesh:         mesh,
		DomainSuffix: a.DomainSuffix,
	}

	sourceSchema := getSourceSchema(a)

	var src runtime.Source
	if a.ConfigPath != "" {
		src, err = p.fsNew(a.ConfigPath, sourceSchema, converterCfg)
		if err != nil {
			return nil, err
		}
	} else {
		k, err := p.newKubeFromConfigFile(a.KubeConfig)
		if err != nil {
			return nil, err
		}
		var found []schema.ResourceSpec

		if !a.DisableResourceReadyCheck {
			found, err = p.verifyResourceTypesPresence(k, sourceSchema.All())
		} else {
			found, err = p.findSupportedResources(k, sourceSchema.All())
		}
		if err != nil {
			return nil, err
		}
		sourceSchema = schema.New(found...)
		src, err = p.newSource(k, a.ResyncPeriod, sourceSchema, converterCfg)
		if err != nil {
			return nil, err
		}
	}

	processorCfg := runtime.Config{
		DomainSuffix: a.DomainSuffix,
		Mesh:         mesh,
	}
	distributor := snapshot.New(groups.IndexFunction)
	s.processor = runtime.NewProcessor(src, distributor, &processorCfg)

	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(uint32(a.MaxConcurrentStreams)), grpc.MaxRecvMsgSize(int(a.MaxReceivedMessageSize)))

	s.stopCh = make(chan struct{})
	var checker source.AuthChecker = server.NewAllowAllChecker()
	if !a.Insecure {
		checker, err = watchAccessList(s.stopCh, a.AccessListFile)
		if err != nil {
			return nil, err
		}

		watcher, err := creds.PollFiles(s.stopCh, a.CredentialOptions)
		if err != nil {
			return nil, err
		}
		credentials := creds.CreateForServer(watcher)

		grpcOptions = append(grpcOptions, grpc.Creds(credentials))
	}
	grpc.EnableTracing = a.EnableGRPCTracing
	s.grpcServer = grpc.NewServer(grpcOptions...)

	s.reporter = p.mcpMetricReporter("galley/mcp/source")

	options := &source.Options{
		Watcher:            distributor,
		Reporter:           s.reporter,
		CollectionsOptions: source.CollectionOptionsFromSlice(metadata.Types.Collections(), a.IncrementalResourceUpdate),
		ConnRateLimiter:    mcprate.NewRateLimiter(time.Second, 100), // TODO(Nino-K): https://github.com/istio/istio/issues/12074
	}

	if a.SinkAddress != "" {
		s.callOut, err = newCallout(a.SinkAddress, a.SinkAuthMode, a.SinkMeta, options)
		if err != nil {
			s.callOut = nil
			scope.Fatalf("Callout could not be initialized: %v", err)
		}
	}

	serverOptions := &source.ServerOptions{
		AuthChecker: checker,
		RateLimiter: rate.NewLimiter(rate.Every(time.Second), 100), // TODO(Nino-K): https://github.com/istio/istio/issues/12074
	}

	s.mcpSource = source.NewServer(options, serverOptions)

	// get the network stuff setup
	network := "tcp"
	var address string
	idx := strings.Index(a.APIAddress, "://")
	if idx < 0 {
		address = a.APIAddress
	} else {
		network = a.APIAddress[:idx]
		address = a.APIAddress[idx+3:]
	}

	if s.listener, err = p.netListen(network, address); err != nil {
		return nil, fmt.Errorf("unable to listen: %v", err)
	}

	mcp.RegisterResourceSourceServer(s.grpcServer, s.mcpSource)

	s.controlZ, _ = ctrlz.Run(a.IntrospectionOptions, []fw.Topic{configz.CreateTopic(distributor)})

	return s, nil
}

func getSourceSchema(a *Args) *schema.Instance {
	b := schema.NewBuilder()
	for _, spec := range kubeMeta.Types.All() {
		if !isKindExcluded(a, spec.Kind) {
			b.Add(spec)
		}
	}
	return b.Build()
}

func isKindExcluded(a *Args, kind string) bool {
	for _, excludedKind := range a.ExcludedResourceKinds {
		if kind == excludedKind {
			return true
		}
	}
	return false
}

// Run enables Galley to start receiving gRPC requests on its main API port.
func (s *Server) Run() {
	s.serveWG.Add(1)
	go func() {
		defer s.serveWG.Done()
		err := s.processor.Start()
		if err != nil {
			scope.Errorf("Galley Server unexpectedly terminated: %v", err)
			return
		}

		l := s.getListener()
		if l != nil {
			// start serving
			err = s.grpcServer.Serve(l)
			if err != nil {
				scope.Errorf("Galley Server unexpectedly terminated: %v", err)
			}
		}
	}()
	if s.callOut != nil {
		s.serveWG.Add(1)
		go func() {
			defer s.serveWG.Done()
			s.callOut.Run()
		}()
	}
}

func (s *Server) getListener() net.Listener {
	s.listenerMutex.Lock()
	defer s.listenerMutex.Unlock()
	return s.listener
}

// Address returns the Address of the MCP service.
func (s *Server) Address() net.Addr {
	l := s.getListener()
	if l == nil {
		return nil
	}
	return l.Addr()
}

// Close cleans up resources used by the server.
func (s *Server) Close() error {
	if s.stopCh != nil {
		close(s.stopCh)
		s.stopCh = nil
	}

	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}

	if s.controlZ != nil {
		s.controlZ.Close()
	}

	if s.processor != nil {
		s.processor.Stop()
	}

	s.listenerMutex.Lock()
	if s.listener != nil {
		_ = s.listener.Close()
		s.listener = nil
	}
	s.listenerMutex.Unlock()

	if s.reporter != nil {
		_ = s.reporter.Close()
	}

	if s.callOut != nil {
		s.callOut.Close()
	}

	if s.grpcServer != nil || s.callOut != nil {
		s.serveWG.Wait()
	}

	// final attempt to purge buffered logs
	_ = log.Sync()

	return nil
}

//RunServer start Galley Server mode
func RunServer(sa *Args, livenessProbeController,
	readinessProbeController probe.Controller) {
	log.Infof("Galley started with %s", sa)
	s, err := New(sa)
	if err != nil {
		log.Fatalf("Unable to initialize Galley Server: %v", err)
	}
	log.Infof("Istio Galley: %s\nStarting gRPC server on %v", version.Info, sa.APIAddress)
	s.Run()
	if livenessProbeController != nil {
		serverLivenessProbe := probe.NewProbe()
		serverLivenessProbe.SetAvailable(nil)
		serverLivenessProbe.RegisterProbe(livenessProbeController, "serverLiveness")
		defer serverLivenessProbe.SetAvailable(errors.New("stopped"))
	}
	if readinessProbeController != nil {
		serverReadinessProbe := probe.NewProbe()
		serverReadinessProbe.SetAvailable(nil)
		serverReadinessProbe.RegisterProbe(readinessProbeController, "serverReadiness")
		defer serverReadinessProbe.SetAvailable(errors.New("stopped"))
	}

	s.serveWG.Wait()
	_ = s.Close()
}
