//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package server

import (
	"fmt"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/kube/source"
	"istio.io/istio/galley/pkg/metadata"

	"istio.io/istio/galley/pkg/kube"
	"istio.io/istio/galley/pkg/mcp/server"
	"istio.io/istio/galley/pkg/mcp/snapshot"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
)

// Server is the main entry point into the Galley code.
type Server struct {
	shutdown chan error

	grpcServer *grpc.Server
	processor  *runtime.Processor
	mcp        *server.Server
	listener   net.Listener
	controlZ   *ctrlz.Server

	// probes
	livenessProbe  probe.Controller
	readinessProbe probe.Controller
	*probe.Probe
}

type patchTable struct {
	logConfigure          func(*log.Options) error
	newKubeFromConfigFile func(string) (kube.Interfaces, error)
	newSource             func(kube.Interfaces, time.Duration) (runtime.Source, error)
	netListen             func(network, address string) (net.Listener, error)
}

func defaultPatchTable() patchTable {
	return patchTable{
		logConfigure:          log.Configure,
		newKubeFromConfigFile: kube.NewKubeFromConfigFile,
		newSource:             source.New,
		netListen:             net.Listen,
	}
}

// New returns a new instance of a Server.
func New(a *Args) (*Server, error) {
	return newServer(a, defaultPatchTable())
}

func newServer(a *Args, p patchTable) (*Server, error) {
	s := &Server{}

	if err := p.logConfigure(a.LoggingOptions); err != nil {
		return nil, err
	}

	k, err := p.newKubeFromConfigFile(a.KubeConfig)
	if err != nil {
		return nil, err
	}

	src, err := p.newSource(k, a.ResyncPeriod)
	if err != nil {
		return nil, err
	}

	distributor := snapshot.New()
	s.processor = runtime.NewProcessor(src, distributor)

	s.mcp = server.New(distributor, metadata.Types.TypeURLs())

	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(uint32(a.MaxConcurrentStreams)))
	grpcOptions = append(grpcOptions, grpc.MaxRecvMsgSize(int(a.MaxReceivedMessageSize)))

	grpc.EnableTracing = a.EnableGRPCTracing
	s.grpcServer = grpc.NewServer(grpcOptions...)

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
		_ = s.Close()
		return nil, fmt.Errorf("unable to listen: %v", err)
	}

	mcp.RegisterAggregatedMeshConfigServiceServer(s.grpcServer, s.mcp)

	s.Probe = probe.NewProbe()

	if a.LivenessProbeOptions.IsValid() {
		s.livenessProbe = probe.NewFileController(a.LivenessProbeOptions)
		s.RegisterProbe(s.livenessProbe, "server")
		s.livenessProbe.Start()
	}

	if a.ReadinessProbeOptions.IsValid() {
		s.readinessProbe = probe.NewFileController(a.ReadinessProbeOptions)
		s.readinessProbe.Start()
	}

	s.controlZ, _ = ctrlz.Run(a.IntrospectionOptions, nil)

	return s, nil
}

// Run enables Galley to start receiving gRPC requests on its main API port.
func (s *Server) Run() {
	s.shutdown = make(chan error, 1)
	s.SetAvailable(nil)
	go func() {
		err := s.processor.Start()
		if err != nil {
			s.shutdown <- err
			return
		}

		// start serving
		err = s.grpcServer.Serve(s.listener)
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
		s.grpcServer.GracefulStop()
		_ = s.Wait()
	}

	if s.controlZ != nil {
		s.controlZ.Close()
	}

	if s.processor != nil {
		s.processor.Stop()
	}

	if s.listener != nil {
		_ = s.listener.Close()
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
