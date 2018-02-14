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

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
	pb "istio.io/istio/security/proto"
)

// TODO(wattli): add more tests.
func TestCheck(t *testing.T) {
	server := &WlServer{}

	req := &pb.CheckRequest{Name: "check"}
	resp, err := server.Check(context.Background(), req)
	if err != nil {
		t.Errorf("Failed to check with error %v.", err)
	}
	if resp.Status.Code != int32(rpc.PERMISSION_DENIED) {
		t.Errorf("Failed to check with resp %v.", resp)
	}
}
