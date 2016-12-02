// Copyright 2016 Google Inc.
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

package main

// gRPC server. The APIServer type handles incoming streaming gRPC traffic and invokes method-specific
// handlers to implement the method-specific logic.
//
// When you create an APIServer instance, you specify a number of transport-level options, along with the
// set of method handlers responsible for the logic of individual API methods

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"

	"github.com/golang/glog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"istio.io/mixer/adapters"
	"istio.io/mixer/api/v1"

	proto "github.com/golang/protobuf/proto"
)

// APIServerOptions controls the behavior of a gRPC server.
type APIServerOptions struct {
	// Port specifies the IP port the server should listen on.
	Port uint16

	// MaximumMessageSize constrains the size of incoming requests.
	MaxMessageSize uint

	// MaxConcurrentStreams limits the amount of concurrency allowed,
	// in order to put a cap on server-side resource usage.
	MaxConcurrentStreams uint

	// CompressedPayload determines whether compression should be
	// used on individual messages.
	CompressedPayload bool

	// ServerCertificate provides the server-side cert for TLS connections.
	// If this is not supplied, only connections in the clear are supported.
	ServerCertificate *tls.Certificate

	// ClientCertificate provides the acceptable client-side certs. Only clients
	// presenting one of these certs will be allowed to connect. If this is nil,
	// then any clients will be allowed.
	ClientCertificates *x509.CertPool

	// Handlers holds pointers to the functions that implement request-level processing
	// for all API methods
	Handlers APIHandlers

	// FactConverter is a pointer to the global fact conversion
	// adapter to use.
	FactConverter adapters.FactConverter
}

// APIServer holds the state for the gRPC API server.
// Use NewAPIServer to get one of these.
type APIServer struct {
	server        *grpc.Server
	listener      net.Listener
	handler       APIHandlers
	factConverter adapters.FactConverter
}

// NewAPIServer creates the gRPC serving stack.
func NewAPIServer(options *APIServerOptions) (*APIServer, error) {
	// get the network stuff setup
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", options.Port))
	if err != nil {
		return nil, err
	}

	// construct the gRPC options

	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(uint32(options.MaxConcurrentStreams)))
	grpcOptions = append(grpcOptions, grpc.MaxMsgSize(int(options.MaxMessageSize)))

	if options.CompressedPayload {
		grpcOptions = append(grpcOptions, grpc.RPCCompressor(grpc.NewGZIPCompressor()))
		grpcOptions = append(grpcOptions, grpc.RPCDecompressor(grpc.NewGZIPDecompressor()))
	}

	if options.ServerCertificate != nil {
		// enable TLS
		tlsConfig := &tls.Config{}
		tlsConfig.Certificates = []tls.Certificate{*options.ServerCertificate}

		if options.ClientCertificates != nil {
			// enable TLS mutual auth
			tlsConfig.ClientCAs = options.ClientCertificates
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		}
		tlsConfig.BuildNameToCertificate()

		grpcOptions = append(grpcOptions, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}

	// get everything wired up
	grpcServer := grpc.NewServer(grpcOptions...)
	apiServer := &APIServer{grpcServer, listener, options.Handlers, options.FactConverter}
	mixerpb.RegisterMixerServer(grpcServer, apiServer)
	return apiServer, nil
}

// Start listening for incoming requests. Only returns
// in catastrophic failure cases.
func (s *APIServer) Start() error {
	return s.server.Serve(s.listener)
}

// Stop undoes the effect of a previous call to Listen, basically it stops the server
// from processing any more requests
func (s *APIServer) Stop() {
	s.server.GracefulStop()
}

type handlerFunc func(tracker adapters.FactTracker, request proto.Message, response proto.Message)

func (s *APIServer) streamLoop(stream grpc.ServerStream, request proto.Message, response proto.Message, handler handlerFunc) error {
	tracker := s.factConverter.NewTracker()
	for {
		// get a single message
		if err := stream.RecvMsg(request); err == io.EOF {
			return nil
		} else if err != nil {
			glog.Errorf("Stream error %s", err)
			return err
		}

		// do the actual work for the message
		handler(tracker, request, response)

		// produce the response
		if err := stream.SendMsg(response); err != nil {
			return err
		}

		// reset everything to 0
		request.Reset()
		response.Reset()
	}
}

// Check is the entry point for the external Check method
func (s *APIServer) Check(stream mixerpb.Mixer_CheckServer) error {
	return s.streamLoop(stream,
		new(mixerpb.CheckRequest),
		new(mixerpb.CheckResponse),
		func(tracker adapters.FactTracker, request proto.Message, response proto.Message) {
			s.handler.Check(tracker, request.(*mixerpb.CheckRequest), response.(*mixerpb.CheckResponse))
		})
}

// Report is the entry point for the external Report method
func (s *APIServer) Report(stream mixerpb.Mixer_ReportServer) error {
	return s.streamLoop(stream,
		new(mixerpb.ReportRequest),
		new(mixerpb.ReportResponse),
		func(tracker adapters.FactTracker, request proto.Message, response proto.Message) {
			s.handler.Report(tracker, request.(*mixerpb.ReportRequest), response.(*mixerpb.ReportResponse))
		})
}

// Quota is the entry point for the external Quota method
func (s *APIServer) Quota(stream mixerpb.Mixer_QuotaServer) error {
	return s.streamLoop(stream,
		new(mixerpb.QuotaRequest),
		new(mixerpb.QuotaResponse),
		func(tracker adapters.FactTracker, request proto.Message, response proto.Message) {
			s.handler.Quota(tracker, request.(*mixerpb.QuotaRequest), response.(*mixerpb.QuotaResponse))
		})
}
