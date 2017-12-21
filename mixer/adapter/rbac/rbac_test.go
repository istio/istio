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

package rbac

import (
	"context"
	"testing"

	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/authorization"
)

type fakedAllowRbacstore struct {
	called int
}

func (f *fakedAllowRbacstore) CheckPermission(inst *authorization.Instance) (bool, error) {
	f.called++
	return true, nil
}

type fakedDenyRbacstore struct {
	called int
}

func (f *fakedDenyRbacstore) CheckPermission(inst *authorization.Instance) (bool, error) {
	f.called++
	return false, nil
}

func TestHandleAuthorization_Success(t *testing.T) {
	rbac := &fakedAllowRbacstore{}
	handler := &handler{rbac, test.NewEnv(t)}

	instance := authorization.Instance{}
	result, _ := handler.HandleAuthorization(context.Background(), &instance)

	if rbac.called != 1 {
		t.Fatalf("rbac store was not called")
	}

	if result.Status.Code != int32(rpc.OK) {
		t.Fatalf("Got %v, want OK status", result.Status)
	}
}

func TestHandleAuthorization_Deny(t *testing.T) {
	rbac := &fakedDenyRbacstore{}
	handler := &handler{rbac, test.NewEnv(t)}

	instance := authorization.Instance{}
	result, _ := handler.HandleAuthorization(context.Background(), &instance)

	if rbac.called != 1 {
		t.Fatalf("rbac store was not called")
	}

	if result.Status.Code != int32(rpc.PERMISSION_DENIED) {
		t.Fatalf("Got %v, want PermissionDenied status", result.Status)
	}
}
