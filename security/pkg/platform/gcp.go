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

package platform

import (
	"fmt"
	"istio.io/istio/pkg/spiffe"

	"cloud.google.com/go/compute/metadata"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"istio.io/istio/pkg/log"
	cred "istio.io/istio/security/pkg/credential"
)

const (
	bearerTokenScheme = "Bearer"
	httpAuthHeader    = "authorization"
)

type jwtAccess struct {
	token string
}

func (j *jwtAccess) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{
		httpAuthHeader: fmt.Sprintf("%s %s", bearerTokenScheme, j.token),
	}, nil
}

func (j *jwtAccess) RequireTransportSecurity() bool {
	return true
}

// GcpClientImpl is the implementation of GCP metadata client.
type GcpClientImpl struct {
	// Root CA cert file to validate the gRPC service in CA.
	rootCertFile string
	// Istio CA grpc server
	caAddr  string
	fetcher cred.TokenFetcher
}

// NewGcpClientImpl creates a new GcpClientImpl.
func NewGcpClientImpl(rootCert, ca string) *GcpClientImpl {
	return &GcpClientImpl{
		rootCertFile: rootCert,
		caAddr:       ca,
		// The expected token is independent of the URL of the server.
		fetcher: &cred.GcpTokenFetcher{Aud: "grpc://istio-citadel:8060"},
	}
}

// IsProperPlatform returns whether the client is on GCE.
func (ci *GcpClientImpl) IsProperPlatform() bool {
	return metadata.OnGCE()
}

// GetDialOptions returns the GRPC dial options to connect to the CA.
func (ci *GcpClientImpl) GetDialOptions() ([]grpc.DialOption, error) {
	jwtKey, err := ci.fetcher.FetchToken()
	if err != nil {
		log.Errorf("Failed to get token for dial option with error %s", err)
		return nil, err
	}

	creds, err := credentials.NewClientTLSFromFile(ci.rootCertFile, CitadelDNSSan)
	if err != nil {
		return nil, err
	}

	options := []grpc.DialOption{grpc.WithPerRPCCredentials(&jwtAccess{jwtKey}),
		grpc.WithTransportCredentials(creds)}
	return options, nil
}

// GetServiceIdentity gets the identity of the GCE service.
func (ci *GcpClientImpl) GetServiceIdentity() (string, error) {
	serviceAccount, err := ci.fetcher.FetchServiceAccount()
	if err != nil {
		log.Errorf("Failed to get service account with error: %v", err)
		return "", err
	}
	return spiffe.GenSpiffeURI("default", serviceAccount)
}

// GetAgentCredential returns the GCP JWT for the serivce account.
func (ci *GcpClientImpl) GetAgentCredential() ([]byte, error) {
	jwtKey, err := ci.fetcher.FetchToken()
	if err != nil {
		log.Errorf("Failed to get creds with error %s", err)
		return nil, err
	}

	return []byte(jwtKey), nil
}

// GetCredentialType returns the credential type as "gcp".
func (ci *GcpClientImpl) GetCredentialType() string {
	return "gcp"
}
