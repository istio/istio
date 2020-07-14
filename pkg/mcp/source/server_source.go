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

package source

import (
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/rate"
)

// TODO: consolidate common interfaces in source/server_source.go and sink/server_sink.go

// AuthChecker is used to check the transport auth info that is associated with each stream. If the function
// returns nil, then the connection will be allowed. If the function returns an error, then it will be
// percolated up to the gRPC stack.
//
// Note that it is possible that this method can be called with nil authInfo. This can happen either if there
// is no peer info, or if the underlying gRPC stream is insecure. The implementations should be resilient in
// this case and apply appropriate policy.
type AuthChecker interface {
	Check(authInfo credentials.AuthInfo) error
}

// Server implements the server for the MCP source service. The server is the source of configuration and sends
// configuration to the client.
type Server struct {
	authCheck   AuthChecker
	rateLimiter rate.Limit
	src         *Source
	metadata    metadata.MD
}

var _ mcp.ResourceSourceServer = &Server{}

// ServerOptions contains sink server specific options
type ServerOptions struct {
	AuthChecker AuthChecker
	RateLimiter rate.Limit
	Metadata    metadata.MD
}

// NewServer creates a new instance of a MCP source server.
func NewServer(srcOptions *Options, serverOptions *ServerOptions) *Server {
	s := &Server{
		src:         New(srcOptions),
		authCheck:   serverOptions.AuthChecker,
		rateLimiter: serverOptions.RateLimiter,
		metadata:    serverOptions.Metadata,
	}
	return s
}

// EstablishResourceStream implements the ResourceSourceServer interface.
func (s *Server) EstablishResourceStream(stream mcp.ResourceSource_EstablishResourceStreamServer) error {
	if s.rateLimiter != nil {
		if err := s.rateLimiter.Wait(stream.Context()); err != nil {
			return err
		}

	}
	var authInfo credentials.AuthInfo
	if peerInfo, ok := peer.FromContext(stream.Context()); ok {
		authInfo = peerInfo.AuthInfo
	} else {
		scope.Warnf("No peer info found on the incoming stream.")
	}

	if err := s.authCheck.Check(authInfo); err != nil {
		return status.Errorf(codes.Unauthenticated, "Authentication failure: %v", err)
	}

	if err := stream.SendHeader(s.metadata); err != nil {
		return err
	}
	err := s.src.ProcessStream(stream)
	code := status.Code(err)
	if code == codes.OK || code == codes.Canceled || err == io.EOF {
		return nil
	}
	return err
}
