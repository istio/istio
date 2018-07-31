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

package sds

import (
	"context"
	"time"
)

// SecretManager defines secrets management interface which is used by SDS.
type SecretManager interface {
	// GetSecret generates new secret and cache the secret.
	GetSecret(ctx context.Context, proxyID, spiffeID, token string) (*SecretItem, error)

	// SecretExist checks if secret already existed.
	SecretExist(proxyID, spiffeID, token, version string) bool
}

// SecretItem is the cached item in in-memory secret store.
type SecretItem struct {
	CertificateChain []byte
	PrivateKey       []byte

	// SpiffeID passed from envoy.
	SpiffeID string

	// Credential token passed from envoy, caClient uses this token to send
	// CSR to CA to sign certificate.
	Token string

	// Version is used(together with token and SpiffeID) to identify discovery request from
	// envoy which is used only for confirm purpose.
	Version string

	CreatedTime time.Time
}
