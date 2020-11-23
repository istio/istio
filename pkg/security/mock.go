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
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"sync"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/security/pkg/server/ca/authenticate"
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

	mu sync.Mutex
}

func (f *FakeAuthenticator) Authenticate(ctx context.Context) (*authenticate.Caller, error) {
	token := checkToken(ctx, f.AllowedToken)
	cert := checkCert(ctx, f.AllowedCert)
	id := []string{spiffe.Identity{
		TrustDomain:    "cluster.local",
		Namespace:      "fake-namespace",
		ServiceAccount: "fake-sa",
	}.String()}
	log.WithLabels("name", f.Name, "cert", cert, "token", token).Infof("authentication complete")
	if cert == nil {
		return &authenticate.Caller{
			AuthSource: authenticate.AuthSourceClientCertificate,
			Identities: id,
		}, nil
	}
	if token == nil {
		return &authenticate.Caller{
			AuthSource: authenticate.AuthSourceIDToken,
			Identities: id,
		}, nil
	}
	return nil, fmt.Errorf("neither token (%v) nor cert (%v) succeeded", token, cert)
}

func (f *FakeAuthenticator) AuthenticatorType() string {
	return "fake"
}

func (f *FakeAuthenticator) Set(token string, certFile string) *FakeAuthenticator {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.AllowedToken = token
	f.AllowedCert = certFile
	return f
}

var _ authenticate.Authenticator = &FakeAuthenticator{}

func checkToken(ctx context.Context, expected string) error {
	if expected == "" {
		return fmt.Errorf("jwt authentication not allowed")
	}
	targetJWT, err := authenticate.ExtractBearerToken(ctx)
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
	pemCerts, err := ioutil.ReadFile(expected)
	if err != nil {
		return err
	}
	var block *pem.Block
	block, _ = pem.Decode(pemCerts)
	if block == nil {
		return fmt.Errorf("invalid expected cert")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	sn := cert.SerialNumber.String()
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

	if chains[0][0].SerialNumber.String() != sn {
		return fmt.Errorf("expected SN %q, got %q", sn, chains[0][0].SerialNumber.String())
	}

	return nil
}
