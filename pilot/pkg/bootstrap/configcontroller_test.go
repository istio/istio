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

package bootstrap

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	wrappers "google.golang.org/protobuf/types/known/wrapperspb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/validation/agent"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
)

func TestGetTransportCredentials(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caCertStr := testCert(1, priv, nil, []string{"example.com"})
	block, _ := pem.Decode([]byte(caCertStr))
	caCert, _ := x509.ParseCertificate(block.Bytes)
	cases := []struct {
		name                       string
		args                       *PilotArgs
		tlsSettings                *v1alpha3.ClientTLSSettings
		secret                     *corev1.Secret
		credentialSecurityProtocol string
		expectedError              error
	}{
		{
			name:                       "No TLS Setting",
			args:                       &PilotArgs{Namespace: testNamespace},
			credentialSecurityProtocol: "insecure",
			expectedError:              nil,
		},
		{
			name: "SIMPLE TLS",
			args: &PilotArgs{Namespace: testNamespace},
			tlsSettings: &v1alpha3.ClientTLSSettings{
				Mode:           v1alpha3.ClientTLSSettings_SIMPLE,
				CaCertificates: caCertStr,
			},
			credentialSecurityProtocol: "tls",
			expectedError:              nil,
		},
		{
			name: "SIMPLE TLS with InsecureSkipVerify",
			args: &PilotArgs{Namespace: testNamespace},
			tlsSettings: &v1alpha3.ClientTLSSettings{
				Mode:               v1alpha3.ClientTLSSettings_SIMPLE,
				InsecureSkipVerify: &wrappers.BoolValue{Value: true},
			},
			credentialSecurityProtocol: "tls",
			expectedError:              nil,
		},
		{
			name: "SIMPLE TLS from secrets",
			args: &PilotArgs{Namespace: testNamespace},
			tlsSettings: &v1alpha3.ClientTLSSettings{
				Mode:           v1alpha3.ClientTLSSettings_SIMPLE,
				CredentialName: "test-secret",
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"cacert": []byte(caCertStr),
				},
				Type: corev1.SecretTypeOpaque,
			},
			credentialSecurityProtocol: "tls",
			expectedError:              nil,
		},
		{
			name: "SIMPLE TLS from secrets ca.crt",
			args: &PilotArgs{Namespace: testNamespace},
			tlsSettings: &v1alpha3.ClientTLSSettings{
				Mode:           v1alpha3.ClientTLSSettings_SIMPLE,
				CredentialName: "test-secret",
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"ca.crt": []byte(caCertStr),
				},
				Type: corev1.SecretTypeOpaque,
			},
			credentialSecurityProtocol: "tls",
			expectedError:              nil,
		},
		{
			name: "Fail SIMPLE TLS from secrets not in cluster",
			args: &PilotArgs{Namespace: testNamespace},
			tlsSettings: &v1alpha3.ClientTLSSettings{
				Mode:           v1alpha3.ClientTLSSettings_SIMPLE,
				CredentialName: "test-secret",
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-test-secret",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{},
				Type: corev1.SecretTypeOpaque,
			},
			credentialSecurityProtocol: "tls",
			expectedError:              errors.New("failed to get credential with name test-secret: secrets \"test-secret\" not found"),
		},
		{
			name: "Fail SIMPLE TLS from secrets but caCert is also configured",
			args: &PilotArgs{Namespace: testNamespace},
			tlsSettings: &v1alpha3.ClientTLSSettings{
				Mode:           v1alpha3.ClientTLSSettings_SIMPLE,
				CredentialName: "test-secret",
				CaCertificates: caCertStr,
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"cacert": []byte(base64.StdEncoding.EncodeToString([]byte(caCertStr))),
				},
				Type: corev1.SecretTypeOpaque,
			},
			credentialSecurityProtocol: "tls",
			expectedError:              errors.New("cannot specify client certificates or CA certificate or CA CRL If credentialName is set"),
		},
		{
			name: "MUTUAL TLS",
			args: &PilotArgs{Namespace: testNamespace},
			tlsSettings: &v1alpha3.ClientTLSSettings{
				Mode:              v1alpha3.ClientTLSSettings_MUTUAL,
				CaCertificates:    caCertStr,
				ClientCertificate: testCert(5, priv, caCert, []string{"example.com"}),
				PrivateKey:        priv.D.String(),
			},
			credentialSecurityProtocol: "tls",
			expectedError:              nil,
		},
		{
			name: "Fail MUTUAL TLS without client cert and key",
			args: &PilotArgs{Namespace: testNamespace},
			tlsSettings: &v1alpha3.ClientTLSSettings{
				Mode:           v1alpha3.ClientTLSSettings_MUTUAL,
				CaCertificates: caCertStr,
			},
			credentialSecurityProtocol: "tls",
			expectedError:              agent.AppendErrors(fmt.Errorf("client certificate required for mutual tls"), fmt.Errorf("private key required for mutual tls")),
		},
		{
			name: "MUTUAL TLS from secrets",
			args: &PilotArgs{Namespace: testNamespace},
			tlsSettings: &v1alpha3.ClientTLSSettings{
				Mode:           v1alpha3.ClientTLSSettings_MUTUAL,
				CredentialName: "test-secret",
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"cacert": []byte(caCertStr),
					"cert":   []byte(testCert(5, priv, caCert, []string{"example.com"})),
					"key":    []byte(priv.D.String()),
				},
				Type: corev1.SecretTypeOpaque,
			},
			credentialSecurityProtocol: "tls",
			expectedError:              nil,
		},
		{
			name: "Fail MUTUAL TLS from secrets without client cert and key",
			args: &PilotArgs{Namespace: testNamespace},
			tlsSettings: &v1alpha3.ClientTLSSettings{
				Mode:           v1alpha3.ClientTLSSettings_MUTUAL,
				CredentialName: "test-secret",
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"cacert": []byte(caCertStr),
				},
				Type: corev1.SecretTypeOpaque,
			},
			credentialSecurityProtocol: "tls",
			expectedError:              fmt.Errorf("found secret, but didn't have expected keys (cert and key) or (tls.crt and tls.key); found: cacert"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Server{}
			if c.secret != nil {
				client := kube.NewFakeClient(c.secret)
				stop := test.NewStop(t)
				client.RunAndWait(stop)
				s = &Server{kubeClient: client}
			}
			if creds, err := s.getTransportCredentials(c.args, c.tlsSettings); err != nil {
				if c.expectedError == nil {
					t.Errorf("unexpected getTransportCredentials error: %v", err)
				} else if c.expectedError.Error() != err.Error() {
					t.Errorf("expected getTransportCredentials error: %v got: %v", c.expectedError.Error(), err.Error())
				}
			} else if c.expectedError != nil {
				t.Errorf("got not error but expected getTransportCredentials error: %v", c.expectedError.Error())
			} else if creds.Info().SecurityProtocol != c.credentialSecurityProtocol {
				t.Errorf("expected credential security protocol %v got: %v", c.credentialSecurityProtocol, creds.Info().SecurityProtocol)
			}
		})
	}
}

