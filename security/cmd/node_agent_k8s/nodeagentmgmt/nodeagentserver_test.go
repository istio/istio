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
	"testing"

	"golang.org/x/net/context"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
	mwi "istio.io/istio/security/cmd/node_agent_k8s/mgmtwlhintf"
	pb "istio.io/istio/security/proto"
)

// TODO(wattli): add more tests.
func TestWorkloadAddedService(t *testing.T) {
	wlmgmts := map[string]mwi.WorkloadMgmtInterface{
		"testid": nil,
	}
	server := &Server{wlmgmts, "path", make(chan bool), nil}

	attrs := pb.WorkloadInfo_WorkloadAttributes{Uid: "testid"}
	naInp := &pb.WorkloadInfo{Attrs: &attrs}

	resp, err := server.WorkloadAdded(context.Background(), naInp)
	if err != nil {
		t.Errorf("Failed to WorkloadAdded.")
	}
	if resp.Status.Code != int32(rpc.ALREADY_EXISTS) {
		t.Errorf("Failed to WorkloadAdded with resp %v.", resp)
	}
}

func TestWorkloadDeletedService(t *testing.T) {
	wlmgmts := map[string]mwi.WorkloadMgmtInterface{
		"testid": nil,
	}
	server := &Server{wlmgmts, "path", make(chan bool), nil}

	attrs := pb.WorkloadInfo_WorkloadAttributes{Uid: "testid2"}
	naInp := &pb.WorkloadInfo{Attrs: &attrs}

	resp, err := server.WorkloadDeleted(context.Background(), naInp)
	if err != nil {
		t.Errorf("Failed to WorkloadDeleted.")
	}
	if resp.Status.Code != int32(rpc.NOT_FOUND) {
		t.Errorf("Failed to WorkloadDeleted with resp %v.", resp)
	}
}
