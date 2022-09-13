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

package kube

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc/credentials"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/pkg/log"
)

type tokenSupplier struct {
	// The token itself.  (These are public in case we need to serialize)
	Token   string
	Expires time.Time

	// regenerate tokens using this
	mu                  sync.RWMutex
	tokenNamespace      string
	tokenServiceAccount string
	audiences           []string
	expirationSeconds   int64
	kubeClient          Client
	// sunsetPeriod is how long before expiration that we start trying to renew (a minute)
	sunsetPeriod time.Duration
}

var _ credentials.PerRPCCredentials = &tokenSupplier{}

// NewRPCCredentials creates a PerRPCCredentials capable of getting tokens from Istio and tracking their expiration
func NewRPCCredentials(kubeClient Client, tokenNamespace, tokenSA string,
	tokenAudiences []string, expirationSeconds, sunsetPeriodSeconds int64,
) (credentials.PerRPCCredentials, error) {
	tokenRequest, err := createServiceAccountToken(context.TODO(), kubeClient, tokenNamespace, tokenSA, tokenAudiences, expirationSeconds)
	if err != nil {
		return nil, err
	}
	return &tokenSupplier{
		Token:   tokenRequest.Status.Token,
		Expires: tokenRequest.Status.ExpirationTimestamp.Time,

		// Save in case we need to renew during a very long-lived gRPC
		tokenNamespace:      tokenNamespace,
		tokenServiceAccount: tokenSA,
		audiences:           tokenAudiences,
		expirationSeconds:   expirationSeconds,
		sunsetPeriod:        time.Duration(sunsetPeriodSeconds) * time.Second,
		kubeClient:          kubeClient,
	}, nil
}

// GetRequestMetadata fulfills the grpc/credentials.PerRPCCredentials interface
func (its *tokenSupplier) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	its.mu.RLock()
	token := its.Token
	needRecreate := time.Until(its.Expires) < its.sunsetPeriod
	its.mu.RUnlock()

	if needRecreate {
		its.mu.Lock()
		// This checks the same condition as above.  (The outer check is to bypass the mutex when it is too early to renew)
		if time.Until(its.Expires) < its.sunsetPeriod {
			log.Debug("GetRequestMetadata will generate a new token to replace one that is about to expire")
			// We have no 'renew' method, just request a new token
			tokenRequest, err := createServiceAccountToken(ctx, its.kubeClient, its.tokenNamespace, its.tokenServiceAccount,
				its.audiences, its.expirationSeconds)
			if err == nil {
				its.Token = tokenRequest.Status.Token
				its.Expires = tokenRequest.Status.ExpirationTimestamp.Time
			} else {
				log.Infof("GetRequestMetadata failed to recreate token: %v", err.Error())
			}
		}
		token = its.Token
		its.mu.Unlock()
	}

	return map[string]string{
		"authorization": "Bearer " + token,
	}, nil
}

// RequireTransportSecurity fulfills the grpc/credentials.PerRPCCredentials interface
func (its *tokenSupplier) RequireTransportSecurity() bool {
	return false
}

func createServiceAccountToken(ctx context.Context, client Client,
	tokenNamespace, tokenServiceAccount string, audiences []string, expirationSeconds int64,
) (*authenticationv1.TokenRequest, error) {
	return client.Kube().CoreV1().ServiceAccounts(tokenNamespace).CreateToken(ctx, tokenServiceAccount,
		&authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				Audiences:         audiences,
				ExpirationSeconds: &expirationSeconds,
			},
		}, metav1.CreateOptions{})
}
