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
	"testing"

	"golang.org/x/net/context"

	"google.golang.org/grpc/peer"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/istio/security/pkg/flexvolume"
	"istio.io/istio/security/pkg/flexvolume/binder"
	pb "istio.io/istio/security/proto"
)

func TestCheckWithPeerCredentials(t *testing.T) {
	credential := binder.Credentials{
		WorkloadCredentials: flexvolume.Credential{UID: "1111-1111-1111",
			Workload:       "foo",
			Namespace:      "default",
			ServiceAccount: "serviceaccount"},
	}
	ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credential})

	req := &pb.CheckRequest{Name: "check"}
	server := NewWorkloadAPIServer()
	resp, err := server.Check(ctx, req)
	if err != nil {
		t.Errorf("Failed to check with error %v.", err)
	}
	if resp.Status.Code != int32(rpc.OK) {
		t.Errorf("Failed to check with resp %v.", resp)
	}
}

func TestCheckNoPeerCredentials(t *testing.T) {
	req := &pb.CheckRequest{Name: "check"}
	server := NewWorkloadAPIServer()
	resp, err := server.Check(context.Background(), req)
	if err != nil {
		t.Errorf("Failed to check with error %v.", err)
	}
	if resp.Status.Code != int32(rpc.PERMISSION_DENIED) {
		t.Errorf("Failed to check with resp %v.", resp)
	}
}
