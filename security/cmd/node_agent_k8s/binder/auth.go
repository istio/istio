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
	"golang.org/x/net/context"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	fvcreds "istio.io/istio/security/pkg/flexvolume"
)

const (
	authType = "udsuspver"
)

type Credentials struct {
	WorkloadCredentials fvcreds.Credential
}

func (c Credentials) AuthType() string {
	return authType
}

func CallerFromContext(ctx context.Context) (fvcreds.Credential, bool) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		return fvcreds.Credential{}, false
	}
	return CallerFromAuthInfo(peer.AuthInfo)
}

func CallerFromAuthInfo(ainfo credentials.AuthInfo) (fvcreds.Credential, bool) {
	if ci, ok := ainfo.(Credentials); ok {
		return ci.WorkloadCredentials, true
	}
	return fvcreds.Credential{}, false
}
