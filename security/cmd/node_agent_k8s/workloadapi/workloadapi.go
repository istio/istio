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

package workloadapi

import (
	"fmt"
	"log"

	"golang.org/x/net/context"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/istio/security/pkg/flexvolume/binder"
	pb "istio.io/istio/security/proto"
)

// WorkloadServer define the struct for workload server
type WorkloadServer struct{}

// NewWorkloadAPIServer define the new api
func NewWorkloadAPIServer() pb.WorkloadServiceServer {
	return &WorkloadServer{}
}

// Check do the check
func (s *WorkloadServer) Check(ctx context.Context, request *pb.CheckRequest) (*pb.CheckResponse, error) {

	log.Printf("[%v]: %v Check called", s, request)
	// Get the caller's credentials from the context.
	creds, e := binder.CallerFromContext(ctx)
	if !e {
		resp := fmt.Sprint("Not able to get credentials")
		status := &rpc.Status{Code: int32(rpc.PERMISSION_DENIED), Message: resp}
		return &pb.CheckResponse{Status: status}, nil
	}

	log.Printf("Credentials are %v", creds)

	resp := fmt.Sprintf("all good to workload with service account %v", creds.ServiceAccount)
	status := &rpc.Status{Code: int32(rpc.OK), Message: resp}
	return &pb.CheckResponse{Status: status}, nil
}
