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

package grpc

import (
	"fmt"
	"net"

	"google.golang.org/grpc"

	"github.com/golang/glog"

	"golang.org/x/net/context"

	"istio.io/auth/pkg/pki"
	"istio.io/auth/pkg/pki/ca"
	pb "istio.io/auth/proto"
)

// Server implements pb.IstioCAService and provides the service on the
// specified port.
type Server struct {
	ca   ca.CertificateAuthority
	port int
}

// HandleCSR handles an incoming certificate signing request (CSR). It does
// proper validation (e.g. authentication) and upon validated, signs the CSR
// and returns the resulting certificate. If not approved, reason for refusal
// to sign is returned as part of the response object.
func (s *Server) HandleCSR(ctx context.Context, request *pb.Request) (*pb.Response, error) {
	// TODO: handle authentication here

	csr, err := pki.ParsePemEncodedCSR(request.CsrPem)
	if err != nil {
		glog.Error(err)
		// TODO: possibly wrap err in GRPC error
		return nil, err
	}

	cert, err := s.ca.Sign(csr)
	if err != nil {
		glog.Error(err)
		// TODO: possibly wrap err in GRPC error
		return nil, err
	}

	response := &pb.Response{
		IsApproved:      true,
		SignedCertChain: cert,
	}

	return response, nil
}

// Run starts a GRPC server on the specified port.
func (s *Server) Run() error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("cannot listen on port %d (error: %v)", s.port, err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterIstioCAServiceServer(grpcServer, s)

	// grpcServer.Serve() is a blocking call, so run it in a goroutine.
	go func() {
		glog.Infof("Starting GRPC server on port %d", s.port)

		err := grpcServer.Serve(listener)

		// grpcServer.Serve() always returns a non-nil error.
		glog.Warningf("GRPC server returns an error: %v", err)
	}()

	return nil
}

// New creates a new instance of `IstioCAServiceServer`.
func New(ca ca.CertificateAuthority, port int) *Server {
	return &Server{ca, port}
}
