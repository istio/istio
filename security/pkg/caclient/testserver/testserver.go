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

// Package testserver provides a simple ca server for testing.
package testserver

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "istio.io/istio/security/proto"
)

// CAServer is a ca server for testing.
type CAServer struct {
	response *pb.CsrResponse // the response the server returns.
	errorMsg string          // if not empty, returned error with this string
	counter  int             // tracks the HandleCSR invoking times.
}

// HandleCSR accepts the request and returns the configured response or error.
func (s *CAServer) HandleCSR(ctx context.Context, req *pb.CsrRequest) (*pb.CsrResponse, error) {
	s.counter++
	if len(s.errorMsg) > 0 {
		return nil, fmt.Errorf(s.errorMsg)
	}
	if s.response != nil {
		return s.response, nil
	}
	return &pb.CsrResponse{}, nil
}

// InvokeTimes returns the times HandleCSR is invoked for this test server.
func (s *CAServer) InvokeTimes() int {
	return s.counter
}

// New creates a test server, returns the server, its address.
func New(response *pb.CsrResponse, errmsg string) (server *CAServer, addr string, err error) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, "", fmt.Errorf("failed to allocate address for server %v", err)
	}
	ca := &CAServer{
		response: response,
		errorMsg: errmsg,
	}
	s := grpc.NewServer()
	pb.RegisterIstioCAServiceServer(s, ca)
	reflection.Register(s)
	go s.Serve(lis)
	return ca, lis.Addr().String(), nil
}
