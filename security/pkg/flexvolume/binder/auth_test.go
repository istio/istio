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

package binder

import (
	"reflect"
	"testing"

	"golang.org/x/net/context"

	"google.golang.org/grpc/peer"

	"istio.io/istio/security/pkg/flexvolume"
)

func TestAuthType(t *testing.T) {
	cred := Credentials{}
	if cred.AuthType() != authType {
		t.Errorf("Auth type not equal to %v.", authType)
	}
}

func TestCallerFromContext(t *testing.T) {
	expectedCredential := flexvolume.Credential{UID: "1111-1111-1111",
		Workload:       "foo",
		Namespace:      "default",
		ServiceAccount: "serviceaccount"}

	credential := Credentials{
		WorkloadCredentials: expectedCredential,
	}
	ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credential})
	gotCredential, e := CallerFromContext(ctx)
	if !e {
		t.Errorf("Expected %v got error", expectedCredential)
	}
	if !reflect.DeepEqual(expectedCredential, gotCredential) {
		t.Errorf("Expected %v got %v", expectedCredential, gotCredential)
	}
}

func TestCallerNoPeerContext(t *testing.T) {
	_, e := CallerFromContext(context.Background())
	if e {
		t.Errorf("Expected failure to get Credentials")
	}
}

func TestCallerFromAuthInfo(t *testing.T) {
	expectedCredential := flexvolume.Credential{UID: "1111-1111-1111",
		Workload:       "foo",
		Namespace:      "default",
		ServiceAccount: "serviceaccount"}

	credential := Credentials{
		WorkloadCredentials: expectedCredential,
	}
	_, e := CallerFromAuthInfo(credential)
	if !e {
		t.Errorf("Expected no failure")
	}
}

type emptyCredential struct{}

func (emptyCredential) AuthType() string {
	return "Empty"
}

func TestCallerFromAuthInfoNoCredential(t *testing.T) {
	ec := emptyCredential{}
	_, e := CallerFromAuthInfo(ec)
	if e {
		t.Errorf("Expected failure in credetial typecast")
	}
}
