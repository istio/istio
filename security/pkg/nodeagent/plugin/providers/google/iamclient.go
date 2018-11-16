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

// Package iamclient is for IAM integration.
package iamclient

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"github.com/golang/protobuf/ptypes"
	iam "google.golang.org/genproto/googleapis/iam/credentials/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/nodeagent/plugin"
)

var (
	scope       = []string{"https://www.googleapis.com/auth/cloud-platform"}
	iamEndpoint = "iamcredentials.googleapis.com:443"
	tlsFlag     = true
)

// Plugin for google IAM interaction.
type Plugin struct {
	iamClient iam.IAMCredentialsClient
}

// NewPlugin returns an instance of the google iam client plugin
func NewPlugin() plugin.Plugin {
	var opts grpc.DialOption
	if tlsFlag {
		pool, err := x509.SystemCertPool()
		if err != nil {
			log.Errorf("could not get SystemCertPool: %v", err)
			return nil
		}
		creds := credentials.NewClientTLSFromCert(pool, "")
		opts = grpc.WithTransportCredentials(creds)
	} else {
		opts = grpc.WithInsecure()
	}

	conn, err := grpc.Dial(iamEndpoint, opts)
	if err != nil {
		log.Errorf("Failed to connect to endpoint %q: %v", iamEndpoint, err)
		return nil
	}

	return Plugin{
		iamClient: iam.NewIAMCredentialsClient(conn),
	}
}

// ExchangeToken exchange oauth access token from trusted domain and k8s sa jwt.
func (p Plugin) ExchangeToken(ctx context.Context, trustDomain, k8sSAjwt string) (
	string /*access token*/, time.Time /*expireTime*/, error) {
	// The resource name of the service account for which the credentials are requested,
	// in the following format: projects/-/serviceAccounts/{ACCOUNT_EMAIL_OR_UNIQUEID}
	// Similar to https://cloud.google.com/iam/credentials/reference/rest/v1/projects.serviceAccounts/generateAccessToken
	req := &iam.GenerateIdentityBindingAccessTokenRequest{
		Name:  fmt.Sprintf("projects/-/serviceAccounts/%s", trustDomain),
		Scope: scope,
		Jwt:   k8sSAjwt,
	}

	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("Authorization", k8sSAjwt))
	r, err := p.iamClient.GenerateIdentityBindingAccessToken(ctx, req)
	if err != nil {
		log.Errorf("Failed to call GenerateIdentityBindingAccessToken: %v", err)
		return "", time.Now(), errors.New("failed to exchange token")
	}

	expireTime, err := ptypes.Timestamp(r.ExpireTime)
	if err != nil {
		return "", time.Now(), errors.New("failed to exchange token")
	}

	return r.AccessToken, expireTime, nil
}
