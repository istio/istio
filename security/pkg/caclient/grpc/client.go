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

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/platform"
	pb "istio.io/istio/security/proto"
)

// CAGrpcClient is for implementing the GRPC client to talk to CA.
type CAGrpcClient interface {
	// Send CSR to the CA and gets the response or error.
	SendCSR(*pb.CsrRequest, platform.Client, string) (*pb.CsrResponse, error)
}

// CAGrpcClientImpl is an implementation of GRPC client to talk to CA.
type CAGrpcClientImpl struct {
}

// SendCSR sends CSR to CA through GRPC.
func (c *CAGrpcClientImpl) SendCSR(req *pb.CsrRequest, pc platform.Client, caAddress string) (*pb.CsrResponse, error) {
	if caAddress == "" {
		return nil, fmt.Errorf("istio CA address is empty")
	}
	dialOptions, err := pc.GetDialOptions()
	if err != nil {
		return nil, err
	}
	conn, err := grpc.Dial(caAddress, dialOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %v", caAddress, err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Errorf("Failed to close connection")
		}
	}()
	client := pb.NewIstioCAServiceClient(conn)
	resp, err := client.HandleCSR(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("CSR request failed %v", err)
	}
	return resp, nil
}
