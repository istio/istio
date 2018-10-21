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

// Package protocol defines the interface of CA client protocol. Currently we only support gRPC
// protocol sent to Istio CA server.
package protocol

import (
	"fmt"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pb "istio.io/istio/security/proto"
)

// CAProtocol is the interface for talking to CA.
type CAProtocol interface {
	// SendCSR send CSR request to the CA server.
	SendCSR(*pb.CsrRequest) (*pb.CsrResponse, error)
}

// GrpcConnection implements CAProtocol talking to CA via gRPC.
// TODO(incfly): investigate the overhead of maintaining gRPC connection for CA server compared with
// establishing new connection every time.
type GrpcConnection struct {
	connection *grpc.ClientConn
}

// NewGrpcConnection creates a gRPC connection.
func NewGrpcConnection(caAddr string, dialOptions []grpc.DialOption) (*GrpcConnection, error) {
	if caAddr == "" {
		return nil, fmt.Errorf("istio CA address is empty")
	}
	conn, err := grpc.Dial(caAddr, dialOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %v", caAddr, err)
	}
	return &GrpcConnection{
		connection: conn,
	}, nil
}

// SendCSR sends a resquest to CA server and returns the response.
func (c *GrpcConnection) SendCSR(req *pb.CsrRequest) (*pb.CsrResponse, error) {
	client := pb.NewIstioCAServiceClient(c.connection)
	return client.HandleCSR(context.Background(), req)
}

// Close closes the gRPC connection.
func (c *GrpcConnection) Close() error {
	return c.connection.Close()
}
