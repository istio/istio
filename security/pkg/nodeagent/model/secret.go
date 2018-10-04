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

// Package model contains data models for nodeagent.
package model

import "time"

// SecretItem is the cached item in in-memory secret store.
type SecretItem struct {
	CertificateChain []byte
	PrivateKey       []byte

	RootCert []byte

	// ResourceName passed from envoy SDS discovery request, spiffeID format for key/cert request.
	// "ROOTCA" for root cert request.
	ResourceName string

	// Credential token passed from envoy, caClient uses this token to send
	// CSR to CA to sign certificate.
	Token string

	// Version is used(together with token and SpiffeID) to identify discovery request from
	// envoy which is used only for confirm purpose.
	Version string

	CreatedTime time.Time
}
