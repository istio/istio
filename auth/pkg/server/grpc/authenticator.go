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

package grpc

import (
	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

type user struct {
	identities []string
}

type authenticator interface {
	authenticate(ctx context.Context) *user
}

// A authenticator that extracts identities from client certificate.
type clientCertAuthenticator struct{}

// authenticate extracts identities from presented client certificates. This
// method assumes that certificate chain has been properly validated before
// this method is called. In other words, this method does not do certificate
// chain validation itself.
func (cca *clientCertAuthenticator) authenticate(ctx context.Context) *user {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		glog.Info("no client certificate is presented")
		return nil
	}

	if authType := peer.AuthInfo.AuthType(); authType != "tls" {
		glog.Warningf("unsupported auth type: %q", authType)
		return nil
	}

	tlsInfo := peer.AuthInfo.(credentials.TLSInfo)
	chains := tlsInfo.State.VerifiedChains
	if len(chains) == 0 || len(chains[0]) == 0 {
		glog.Warningf("no verified chain is found")
		return nil
	}

	ids := extractIDs(chains[0][0].Extensions)
	return &user{ids}
}
