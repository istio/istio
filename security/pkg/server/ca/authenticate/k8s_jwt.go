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

// Config for Vault prototyping purpose
const (
	jwtPath         = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	caCertPath      = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	k8sAPIServerURL = "https://kubernetes.default.svc/apis/authentication.k8s.io/v1/tokenreviews"
)

type tokenReviewClient interface {
	Review(targetJWT string) (string, error)
}

// KubeJWTAuthenticator authenticates K8s JWTs.
type KubeJWTAuthenticator struct {
	client tokenReviewClient
}

// NewKubeJWTAuthenticator creates a new kubeJWTAuthenticator.
func NewKubeJWTAuthenticator() (*KubeJWTAuthenticator, error) {
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
		client: tokenreview.NewClient(k8sAPIServerURL, caCert, string(reviewerJWT[:])),
	}, nil
}

// Authenticate authenticates the call using the K8s JWT from the context.
func (a *KubeJWTAuthenticator) Authenticate(ctx context.Context) (*Caller, error) {
	targetJWT, err := extractBearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("target JWT extraction error: %v", err)
	}
	id, err := a.client.Review(targetJWT)
	if err != nil {
		return nil, fmt.Errorf("failed to validate the JWT: %v", err)
	}
	return &Caller{
		AuthSource: AuthSourceIDToken,
		Identities: []string{id},
	}, nil
}
