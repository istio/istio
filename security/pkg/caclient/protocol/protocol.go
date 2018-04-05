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

//// CAGrpcClientImpl is an implementation of GRPC client to talk to CA.
//type CAGrpcClientImpl struct {
//}

// CAGrpcProtocol implements CAProtocol talking to CA via gRPC.
type GrpcConnection struct {
	connection *grpc.ClientConn
}

// NewGrpcConnection creates a gRPC connection.
func NewGrpcConnection(caAddr string, dialOptions []grpc.DialOption) (*GrpcConnection, error) {
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

// SendCSR sends CSR to CA through GRPC.
//func (c *CAGrpcClientImpl) SendCSR(req *pb.CsrRequest, pc platform.Client, caAddress string) (*pb.CsrResponse, error) {
//	if caAddress == "" {
//		return nil, fmt.Errorf("istio CA address is empty")
//	}
//	dialOptions, err := pc.GetDialOptions()
//	if err != nil {
//		return nil, err
//	}
//	conn, err := grpc.Dial(caAddress, dialOptions...)
//	if err != nil {
//		return nil, fmt.Errorf("failed to dial %s: %v", caAddress, err)
//	}
//	defer func() {
//		if closeErr := conn.Close(); closeErr != nil {
//			log.Errorf("Failed to close connection")
//		}
//	}()
//	client := pb.NewIstioCAServiceClient(conn)
//	resp, err := client.HandleCSR(context.Background(), req)
//	if err != nil {
//		return nil, fmt.Errorf("CSR request failed %v", err)
//	}
//	return resp, nil
//}

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
	if f.errMsg != "" {
		return nil, fmt.Errorf(f.errMsg)
	}
	return f.resp, nil
}

// InvokeTimes returns the times that SendCSR has been invoked.
func (f *FakeProtocol) InvokeTimes() int {
	return f.counter
}
