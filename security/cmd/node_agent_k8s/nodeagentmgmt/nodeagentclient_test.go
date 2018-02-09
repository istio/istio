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

package nodeagentmgmt

import (
	"net"
	"testing"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
	pb "istio.io/istio/security/proto"
)

type FakeNodeAgentGrpcServer struct {
	response *pb.CsrResponse
	errorMsg string
}

func (s *FakeNodeAgentGrpcServer) WorkloadAdded(ctx context.Context, request *pb.WorkloadInfo) (*pb.NodeAgentMgmtResponse, error) {
	status := &rpc.Status{Code: int32(rpc.OK), Message: "OK"}
	return &pb.NodeAgentMgmtResponse{Status: status}, nil
}

func (s *FakeNodeAgentGrpcServer) WorkloadDeleted(ctx context.Context, request *pb.WorkloadInfo) (*pb.NodeAgentMgmtResponse, error) {
	status := &rpc.Status{Code: int32(rpc.OK), Message: "OK"}
	return &pb.NodeAgentMgmtResponse{Status: status}, nil
}

func TestWorkloadAdded(t *testing.T) {
	// create a local grpc server
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Errorf("failed to listen: %v", err)
	}
	serv := FakeNodeAgentGrpcServer{}

	go func() {
		defer func() {
			s.Stop()
		}()
		pb.RegisterNodeAgentServiceServer(s, &serv)
		reflection.Register(s)
		if err2 := s.Serve(lis); err2 != nil {
			t.Errorf("failed to serve: %v", err2)
		}
	}()

	// The goroutine starting the server may not be ready, results in flakiness.
	time.Sleep(1 * time.Second)
	client := &NodeAgentClient{nil, "Dest", false}

	attrs := pb.WorkloadInfo_WorkloadAttributes{Uid: "testid"}
	naInp := &pb.WorkloadInfo{Attrs: &attrs}

	_, err = client.WorkloadAdded(naInp)
	if err == nil {
		t.Errorf("Failed to WorkloadAdded.")
	}
}

func TestWorkloadDeleted(t *testing.T) {
	// create a local grpc server
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Errorf("failed to listen: %v", err)
	}
	serv := FakeNodeAgentGrpcServer{}

	go func() {
		defer func() {
			s.Stop()
		}()
		pb.RegisterNodeAgentServiceServer(s, &serv)
		reflection.Register(s)
		if err2 := s.Serve(lis); err2 != nil {
			t.Errorf("failed to serve: %v", err2)
		}
	}()

	// The goroutine starting the server may not be ready, results in flakiness.
	time.Sleep(1 * time.Second)
	client := &NodeAgentClient{nil, "Dest", false}

	attrs := pb.WorkloadInfo_WorkloadAttributes{Uid: "testid"}
	naInp := &pb.WorkloadInfo{Attrs: &attrs}

	_, err = client.WorkloadDeleted(naInp)
	if err == nil {
		t.Errorf("Failed to WorkloadDeleted.")
	}
}
