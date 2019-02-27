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

package model

import (
	"time"
)

// Args is configuration for the CA client.
type Args struct {
	// Organization presented in the certificates
	Org string

	// Requested TTL of the certificates
	RequestedCertTTL time.Duration

	// Size of RSA private key
	RSAKeySize int

	// Whether the certificate is for CA
	ForCA bool

	// CertFile defines the cert of the CA client.
	CertFile string

	// CertChainFile defines the cert chain file of the CA client, including the client's cert.
	CertChainFile string

	// KeyFile defines the private key of the CA client.
	KeyFile string

	// RootCertFile defines the root cert of the CA client.
	RootCertFile string

	//IsSelfSigned defines self signed
	IsSelfSigned bool

	CertTTL time.Duration

	MaxCertTTL time.Duration
}

// DefaultArgs allocates a Config struct initialized.
func DefaultArgs() *Args {
	return &Args{
		Org:              "org",
		RequestedCertTTL: 365 * 24 * time.Hour,
		RSAKeySize:       2048,
		ForCA:            true,
		IsSelfSigned:     true,
		CertFile:         "/etc/certs/cert-chain.pem",
		CertChainFile:    "/etc/certs/cert-chain.pem",
		KeyFile:          "/etc/certs/key.pem",
		RootCertFile:     "/etc/certs/root-cert.pem",
		CertTTL:          90 * 24 * time.Hour,
		MaxCertTTL:       90 * 24 * time.Hour,
	}
}
