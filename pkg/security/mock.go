// Copyright Istio Authors
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

package security

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"go.uber.org/atomic"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"
)

type DirectSecretManager struct {
	items map[string]*SecretItem
	mu    sync.RWMutex
}

var _ SecretManager = &DirectSecretManager{}

func NewDirectSecretManager() *DirectSecretManager {
	return &DirectSecretManager{
		items: map[string]*SecretItem{},
	}
}

func (d *DirectSecretManager) GenerateSecret(resourceName string) (*SecretItem, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	si, f := d.items[resourceName]
	if !f {
		return nil, fmt.Errorf("resource %v not found", resourceName)
	}
	return si, nil
}

func (d *DirectSecretManager) Set(resourceName string, secret *SecretItem) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if secret == nil {
		delete(d.items, resourceName)
	} else {
		d.items[resourceName] = secret
	}
}

type FakeAuthenticator struct {
	AllowedToken string
	AllowedCert  string
	Name         string

	Successes *atomic.Int32
	Failures  *atomic.Int32

	mu sync.Mutex
}

func NewFakeAuthenticator(name string) *FakeAuthenticator {
	return &FakeAuthenticator{
		Name:      name,
		Successes: atomic.NewInt32(0),
		Failures:  atomic.NewInt32(0),
	}
}

func (f *FakeAuthenticator) Authenticate(authCtx AuthContext) (*Caller, error) {
	if authCtx.GrpcContext != nil {
		return f.authenticateGrpc(authCtx.GrpcContext)
	}
	if authCtx.Request != nil {
		return f.authenticateHTTP(authCtx.Request)
	}
	return nil, nil
}

func (f *FakeAuthenticator) authenticateHTTP(req *http.Request) (*Caller, error) {
	return nil, errors.New("not implemented")
}

func (f *FakeAuthenticator) authenticateGrpc(ctx context.Context) (*Caller, error) {
	f.mu.Lock()
	at := f.AllowedToken
	ac := f.AllowedCert
	f.mu.Unlock()
	token := checkToken(ctx, at)
	cert := checkCert(ctx, ac)
	id := []string{spiffe.Identity{
		TrustDomain:    "cluster.local",
		Namespace:      "fake-namespace",
		ServiceAccount: "fake-sa",
	}.String()}
	log.WithLabels("name", f.Name, "cert", cert, "token", token).Infof("authentication complete")
	if cert == nil {
		f.Successes.Inc()
		return &Caller{
			AuthSource: AuthSourceClientCertificate,
			Identities: id,
		}, nil
	}
	if token == nil {
		f.Successes.Inc()
		return &Caller{
			AuthSource: AuthSourceIDToken,
			Identities: id,
		}, nil
	}
	f.Failures.Inc()
	return nil, fmt.Errorf("neither token (%v) nor cert (%v) succeeded", token, cert)
}

func (f *FakeAuthenticator) AuthenticatorType() string {
	return "fake"
}

func (f *FakeAuthenticator) Set(token string, identity string) *FakeAuthenticator {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.AllowedToken = token
	f.AllowedCert = identity
	return f
}

var _ Authenticator = &FakeAuthenticator{}

func checkToken(ctx context.Context, expected string) error {
	if expected == "" {
		return fmt.Errorf("jwt authentication not allowed")
	}
	targetJWT, err := ExtractBearerToken(ctx)
	if err != nil {
		return fmt.Errorf("target JWT extraction error: %v", err)
	}
	if targetJWT != expected {
		return fmt.Errorf("expected token %q got %q", expected, targetJWT)
	}
	return nil
}

func checkCert(ctx context.Context, expected string) error {
	if expected == "" {
		return fmt.Errorf("cert authentication not allowed")
	}

	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return fmt.Errorf("no client certificate is presented")
	}

	if authType := p.AuthInfo.AuthType(); authType != "tls" {
		return fmt.Errorf("unsupported auth type: %q", authType)
	}

	tlsInfo := p.AuthInfo.(credentials.TLSInfo)
	chains := tlsInfo.State.VerifiedChains
	if len(chains) == 0 || len(chains[0]) == 0 {
		return fmt.Errorf("no verified chain is found")
	}

	ids, err := util.ExtractIDs(chains[0][0].Extensions)
	if err != nil {
		return fmt.Errorf("failed to extract IDs")
	}
	if !sets.New(ids...).Contains(expected) {
		return fmt.Errorf("expected identity %q, got %v", expected, ids)
	}

	return nil
}
