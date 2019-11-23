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

package stsservice

import (
	"fmt"
	"sync"

	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/security/pkg/pki/util"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Config for the STS server.
type Config struct {
	LocalHostAddr string
	LocalPort 		uint16
	NodeType      model.NodeType
	stsServer     string
}

// StsRequestAttributes stores all STS request attributes defined in
// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.1
type StsRequestAttributes struct {
	grantType            string  // Required
	resource             string
	audience             string
	scope                string
	requestedTokenType   string
	subjectToken         string  // Required
	subjectTokenType     string  // Required
	actorToken           string
	actorTokenType       string
}

// StsResponseAttributes stores all attributes sent as JSON in a successful STS
// response. These attributes are defined in
// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.1
type StsResponseAttributes struct {
	accessToken 		string `json:"access_token"` // Required
	issuedTokenType string `json:"issued_token_type"`  // Required
	tokenType       string `json:"token_type"`  // Required
	expiresIn       string `json:"expires_in"`
	scope           string `json:"scope"`
  refreshToken    string `json:"refresh_token"`
}

// StsErrorResponse stores all error information sent as JSON in a STS error response.
// The response attributes are defined in
// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.2
type StsErrorResponse struct {

}

type StsResponse struct {
	success StsResponseAttributes
	failure StsErrorResponse
}

// tokenManager contains methods for fetching token.
type tokenManager interface {
	FetchToken(attributes StsRequestAttributes) StsResponse
}

// Server provides an endpoint for handling security token service (STS) requests.
type Server struct {

}

// NewServer creates a new status server.
func NewServer(config Config) (*Server, error) {
}