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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	wrappers "google.golang.org/protobuf/types/known/wrapperspb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
)

func TestGetTransportCredentials(t *testing.T) {
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
				CaCertificates: testCert(),
			},
			credentialSecurityProtocol: "tls",
			expectedError:              nil,
		},
		{
			name: "Fail SIMPLE TLS with InsecureSkipVerify",
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
					"cacert": []byte(testCert()),
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
					"ca.crt": []byte(testCert()),
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
				CaCertificates: testCert(),
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"cacert": []byte(base64.StdEncoding.EncodeToString([]byte(testCert()))),
				},
				Type: corev1.SecretTypeOpaque,
			},
			credentialSecurityProtocol: "tls",
			expectedError:              errors.New("only one of caCertificates or credentialName can be specified"),
		},
		{
			name: "MUTUAL TLS",
			args: &PilotArgs{Namespace: testNamespace},
			tlsSettings: &v1alpha3.ClientTLSSettings{
				Mode: v1alpha3.ClientTLSSettings_MUTUAL,
			},
			credentialSecurityProtocol: "insecure",
			expectedError:              nil,
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

func testCert() string {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Organization"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"example.com"},
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, cert, cert, &priv.PublicKey, priv)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	return string(certPEM)
}
