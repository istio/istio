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
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	sds "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
)

var (
	tls      = flag.Bool("tls", false, "Connection uses TLS if true, else plain TCP")
	certFile = flag.String("cert_file", "", "The TLS cert file")
	keyFile  = flag.String("key_file", "", "The TLS key file")
	port     = flag.Int("port", 10000, "The server port")
	udsPath  = flag.String("uds_path", "sock", "Unix Domain Socket file path name")
)

// SDSServer implements sds.SecretDiscoveryServiceServer that listens on a
// Unix Domain Socket.
type SDSServer struct {
	// Specifies the Unix Domain Socket paths the server listens on.
	// The UDS path identifies the identity for which the workload will
	// request X.509 key/cert from this server. This path should only be
	// accessible by such workload.
	// TODO: describe more details how this path identifies the workload
	// identity once the UDS path format is settled down.
	udsPath string
}

// GetTLSCertificate generates the X.509 key/cert for the workload identity
// derived from udsPath, which is where the FetchSecrets grpc request is
// received.
func (s *SDSServer) GetTLSCertificate() *auth.TlsCertificate {
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
		Name: "SPKI",
		Type: &auth.Secret_TlsCertificate{
			TlsCertificate: s.GetTLSCertificate(),
		},
	}
	data, _ := proto.Marshal(secret)
	typeURL := "type.googleapis.com/envoy.api.v2.auth.Secret"
	resources[0] = types.Any{
		TypeUrl: typeURL,
		Value:   data,
	}
	response := &api.DiscoveryResponse{
		VersionInfo: "0",
		Resources:   resources,
		TypeUrl:     typeURL,
	}

	return response, nil
}

// StreamSecrets is not supported.
func (s *SDSServer) StreamSecrets(stream sds.SecretDiscoveryService_StreamSecretsServer) error {
	log.Print("StreamSecrets is not implemented.")
	return nil
}

// newServer creates a SDSServer.
func newServer(udsPath string) *SDSServer {
	s := &SDSServer{
		udsPath: udsPath,
	}
	return s
}

func main() {
	flag.Parse()
	var opts []grpc.ServerOption
	if *tls {
		creds, err := credentials.NewServerTLSFromFile(*certFile, *keyFile)
		if err != nil {
			log.Fatalf("Failed to generate credentials %v", err)
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}
	grpcServer := grpc.NewServer(opts...)

	var lis net.Listener
	var err error
	if *udsPath != "" {
		sds.RegisterSecretDiscoveryServiceServer(grpcServer, newServer(*udsPath))
		_, err = os.Stat(*udsPath)
		if err == nil {
			err = os.RemoveAll(*udsPath)
			if err != nil {
				log.Fatalf("failed to %v %v", *udsPath, err)
			}
		}
		lis, err = net.Listen("unix", *udsPath)
		if err != nil {
			log.Fatalf("failed to %v", err)
		}
	} else {
		lis, err = net.Listen("tcp", fmt.Sprintf("localhost:%d", *port))
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}
	}

	_ = grpcServer.Serve(lis)
}
