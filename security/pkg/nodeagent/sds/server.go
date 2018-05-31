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

package sds

import (
	"net"

	"google.golang.org/grpc"

	"istio.io/istio/pkg/log"
)

const maxStreams = 100000

// Options provides all of the configuration parameters for secret discovery service.
type Options struct {
	// UDSPath is the unix domain socket through which SDS server communicates with proxies.
	UDSPath string
}

// Server is the gPRC server that exposes SDS through UDS.
type Server struct {
	envoySds          *sdsservice
	grpcServer        *grpc.Server
	grpcListeningAddr net.Addr

	closing chan bool
}

// NewServer creates and starts the Grpc server for SDS.
func NewServer(options Options, st secretStore) (*Server, error) {
	s := &Server{
		envoySds: newSDSService(st),
		closing:  make(chan bool, 1),
	}
	if err := s.initDiscoveryService(&options, st); err != nil {
		log.Errorf("Failed to initialize secret discovery service: %v", err)
		return nil, err
	}
	return s, nil
}

// Stop closes the gRPC server.
func (s *Server) Stop() {
	if s == nil {
		return
	}
	s.closing <- true
}

func (s *Server) initDiscoveryService(options *Options, st secretStore) error {
	s.initGrpcServer()
	s.envoySds.register(s.grpcServer)

	grpcListener, err := net.Listen("unix", options.UDSPath)
	if err != nil {
		log.Errorf("Failed to listen on unix socket %q: %v", options.UDSPath, err)
		return err
	}
	s.grpcListeningAddr = grpcListener.Addr()

	go func() {
		if err = s.grpcServer.Serve(grpcListener); err != nil {
			log.Errorf("SDS grpc server failed to start: %v", err)
		}
	}()

	go func() {
		<-s.closing
		if grpcListener != nil {
			grpcListener.Close()
		}

		if s.grpcServer != nil {
			s.grpcServer.Stop()
		}
	}()

	return nil
}

func (s *Server) initGrpcServer() {
	grpcOptions := s.grpcServerOptions()
	s.grpcServer = grpc.NewServer(grpcOptions...)
}

func (s *Server) grpcServerOptions() []grpc.ServerOption {
	grpcOptions := []grpc.ServerOption{
		grpc.MaxConcurrentStreams(uint32(maxStreams)),
	}
	return grpcOptions
}