func TestVerifyCert(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caCertStr := testCert(1, priv, nil, []string{"example.com"})
	block, _ := pem.Decode([]byte(caCertStr))
	caCert, _ := x509.ParseCertificate(block.Bytes)
	cases := []struct {
		name             string
		sans             []string
		certDomainNames  []string
		crlNums          []int64
		certSerialNumber int64
		expectedErr      error
	}{
		{
			name:             "Cert serial number in revoked list",
			sans:             nil,
			certDomainNames:  []string{"example.com"},
			crlNums:          []int64{5, 7},
			certSerialNumber: 5,
			expectedErr:      fmt.Errorf("certificate is revoked"),
		},
		{
			name:             "Cert serial number not in revoked list",
			sans:             nil,
			certDomainNames:  []string{"example.com"},
			crlNums:          []int64{5, 7},
			certSerialNumber: 3,
			expectedErr:      nil,
		},
		{
			name:             "Cert domain names not in SANs list",
			sans:             []string{"google.com", "istio.io"},
			certDomainNames:  []string{"example.com"},
			crlNums:          nil,
			certSerialNumber: 3,
			expectedErr:      fmt.Errorf("no matching SAN found"),
		},
		{
			name:             "Cert domain name in SANs list",
			sans:             []string{"example.com", "istio.io"},
			certDomainNames:  []string{"example.com"},
			crlNums:          nil,
			certSerialNumber: 3,
			expectedErr:      nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			crl := testCRL(caCert, priv, c.crlNums)
			certStr := testCert(c.certSerialNumber, priv, caCert, c.certDomainNames)
			s := Server{}
			block, _ = pem.Decode([]byte(certStr))
			actualErr := s.verifyCert([][]byte{block.Bytes}, &v1alpha3.ClientTLSSettings{SubjectAltNames: c.sans, CaCrl: crl})
			if actualErr == nil && c.expectedErr != nil {
				t.Errorf("expected verifyCert error: %v", c.expectedErr.Error())
			} else if c.expectedErr == nil && actualErr != nil {
				t.Errorf("unexpected verifyCert error: %v", actualErr.Error())
			} else if actualErr != nil && c.expectedErr != nil && actualErr.Error() != c.expectedErr.Error() {
				t.Errorf("expected error: %v, got error: %v", c.expectedErr.Error(), actualErr.Error())
			}
		})
	}
}

func testCert(sno int64, priv *ecdsa.PrivateKey, issuer *x509.Certificate, dnsNames []string) string {
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(sno),
		Subject: pkix.Name{
			Organization: []string{"Test Organization"},
			CommonName:   "localhost",
		},
		SubjectKeyId:          []byte("local"),
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCRLSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
	}
	if issuer == nil {
		issuer = cert
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, cert, issuer, &priv.PublicKey, priv)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	return string(certPEM)
}

func testCRL(caCert *x509.Certificate, caKey crypto.Signer, revokedSerials []int64) string {
	var revokedCerts []x509.RevocationListEntry
	for _, revokedSerial := range revokedSerials {
		revokedCerts = append(revokedCerts, x509.RevocationListEntry{
			SerialNumber:   big.NewInt(revokedSerial),
			RevocationTime: time.Now(),
		})
	}
	crlTemplate := x509.RevocationList{
		SignatureAlgorithm:        x509.ECDSAWithSHA256,
		RevokedCertificateEntries: revokedCerts,
		Number:                    big.NewInt(1),
		ThisUpdate:                time.Now(),
		NextUpdate:                time.Now().Add(365 * 24 * time.Hour),
	}
	crlBytes, err := x509.CreateRevocationList(rand.Reader, &crlTemplate, caCert, caKey)
	if err != nil {
		panic(err)
	}
	crlPEM := pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: crlBytes})
	return string(crlPEM)
}
