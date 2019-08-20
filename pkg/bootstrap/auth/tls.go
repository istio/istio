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

package auth

// Data source consisting of a file.
type DataSource struct {
	// Only support Filename:
	Filename string `json:"filename,omitempty"`
}

type TLSCertificate struct {
	// The TLS certificate chain.
	CertificateChain *DataSource `json:"certificate_chain,omitempty"`
	// The TLS private key.
	PrivateKey *DataSource `json:"private_key,omitempty"`
}

// TLS context shared by both client and server TLS contexts.
type CommonTLSContext struct {
	// Only a single TLS certificate is supported in client contexts.
	TLSCertificates []*TLSCertificate `json:"tls_certificates,omitempty"`
	//How to validate peer certificates
	ValidationContext *CertificateValidationContext `json:"validation_context,omitempty"`
	// Supplies the list of ALPN protocols that the listener should expose.
	AlpnProtocols []string `json:"alpn_protocols,omitempty"`
}

type UpstreamTLSContext struct {
	// Common TLS context settings.
	CommonTLSContext *CommonTLSContext `json:"common_tls_context,omitempty"`
	// SNI string to use when creating TLS backend connections.
	Sni string `json:"sni,omitempty"`
}

type CertificateValidationContext struct {
	// TLS certificate data containing certificate authority certificates to use in verifying
	// a presented peer certificate (e.g. server certificate for clusters or client certificate
	// for listeners).
	TrustedCa *DataSource `json:"trusted_ca,omitempty"`
	// An optional list of Subject Alternative Names. If specified, Envoy will verify that the
	// Subject Alternative Name of the presented certificate matches one of the specified values.
	VerifySubjectAltName []string `json:"verify_subject_alt_name,omitempty"`
}
