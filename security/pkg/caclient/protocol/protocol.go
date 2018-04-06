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

	//"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/platform"
	pb "istio.io/istio/security/proto"
)

// CAProtocol is the interface for talking to CA.
type CAProtocol interface {
	// SendCSR send CSR request to the CA server.
	SendCSR(*pb.CsrRequest) (*pb.CsrResponse, error)
}

// CAGrpcClient is for implementing the GRPC client to talk to CA.
type CAGrpcClient interface {
	// Send CSR to the CA and gets the response or error.
	SendCSR(*pb.CsrRequest, platform.Client, string) (*pb.CsrResponse, error)
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

// FakeProtocol is a fake for testing, implements CAProtocol interface.
type FakeProtocol struct {
	counter int
	resp    *pb.CsrResponse
	errMsg  string
}

// NewFakeProtocol returns a FakeProtocol with configured response and expected error.
func NewFakeProtocol(response *pb.CsrResponse, err string) *FakeProtocol {
	return &FakeProtocol{
		resp:   response,
		errMsg: err,
	}
}

// SendCSR returns the result based on the predetermined config.
func (f *FakeProtocol) SendCSR(req *pb.CsrRequest) (*pb.CsrResponse, error) {
	f.counter++
	if f.counter > 8 {
		return nil, fmt.Errorf("terminating the test with errors")
	}

	if f.errMsg != "" {
		return nil, fmt.Errorf(f.errMsg)
	}
	if f.resp == nil {
		return &pb.CsrResponse{}, nil
	}
	return f.resp, nil
}

// InvokeTimes returns the times that SendCSR has been invoked.
func (f *FakeProtocol) InvokeTimes() int {
	return f.counter
}
