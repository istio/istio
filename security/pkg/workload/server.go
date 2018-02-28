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

package workload

import (
	"fmt"
	"net"
	"os"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	sds "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"istio.io/istio/pkg/log"
)

// SDSServer implements api.SecretDiscoveryServiceServer that listens on a
// Unix Domain Socket.
type SDSServer struct {
	// Specifies the Unix Domain Socket paths the server listens on.
	// The UDS path identifies the identity for which the workload will
	// request X.509 key/cert from this server. This path should only be
	// accessible by such workload.
	// TODO: describe more details how this path identifies the workload
	// identity once the UDS path format is settled down.
	udsPath string

	// The grpc server that listens on the above udsPath.
	grpcServer *grpc.Server
}

const (
	// SecretTypeURL defines the type URL for Envoy secret proto
	SecretTypeURL = "type.googleapis.com/envoy.api.v2.auth.Secret"

	// SecretName defines the type of the secrets to fetch from the SDS server.
	SecretName = "SPKI"
)

// GetTLSCertificate generates the X.509 key/cert for the workload identity
// derived from udsPath, which is where the FetchSecrets grpc request is
// received.
func (s *SDSServer) GetTLSCertificate(udsPath string) *auth.TlsCertificate {
	// TODO: Add implementation. Consider define an interface to support
	// different implementations that can get certificate from different CA
	// systems including Istio CA and other CAs.
	return &auth.TlsCertificate{}
}

// FetchSecrets fetches the X.509 key/cert for a given workload whose identity
// can be derived from the UDS path where this call is received.
func (s *SDSServer) FetchSecrets(ctx context.Context, request *api.DiscoveryRequest) (*api.DiscoveryResponse, error) {
	resources := make([]types.Any, 1)
	secret := &auth.Secret{
		Name: SecretName,
		Type: &auth.Secret_TlsCertificate{
			TlsCertificate: s.GetTLSCertificate(s.udsPath),
		},
	}
	data, err := proto.Marshal(secret)
	if err != nil {
		errMessage := fmt.Sprintf("Generates invalid secret (%v)", err)
		log.Errorf(errMessage)
		return nil, status.Errorf(codes.Internal, errMessage)
	}
	resources[0] = types.Any{
		TypeUrl: SecretTypeURL,
		Value:   data,
	}
	// TODO: Set VersionInfo
	response := &api.DiscoveryResponse{
		Resources: resources,
		TypeUrl:   SecretTypeURL,
	}

	return response, nil
}

// StreamSecrets is not supported.
func (s *SDSServer) StreamSecrets(stream sds.SecretDiscoveryService_StreamSecretsServer) error {
	errMessage := "StreamSecrets is not implemented."
	log.Error(errMessage)
	return status.Errorf(codes.Unimplemented, errMessage)
}

// NewSDSServer creates the SDSServer that registers
// SecretDiscoveryServiceServer, a gRPC server, which listens on the given
// Unix Domain Socket.
func NewSDSServer(udsPath string) (*SDSServer, error) {
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	s := &SDSServer{
		udsPath:    udsPath,
		grpcServer: grpcServer,
	}

	sds.RegisterSecretDiscoveryServiceServer(grpcServer, s)

	_, err := os.Stat(udsPath)
	if err == nil {
		return nil, fmt.Errorf("UDS path %v already exists", udsPath)
	}
	listener, err := net.Listen("unix", udsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %v", err)
	}

	// grpcServer.Serve() is a blocking call, so run it in a goroutine.
	go func() {
		log.Infof("Starting GRPC server on UDS path", udsPath)

		err := s.grpcServer.Serve(listener)
		// grpcServer.Serve() always returns a non-nil error.
		log.Warnf("GRPC server returns an error: %v", err)
	}()

	return s, nil
}

// TODO: add methods to unregister the deleted UDS paths and the listeners.
