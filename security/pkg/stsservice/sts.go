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

package stsservice

import "time"

// StsResponseParameters stores all attributes sent as JSON in a successful STS
// response. These attributes are defined in
// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.1
type StsResponseParameters struct {
	// REQUIRED. The security token issued by the authorization server
	// in response to the token exchange request.
	AccessToken string `json:"access_token"`
	// REQUIRED. An identifier, representation of the issued security token.
	IssuedTokenType string `json:"issued_token_type"`
	// REQUIRED. A case-insensitive value specifying the method of using the access
	// token issued. It provides the client with information about how to utilize the
	// access token to access protected resources.
	TokenType string `json:"token_type"`
	// RECOMMENDED. The validity lifetime, in seconds, of the token issued by the
	// authorization server.
	ExpiresIn int64 `json:"expires_in"`
	// OPTIONAL, if the Scope of the issued security token is identical to the
	// Scope requested by the client; otherwise, REQUIRED.
	Scope string `json:"scope"`
	// OPTIONAL. A refresh token will typically not be issued when the exchange is
	// of one temporary credential (the subject_token) for a different temporary
	// credential (the issued token) for use in some other context.
	RefreshToken string `json:"refresh_token"`
}

// StsErrorResponse stores all Error parameters sent as JSON in a STS Error response.
// The Error parameters are defined in
// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.2.
type StsErrorResponse struct {
	// REQUIRED. A single ASCII Error code.
	Error string `json:"error"`
	// OPTIONAL. Human-readable ASCII [USASCII] text providing additional information.
	ErrorDescription string `json:"error_description"`
	// OPTIONAL. A URI identifying a human-readable web page with information
	// about the Error.
	ErrorURI string `json:"error_uri"`
}

// TokenInfo stores token information maintained at TokenManager.
type TokenInfo struct {
	TokenType  string    `json:"token_type"`
	IssueTime  time.Time `json:"issue_time"`
	ExpireTime time.Time `json:"expire_time"`
	Token      string    `json:"token"`
}

// TokensDump stores information about all generated tokens.
type TokensDump struct {
	Tokens []TokenInfo `json:"tokens"`
}
