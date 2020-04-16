// Copyright 2019 Istio Authors
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
	"strings"

	"golang.org/x/net/context"

	"k8s.io/client-go/kubernetes"

	v1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authenticationclient "k8s.io/client-go/kubernetes/typed/authentication/v1"

	"istio.io/istio/pkg/jwt"
	"istio.io/istio/security/pkg/k8s/tokenreview"
)

const (
	// identityTemplate is the SPIFFE format template of the identity.
	identityTemplate               = "spiffe://%s/ns/%s/sa/%s"
	KubeJWTAuthenticatorType       = "KubeJWTAuthenticator"
	RemoteKubeJWTAuthenticatorType = "RemoteKubeJWTAuthenticator"
)

type tokenReviewClient interface {
	ValidateK8sJwt(targetJWT, jwtPolicy string) ([]string, error)
}

// RemoteJWTAuthenticator authenticates remote K8s JWTs.
type RemoteJWTAuthenticator struct {
	client      authenticationclient.TokenReviewInterface
	trustDomain string
	jwtPolicy   string
}

// KubeJWTAuthenticator authenticates K8s JWTs.
type KubeJWTAuthenticator struct {
	client      tokenReviewClient
	trustDomain string
	jwtPolicy   string
}

// NewKubeJWTAuthenticator creates a new kubeJWTAuthenticator.
func NewKubeJWTAuthenticator(k8sAPIServerURL, caCertPath, jwtPath, trustDomain, jwtPolicy string) (*KubeJWTAuthenticator, error) {
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
		jwtPolicy:   jwtPolicy,
	}, nil
}

// NewRemoteJWTAuthenticator creates a new JWTAuthenticator for remote cluster.
func NewRemoteJWTAuthenticator(client kubernetes.Interface, trustDomain, jwtPolicy string) (*RemoteJWTAuthenticator, error) {
	return &RemoteJWTAuthenticator{
		client:      client.AuthenticationV1().TokenReviews(),
		trustDomain: trustDomain,
		jwtPolicy:   jwtPolicy,
	}, nil
}

func (a *KubeJWTAuthenticator) AuthenticatorType() string {
	return KubeJWTAuthenticatorType
}

// Authenticate authenticates the call using the K8s JWT from the context.
// The returned Caller.Identities is in SPIFFE format.
func (a *KubeJWTAuthenticator) Authenticate(ctx context.Context) (*Caller, error) {
	targetJWT, err := extractBearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("target JWT extraction error: %v", err)
	}
	id, err := a.client.ValidateK8sJwt(targetJWT, a.jwtPolicy)
	if err != nil {
		return nil, fmt.Errorf("failed to validate the JWT: %v", err)
	}
	if len(id) != 2 {
		return nil, fmt.Errorf("failed to parse the JWT. Validation result length is not 2, but %d", len(id))
	}
	callerNamespace := id[0]
	callerServiceAccount := id[1]
	return &Caller{
		AuthSource: AuthSourceIDToken,
		Identities: []string{fmt.Sprintf(identityTemplate, a.trustDomain, callerNamespace, callerServiceAccount)},
	}, nil
}

// Authenticate authenticates the call using the remote K8s JWT from the context.
// The returned Caller.Identities is in SPIFFE format.
func (a *RemoteJWTAuthenticator) Authenticate(ctx context.Context) (*Caller, error) {
	targetJWT, err := extractBearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("target JWT extraction error: %v", err)
	}

	id, err := a.validateRemoteK8sJwt(targetJWT)
	if err != nil {
		return nil, fmt.Errorf("failed to validate the JWT: %v", err)
	}
	if len(id) != 2 {
		return nil, fmt.Errorf("failed to parse the JWT. Validation result length is not 2, but %d", len(id))
	}
	callerNamespace := id[0]
	callerServiceAccount := id[1]
	return &Caller{
		AuthSource: AuthSourceIDToken,
		Identities: []string{fmt.Sprintf(identityTemplate, a.trustDomain, callerNamespace, callerServiceAccount)},
	}, nil
}

func (a *RemoteJWTAuthenticator) validateRemoteK8sJwt(targetJWT string) ([]string, error) {
	// SDS requires JWT to be trustworthy (has aud, exp, and mounted to the pod).
	isTrustworthyJwt, err := tokenreview.IsTrustworthyJwt(targetJWT)
	if err != nil {
		return nil, fmt.Errorf("failed to check if jwt is trustworthy: %v", err)
	}
	if !isTrustworthyJwt && a.jwtPolicy == jwt.JWTPolicyThirdPartyJWT {
		return nil, fmt.Errorf("legacy JWTs are not allowed and the provided jwt is not trustworthy")
	}
	tokenReview := &v1.TokenReview{}

	if a.jwtPolicy == jwt.JWTPolicyThirdPartyJWT {
		tokenReview.APIVersion = "authentication.k8s.io/v1"
		tokenReview.Kind = "TokenReview"
		tokenReview.Spec = v1.TokenReviewSpec{
			Token:     targetJWT,
			Audiences: []string{"istio-ca"},
		}
	} else if a.jwtPolicy == jwt.JWTPolicyFirstPartyJWT {
		tokenReview.APIVersion = "authentication.k8s.io/v1"
		tokenReview.Kind = "TokenReview"
		tokenReview.Spec = v1.TokenReviewSpec{
			Token: targetJWT,
		}
	} else {
		return nil, fmt.Errorf("invalid JWT policy: %v", a.jwtPolicy)
	}
	reviewRes, err := a.client.Create(context.TODO(), tokenReview, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to validate the JWT: %v", err)
	}

	if reviewRes.Status.Error != "" {
		return nil, fmt.Errorf("the service account authentication returns an error: %v", reviewRes.Status.Error)
	}
	inServiceAccountGroup := false
	for _, group := range reviewRes.Status.User.Groups {
		if group == "system:serviceaccounts" {
			inServiceAccountGroup = true
			break
		}
	}
	if !inServiceAccountGroup {
		return nil, fmt.Errorf("the token is not a service account")
	}
	// "username" is in the form of system:serviceaccount:{namespace}:{service account name}",
	// e.g., "username":"system:serviceaccount:default:example-pod-sa"
	subStrings := strings.Split(reviewRes.Status.User.Username, ":")
	if len(subStrings) != 4 {
		return nil, fmt.Errorf("invalid username field in the token review result")
	}
	namespace := subStrings[2]
	saName := subStrings[3]
	return []string{namespace, saName}, nil
}

func (a *RemoteJWTAuthenticator) AuthenticatorType() string {
	return RemoteKubeJWTAuthenticatorType
}
