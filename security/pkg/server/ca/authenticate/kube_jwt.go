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

package authenticate

import (
	"fmt"
	"io/ioutil"

	"golang.org/x/net/context"

	"istio.io/istio/security/pkg/k8s/tokenreview"
)

const (
	// identityTemplate is the SPIFFE format template of the identity.
	identityTemplate = "spiffe://%s/ns/%s/sa/%s"
)

type tokenReviewClient interface {
	ValidateK8sJwt(targetJWT string) ([]string, error)
}

// KubeJWTAuthenticator authenticates K8s JWTs.
type KubeJWTAuthenticator struct {
	client      tokenReviewClient
	trustDomain string
}

// NewKubeJWTAuthenticator creates a new kubeJWTAuthenticator.
func NewKubeJWTAuthenticator(k8sAPIServerURL, caCertPath, jwtPath, trustDomain string) (*KubeJWTAuthenticator, error) {
	// Read the CA certificate of the k8s apiserver
	caCert, err := ioutil.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read the CA certificate of k8s API server: %v", err)
	}
	reviewerJWT, err := ioutil.ReadFile(jwtPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Citadel JWT: %v", err)
	}
	return &KubeJWTAuthenticator{
		client:      tokenreview.NewK8sSvcAcctAuthn(k8sAPIServerURL, caCert, string(reviewerJWT)),
		trustDomain: trustDomain,
	}, nil
}

// Authenticate authenticates the call using the K8s JWT from the context.
// The returned Caller.Identities is in SPIFFE format.
func (a *KubeJWTAuthenticator) Authenticate(ctx context.Context) (*Caller, error) {
	targetJWT, err := extractBearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("target JWT extraction error: %v", err)
	}
	id, err := a.client.ValidateK8sJwt(targetJWT)
	if err != nil {
		return nil, fmt.Errorf("failed to validate the JWT: %v", err)
	}
	if len(id) != 2 {
		return nil, fmt.Errorf("failed to parse the JWT. Validation result length is not 2, but %d", len(id))
	}
	return &Caller{
		AuthSource: AuthSourceIDToken,
		Identities: []string{fmt.Sprintf(identityTemplate, a.trustDomain, id[0], id[1])},
	}, nil
}
