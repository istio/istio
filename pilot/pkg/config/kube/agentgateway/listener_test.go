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

package agentgateway

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// generateSelfSignedPEM returns a minimal self-signed cert/key pair as PEM bytes,
// suitable for exercising validateTLS's tls.X509KeyPair/x509.CertPool parsing.
func generateSelfSignedPEM(t *testing.T, commonName string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey() error = %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

// TestValidateTLS_CaCertMalformed is a regression test for validateTLS
// re-parsing certInfo.Cert (the already-validated leaf certificate) instead
// of certInfo.CaCert when checking the CA bundle. Pre-fix, a malformed
// CaCert with a well-formed leaf Cert silently passed validation.
func TestValidateTLS_CaCertMalformed(t *testing.T) {
	certPEM, keyPEM := generateSelfSignedPEM(t, "leaf")

	info := &TLSInfo{
		Cert:   certPEM,
		Key:    keyPEM,
		CaCert: []byte("this is not a valid PEM certificate bundle"),
	}

	if err := validateTLS(info); err == nil {
		t.Fatal("validateTLS() = nil error, want an error for a malformed CaCert bundle")
	}
}

// TestValidateTLS_Valid confirms a well-formed leaf cert/key and CA bundle
// both validate cleanly.
func TestValidateTLS_Valid(t *testing.T) {
	certPEM, keyPEM := generateSelfSignedPEM(t, "leaf")
	caPEM, _ := generateSelfSignedPEM(t, "ca")

	info := &TLSInfo{
		Cert:   certPEM,
		Key:    keyPEM,
		CaCert: caPEM,
	}

	if err := validateTLS(info); err != nil {
		t.Fatalf("validateTLS() error = %v, want nil for well-formed cert/key/CA bundle", err)
	}
}
