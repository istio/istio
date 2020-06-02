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

package mock

import (
	"context"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"

	gcapb "istio.io/istio/security/proto/providers/google"
)

// CAService is a simple mocked Google CA Service.
type CAService struct {
	Certs []string
	Err   error
}

// CreateCertificate is a mocked function for the Google Mesh CA API.
func (ca *CAService) CreateCertificate(ctx context.Context, in *gcapb.MeshCertificateRequest) (
	*gcapb.MeshCertificateResponse, error) {
	if ca.Err == nil {
		return &gcapb.MeshCertificateResponse{CertChain: ca.Certs}, nil
	}
	return nil, ca.Err
}

// CAServer is the mocked Mesh CA server.
type CAServer struct {
	Server  *grpc.Server
	Address string
}

// CreateServer creates a mocked local Google CA server and runs it in a separate thread.
// nolint: interfacer
func CreateServer(addr string, service *CAService) (*CAServer, error) {
	// create a local grpc server
	s := &CAServer{
		Server: grpc.NewServer(),
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on the TCP address: %v", err)
	}
	s.Address = lis.Addr().String()

	var serveErr error
	go func() {
		gcapb.RegisterMeshCertificateServiceServer(s.Server, service)
		if err := s.Server.Serve(lis); err != nil {
			serveErr = err
		}
	}()

	// The goroutine starting the server may not be ready, results in flakiness.
	time.Sleep(1 * time.Second)
	if serveErr != nil {
		return nil, err
	}

	return s, nil
}

// Stop stops the Mock Mesh CA server.
func (s *CAServer) Stop() {
	if s.Server != nil {
		s.Server.Stop()
	}
}
