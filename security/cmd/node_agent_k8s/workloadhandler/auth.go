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

package workloadhandler

import (
	"errors"

	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

var (
	// ErrInvalidConnection define the invalide connection
	ErrInvalidConnection = errors.New("invalid connection")

	// ErrNoCredentials define no creds
	ErrNoCredentials = errors.New("no credentials available")
)

const (
	authType = "udsuspver"
)

// CredInfo returned by grpc Credential that the workload API can use.
type CredInfo struct {
	UID            string
	Name           string
	Namespace      string
	ServiceAccount string
	Err            error
}

// AuthType return the auth type
func (c CredInfo) AuthType() string {
	return authType
}

// CallerFromContext return the caller info
func CallerFromContext(ctx context.Context) (CredInfo, bool) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		return CredInfo{}, false
	}
	return CallerFromAuthInfo(peer.AuthInfo)
}

// CallerFromAuthInfo return the auth info
func CallerFromAuthInfo(ainfo credentials.AuthInfo) (CredInfo, bool) {
	if ci, ok := ainfo.(CredInfo); ok {
		return ci, true
	}
	return CredInfo{}, false
}
