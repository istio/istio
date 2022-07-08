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

package ca

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	k8ssecret "istio.io/istio/security/pkg/k8s/secret"
	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
)

var (
	cert1Pem = `
-----BEGIN CERTIFICATE-----
MIIC3jCCAcagAwIBAgIJAMwyWk0iqlOoMA0GCSqGSIb3DQEBCwUAMBwxGjAYBgNV
BAoMEWs4cy5jbHVzdGVyLmxvY2FsMB4XDTE4MDkyMTAyMjAzNFoXDTI4MDkxODAy
MjAzNFowHDEaMBgGA1UECgwRazhzLmNsdXN0ZXIubG9jYWwwggEiMA0GCSqGSIb3
DQEBAQUAA4IBDwAwggEKAoIBAQC8TDtfy23OKCRnkSYrKZwuHG5lOmTZgLwoFR1h
3NDTkjR9406CjnAy6Gl73CRG3zRYVgY/2dGNqTzAKRCeKZlOzBlK6Kilb0NIJ6it
s6ooMAxwXlr7jOKiSn6xbaexVMrP0VPUbCgJxQtGs3++hQ14D6WnyfdzPBZJLKbI
tVdDnAcl/FJXKVV9gIg+MM0gETWOYj5Yd8Ye0FTvoFcgs8NKkxhEZe/LeYa7XYsk
S0PymwbHwNZcfC4znp2bzu28LUmUe6kL97YU8ubvhR0muRy6h5MnQNMQrRG5Q5j4
A2+tkO0vto8gOb6/lacEUVYuQdSkMZJiqWEjWgWKeAYdkTJDAgMBAAGjIzAhMA4G
A1UdDwEB/wQEAwICBDAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IB
AQAxWP3MT0IelJcb+e7fNTfMS0r3UhpiNkRU368Z7gJ4tDNOGRPzntW6CLnaE+3g
IjOMAE8jlXeEmNuXtDQqQoZwWc1D5ma3jyc83E5H9LJzjfmn5rAHafr29YH85Ms2
VlKdpP+teYg8Cag9u4ar/AUR4zMUEpGK5U+T9IH44lVqVH23T+DxAT+btsyuGiB0
DsM76XVDj4g3OKCUalu7a8FHvgTkBpUJBl7vwh9kqo9HwCaj4iC2CwveOm0WtSgy
K9PpVDxTGNSxqsxKn7DJQ15NTOP+gr29ABqFKwRr+S8ggw6evzHbABQTUMebaRSr
iH7cSgrzZBiUvJmZRi7/BrYU
-----END CERTIFICATE-----`

	key1Pem = `
-----BEGIN PRIVATE KEY-----
MIIEwAIBADANBgkqhkiG9w0BAQEFAASCBKowggSmAgEAAoIBAQC8TDtfy23OKCRn
kSYrKZwuHG5lOmTZgLwoFR1h3NDTkjR9406CjnAy6Gl73CRG3zRYVgY/2dGNqTzA
KRCeKZlOzBlK6Kilb0NIJ6its6ooMAxwXlr7jOKiSn6xbaexVMrP0VPUbCgJxQtG
s3++hQ14D6WnyfdzPBZJLKbItVdDnAcl/FJXKVV9gIg+MM0gETWOYj5Yd8Ye0FTv
oFcgs8NKkxhEZe/LeYa7XYskS0PymwbHwNZcfC4znp2bzu28LUmUe6kL97YU8ubv
hR0muRy6h5MnQNMQrRG5Q5j4A2+tkO0vto8gOb6/lacEUVYuQdSkMZJiqWEjWgWK
eAYdkTJDAgMBAAECggEBAJTemFqmVQwWxKF1Kn4ZibcTF1zFDBLCKwBtoStMD3YW
M5YL7nhd8OruwOcCJ1Q5CAOHD63PolOjp7otPUwui1y3FJAa3areCo2zfTLHxxG6
2zrD/p6+xjeVOhFBJsGWzjn7v5FEaWs/9ChTpf2U6A8yH8BGd3MN4Hi96qboaDO0
fFz3zOu7sgjkDNZiapZpUuqs7a6MCCr2T3FPwdWUiILZF2t5yWd/l8KabP+3QvvR
tDU6sNv4j8e+dsF2l9ZT81JLkN+f6HvWcLVAADvcBqMcd8lmMSPgxSbytzKanx7o
wtzIiGkNZBCVKGO7IK2ByCluiyHDpGul60Th7HUluDECgYEA9/Q1gT8LTHz1n6vM
2n2umQN9R+xOaEYN304D5DQqptN3S0BCJ4dihD0uqEB5osstRTf4QpP/qb2hMDP4
qWbWyrc7Z5Lyt6HI1ly6VpVnYKb3HDeJ9M+5Se1ttdwyRCzuT4ZBhT5bbqBatsOU
V7+dyrJKbk8r9K4qy29UFozz/38CgYEAwmhzPVak99rVmqTpe0gPERW//n+PdW3P
Ta6ongU8zkkw9LAFwgjGtNpd4nlk0iQigiM4jdJDFl6edrRXv2cisEfJ9+s53AOb
hXui4HAn2rusPK+Dq2InkHYTGjEGDpx94zC/bjYR1GBIsthIh0w2G9ql8yvLatxG
x6oXEsb7Lz0CgYEA7Oj+/mDYUNrMbSVfdBvF6Rl2aHQWbncQ5h3Khg55+i/uuY3K
J66pqKQ0ojoIfk0XEh3qLOLv0qUHD+F4Y5OJAuOT9OBo3J/OH1M2D2hs/+JIFUPT
on+fEE21F6AuvwkXIhCrJb5w6gB47Etuv3CsOXGkwEURQJXw+bODapB+yc0CgYEA
t7zoTay6NdcJ0yLR2MZ+FvOrhekhuSaTqyPMEa15jq32KwzCJGUPCJbp7MY217V3
N+/533A+H8JFmoNP+4KKcnknFb2n7Z0rO7licyUNRdniK2jm1O/r3Mj7vOFgjCaz
hCnqg0tvBn4Jt55aziTlbuXzuiRGGTUfYE4NiJ2vgTECgYEA8di9yqGhETYQkoT3
E70JpEmkCWiHl/h2ClLcDkj0gXKFxmhzmvs8G5On4S8toNiJ6efmz0KlHN1F7Ldi
2iVd9LZnFVP1YwG0mvTJxxc5P5Uy5q/EhCLBAetqoTkWYlPcpkcathmCbCpJG4/x
iOmuuOfQWnMfcVk8I0YDL5+G9Pg=
-----END PRIVATE KEY-----`
)

// TODO (myidpt): Test Istio CA can load plugin key/certs from secret.

func TestCreateSelfSignedIstioCAWithoutSecret(t *testing.T) {
	caCertTTL := time.Hour
	defaultCertTTL := 30 * time.Minute
	maxCertTTL := time.Hour
	org := "test.ca.Org"
	const caNamespace = "default"
	client := fake.NewSimpleClientset()
	rootCertFile := ""
	rootCertCheckInverval := time.Hour
	rsaKeySize := 2048

	caopts, err := NewSelfSignedIstioCAOptions(context.Background(),
		0, caCertTTL, rootCertCheckInverval, defaultCertTTL,
		maxCertTTL, org, false, caNamespace, -1, client.CoreV1(),
		rootCertFile, false, rsaKeySize)
	if err != nil {
		t.Fatalf("Failed to create a self-signed CA Options: %v", err)
	}

	ca, err := NewIstioCA(caopts)
	if err != nil {
		t.Errorf("Got error while creating self-signed CA: %v", err)
	}
	if ca == nil {
		t.Fatalf("Failed to create a self-signed CA.")
	}

	signingCert, _, certChainBytes, rootCertBytes := ca.GetCAKeyCertBundle().GetAll()
	rootCert, err := util.ParsePemEncodedCertificate(rootCertBytes)
	if err != nil {
		t.Error(err)
	}
	// Root cert and siging cert are the same for self-signed CA.
	if !rootCert.Equal(signingCert) {
		t.Error("CA root cert does not match signing cert")
	}

	if ttl := rootCert.NotAfter.Sub(rootCert.NotBefore); ttl != caCertTTL {
		t.Errorf("Unexpected CA certificate TTL (expecting %v, actual %v)", caCertTTL, ttl)
	}

	if certOrg := rootCert.Issuer.Organization[0]; certOrg != org {
		t.Errorf("Unexpected CA certificate organization (expecting %v, actual %v)", org, certOrg)
	}

	if len(certChainBytes) != 0 {
		t.Errorf("Cert chain should be empty")
	}

	// Check the signing cert stored in K8s secret.
	caSecret, err := client.CoreV1().Secrets("default").Get(context.TODO(), CASecret, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get secret (error: %s)", err)
	}

	signingCertFromSecret, err := util.ParsePemEncodedCertificate(caSecret.Data[CACertFile])
	if err != nil {
		t.Errorf("Failed to parse cert (error: %s)", err)
	}

	if !signingCertFromSecret.Equal(signingCert) {
		t.Error("CA signing cert does not match the K8s secret")
	}
}

func TestCreateSelfSignedIstioCAWithSecret(t *testing.T) {
	rootCertPem := cert1Pem
	// Use the same signing cert and root cert for self-signed CA.
	signingCertPem := []byte(cert1Pem)
	signingKeyPem := []byte(key1Pem)

	client := fake.NewSimpleClientset()
	initSecret := k8ssecret.BuildSecret("", CASecret, "default",
		nil, nil, nil, signingCertPem, signingKeyPem, istioCASecretType)
	_, err := client.CoreV1().Secrets("default").Create(context.TODO(), initSecret, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Failed to create secret (error: %s)", err)
	}

	caCertTTL := time.Hour
	defaultCertTTL := 30 * time.Minute
	maxCertTTL := time.Hour
	org := "test.ca.Org"
	caNamespace := "default"
	const rootCertFile = ""
	rootCertCheckInverval := time.Hour
	rsaKeySize := 2048

	caopts, err := NewSelfSignedIstioCAOptions(context.Background(),
		0, caCertTTL, rootCertCheckInverval, defaultCertTTL, maxCertTTL,
		org, false, caNamespace, -1, client.CoreV1(),
		rootCertFile, false, rsaKeySize)
	if err != nil {
		t.Fatalf("Failed to create a self-signed CA Options: %v", err)
	}

	ca, err := NewIstioCA(caopts)
	if err != nil {
		t.Errorf("Got error while creating self-signed CA: %v", err)
	}
	if ca == nil {
		t.Fatalf("Failed to create a self-signed CA.")
	}

	signingCert, err := util.ParsePemEncodedCertificate(signingCertPem)
	if err != nil {
		t.Errorf("Failed to parse cert (error: %s)", err)
	}

	signingCertFromCA, _, certChainBytesFromCA, rootCertBytesFromCA := ca.GetCAKeyCertBundle().GetAll()

	if !signingCert.Equal(signingCertFromCA) {
		t.Error("Signing cert does not match")
	}

	if !bytes.Equal(rootCertBytesFromCA, []byte(rootCertPem)) {
		t.Error("Root cert does not match")
	}

	if len(certChainBytesFromCA) != 0 {
		t.Errorf("Cert chain should be empty")
	}
}

func TestCreateSelfSignedIstioCAReadSigningCertOnly(t *testing.T) {
	rootCertPem := cert1Pem
	// Use the same signing cert and root cert for self-signed CA.
	signingCertPem := []byte(cert1Pem)
	signingKeyPem := []byte(key1Pem)

	caCertTTL := time.Hour
	defaultCertTTL := 30 * time.Minute
	maxCertTTL := time.Hour
	org := "test.ca.Org"
	caNamespace := "default"
	const rootCertFile = ""
	rootCertCheckInverval := time.Hour
	rsaKeySize := 2048

	client := fake.NewSimpleClientset()

	// Should abort with timeout.
	expectedErr := "secret waiting thread is terminated"
	ctx0, cancel0 := context.WithTimeout(context.Background(), time.Millisecond*50)
	defer cancel0()
	_, err := NewSelfSignedIstioCAOptions(ctx0, 0,
		caCertTTL, defaultCertTTL, rootCertCheckInverval, maxCertTTL, org, false,
		caNamespace, time.Millisecond*10, client.CoreV1(), rootCertFile, false, rsaKeySize)
	if err == nil {
		t.Errorf("Expected error, but succeeded.")
	} else if err.Error() != expectedErr {
		t.Errorf("Unexpected error message: %s VS (expected) %s", err.Error(), expectedErr)
		return
	}

	// Should succeed once secret is ready.
	secret := k8ssecret.BuildSecret("", CASecret, "default", nil, nil, nil, signingCertPem, signingKeyPem, istioCASecretType)
	_, err = client.CoreV1().Secrets("default").Create(context.TODO(), secret, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Failed to create secret (error: %s)", err)
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	caopts, err := NewSelfSignedIstioCAOptions(ctx1, 0,
		caCertTTL, defaultCertTTL, rootCertCheckInverval, maxCertTTL, org, false,
		caNamespace, time.Millisecond*10, client.CoreV1(), rootCertFile, false, rsaKeySize)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	ca, err := NewIstioCA(caopts)
	if err != nil {
		t.Errorf("Got error while creating self-signed CA: %v", err)
	}
	if ca == nil {
		t.Fatalf("Failed to create a self-signed CA.")
	}

	signingCert, err := util.ParsePemEncodedCertificate(signingCertPem)
	if err != nil {
		t.Errorf("Failed to parse cert (error: %s)", err)
	}

	signingCertFromCA, _, certChainBytesFromCA, rootCertBytesFromCA := ca.GetCAKeyCertBundle().GetAll()

	if !signingCert.Equal(signingCertFromCA) {
		t.Error("Signing cert does not match")
	}

	if !bytes.Equal(rootCertBytesFromCA, []byte(rootCertPem)) {
		t.Error("Root cert does not match")
	}

	if len(certChainBytesFromCA) != 0 {
		t.Errorf("Cert chain should be empty")
	}
}

func TestCreatePluggedCertCA(t *testing.T) {
	rootCertFile := "../testdata/multilevelpki/root-cert.pem"
	certChainFile := []string{"../testdata/multilevelpki/int2-cert-chain.pem"}
	signingCertFile := "../testdata/multilevelpki/int2-cert.pem"
	signingKeyFile := "../testdata/multilevelpki/int2-key.pem"
	rsaKeySize := 2048

	defaultWorkloadCertTTL := 99999 * time.Hour
	maxWorkloadCertTTL := time.Hour

	caopts, err := NewPluggedCertIstioCAOptions(SigningCAFileBundle{rootCertFile, certChainFile, signingCertFile, signingKeyFile},
		defaultWorkloadCertTTL, maxWorkloadCertTTL, rsaKeySize)
	if err != nil {
		t.Fatalf("Failed to create a plugged-cert CA Options: %v", err)
	}

	t0 := time.Now()
	ca, err := NewIstioCA(caopts)
	if err != nil {
		t.Errorf("Got error while creating plugged-cert CA: %v", err)
	}
	if ca == nil {
		t.Fatalf("Failed to create a plugged-cert CA.")
	}

	signingCertBytes, signingKeyBytes, certChainBytes, rootCertBytes := ca.GetCAKeyCertBundle().GetAllPem()
	if !comparePem(signingCertBytes, signingCertFile) {
		t.Errorf("Failed to verify loading of signing cert pem.")
	}
	if !comparePem(signingKeyBytes, signingKeyFile) {
		t.Errorf("Failed to verify loading of signing key pem.")
	}
	if !comparePem(certChainBytes, certChainFile[0]) {
		t.Errorf("Failed to verify loading of cert chain pem.")
	}
	if !comparePem(rootCertBytes, rootCertFile) {
		t.Errorf("Failed to verify loading of root cert pem.")
	}

	certChain, err := util.ParsePemEncodedCertificate(certChainBytes)
	if err != nil {
		t.Fatalf("Failed to parse cert chain pem.")
	}
	// if CA cert becomes invalid before workload cert it's going to cause workload cert to be invalid too,
	// however citatel won't rotate if that happens
	delta := certChain.NotAfter.Sub(t0.Add(ca.defaultCertTTL))
	if delta >= time.Second*2 {
		t.Errorf("Invalid default cert TTL, should be the same as cert chain: %v VS (expected) %v",
			t0.Add(ca.defaultCertTTL),
			certChain.NotAfter)
	}
}

func TestSignCSR(t *testing.T) {
	subjectID := "spiffe://example.com/ns/foo/sa/bar"
	cases := map[string]struct {
		forCA         bool
		certOpts      util.CertOptions
		maxTTL        time.Duration
		requestedTTL  time.Duration
		verifyFields  util.VerifyFields
		expectedError string
	}{
		"Workload uses RSA": {
			forCA: false,
			certOpts: util.CertOptions{
				// This value is not used, instead, subjectID should be used in certificate.
				Host:       "spiffe://different.com/test",
				RSAKeySize: 2048,
				IsCA:       false,
			},
			maxTTL:       time.Hour,
			requestedTTL: 30 * time.Minute,
			verifyFields: util.VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				IsCA:        false,
				Host:        subjectID,
			},
			expectedError: "",
		},
		"Workload uses EC": {
			forCA: false,
			certOpts: util.CertOptions{
				// This value is not used, instead, subjectID should be used in certificate.
				Host:     "spiffe://different.com/test",
				ECSigAlg: util.EcdsaSigAlg,
				IsCA:     false,
			},
			maxTTL:       time.Hour,
			requestedTTL: 30 * time.Minute,
			verifyFields: util.VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				IsCA:        false,
				Host:        subjectID,
			},
			expectedError: "",
		},
		"CA uses RSA": {
			forCA: true,
			certOpts: util.CertOptions{
				RSAKeySize: 2048,
				IsCA:       true,
			},
			maxTTL:       365 * 24 * time.Hour,
			requestedTTL: 30 * 24 * time.Hour,
			verifyFields: util.VerifyFields{
				KeyUsage: x509.KeyUsageCertSign,
				IsCA:     true,
				Host:     subjectID,
			},
			expectedError: "",
		},
		"CA uses EC": {
			forCA: true,
			certOpts: util.CertOptions{
				ECSigAlg: util.EcdsaSigAlg,
				IsCA:     true,
			},
			maxTTL:       365 * 24 * time.Hour,
			requestedTTL: 30 * 24 * time.Hour,
			verifyFields: util.VerifyFields{
				KeyUsage: x509.KeyUsageCertSign,
				IsCA:     true,
				Host:     subjectID,
			},
			expectedError: "",
		},
		"CSR uses RSA TTL error": {
			forCA: false,
			certOpts: util.CertOptions{
				Org:        "istio.io",
				RSAKeySize: 2048,
			},
			maxTTL:        2 * time.Hour,
			requestedTTL:  3 * time.Hour,
			expectedError: "requested TTL 3h0m0s is greater than the max allowed TTL 2h0m0s",
		},
		"CSR uses EC TTL error": {
			forCA: false,
			certOpts: util.CertOptions{
				Org:      "istio.io",
				ECSigAlg: util.EcdsaSigAlg,
			},
			maxTTL:        2 * time.Hour,
			requestedTTL:  3 * time.Hour,
			expectedError: "requested TTL 3h0m0s is greater than the max allowed TTL 2h0m0s",
		},
	}

	for id, tc := range cases {
		csrPEM, keyPEM, err := util.GenCSR(tc.certOpts)
		if err != nil {
			t.Errorf("%s: GenCSR error: %v", id, err)
		}

		ca, err := createCA(tc.maxTTL, tc.certOpts.ECSigAlg)
		if err != nil {
			t.Errorf("%s: createCA error: %v", id, err)
		}

		caCertOpts := CertOpts{
			SubjectIDs: []string{subjectID},
			TTL:        tc.requestedTTL,
			ForCA:      tc.forCA,
		}
		certPEM, signErr := ca.Sign(csrPEM, caCertOpts)
		if signErr != nil {
			if tc.expectedError == "" {
				t.Errorf("%s: Sign error: %v", id, err)
			}
			if certPEM != nil {
				t.Errorf("%s: Expected null cert be obtained a non-null cert.", id)
			}
			if signErr.(*caerror.Error).Error() != tc.expectedError {
				t.Errorf("%s: Expected error: %s but got error: %s.", id, tc.expectedError, signErr.(*caerror.Error).Error())
			}
			continue
		}

		_, _, certChainBytes, rootCertBytes := ca.GetCAKeyCertBundle().GetAll()
		if err = util.VerifyCertificate(
			keyPEM, append(certPEM, certChainBytes...), rootCertBytes, &tc.verifyFields); err != nil {
			t.Errorf("%s: VerifyCertificate error: %v", id, err)
		}

		cert, err := util.ParsePemEncodedCertificate(certPEM)
		if err != nil {
			t.Errorf("%s: ParsePemEncodedCertificate error: %v", id, err)
		}

		if ttl := cert.NotAfter.Sub(cert.NotBefore) - util.ClockSkewGracePeriod; ttl != tc.requestedTTL {
			t.Errorf("%s: Unexpected certificate TTL (expecting %v, actual %v)", id, tc.requestedTTL, ttl)
		}
		san := util.ExtractSANExtension(cert.Extensions)
		if san == nil {
			t.Errorf("%s: No SAN extension is found in the certificate", id)
		}
		expected, err := util.BuildSubjectAltNameExtension(subjectID)
		if err != nil {
			t.Errorf("%s: BuildSubjectAltNameExtension error: %v", id, err)
		}
		if !reflect.DeepEqual(expected, san) {
			t.Errorf("%s: Unexpected extensions: wanted %v but got %v", id, expected, san)
		}
	}
}

func TestAppendRootCerts(t *testing.T) {
	root1 := "root-cert-1"
	expRootCerts := `root-cert-1
root-cert-2
root-cert-3`
	rootCerts, err := util.AppendRootCerts([]byte(root1), "./root-certs-for-testing.pem")
	if err != nil {
		t.Errorf("AppendRootCerts() returns an error: %v", err)
	} else if expRootCerts != string(rootCerts) {
		t.Errorf("the root certificates do not match. Expect:%v. Actual:%v.",
			expRootCerts, string(rootCerts))
	}
}

func TestAppendRootCertsToNullCert(t *testing.T) {
	// nil certificate
	var root1 []byte
	expRootCerts := `root-cert-2
root-cert-3`
	rootCerts, err := util.AppendRootCerts(root1, "./root-certs-for-testing.pem")
	if err != nil {
		t.Errorf("AppendRootCerts() returns an error: %v", err)
	} else if expRootCerts != string(rootCerts) {
		t.Errorf("the root certificates do not match. Expect:%v. Actual:%v.",
			expRootCerts, string(rootCerts))
	}
}

func TestSignWithCertChain(t *testing.T) {
	rootCertFile := "../testdata/multilevelpki/root-cert.pem"
	certChainFile := []string{"../testdata/multilevelpki/int-cert-chain.pem"}
	signingCertFile := "../testdata/multilevelpki/int-cert.pem"
	signingKeyFile := "../testdata/multilevelpki/int-key.pem"
	rsaKeySize := 2048

	defaultWorkloadCertTTL := 30 * time.Minute
	maxWorkloadCertTTL := time.Hour

	caopts, err := NewPluggedCertIstioCAOptions(SigningCAFileBundle{rootCertFile, certChainFile, signingCertFile, signingKeyFile},
		defaultWorkloadCertTTL, maxWorkloadCertTTL, rsaKeySize)
	if err != nil {
		t.Fatalf("Failed to create a plugged-cert CA Options: %v", err)
	}

	ca, err := NewIstioCA(caopts)
	if err != nil {
		t.Errorf("Got error while creating plugged-cert CA: %v", err)
	}
	if ca == nil {
		t.Fatalf("Failed to create a plugged-cert CA.")
	}

	opts := util.CertOptions{
		// This value is not used, instead, subjectID should be used in certificate.
		Host:       "spiffe://different.com/test",
		RSAKeySize: 2048,
		IsCA:       false,
	}
	csrPEM, privPEM, err := util.GenCSR(opts)
	if err != nil {
		t.Error(err)
	}

	caCertOpts := CertOpts{
		SubjectIDs: []string{"localhost"},
		TTL:        time.Hour,
		ForCA:      false,
	}
	certPEM, signErr := ca.signWithCertChain(csrPEM, caCertOpts.SubjectIDs, caCertOpts.TTL, true, caCertOpts.ForCA)

	if signErr != nil {
		t.Error(err)
	}

	cert, err := tls.X509KeyPair(certPEM, privPEM)
	if err != nil {
		t.Error(err)
	}

	if len(cert.Certificate) != 3 {
		t.Errorf("Unexpected number of certificates returned: %d (expected 4)", len(cert.Certificate))
	}
}

func TestGenKeyCert(t *testing.T) {
	cases := map[string]struct {
		rootCertFile      string
		certChainFile     []string
		signingCertFile   string
		signingKeyFile    string
		certLifetime      time.Duration
		checkCertLifetime bool
		expectedError     string
	}{
		"RSA cryptography": {
			rootCertFile:      "../testdata/multilevelpki/root-cert.pem",
			certChainFile:     []string{"../testdata/multilevelpki/int-cert-chain.pem"},
			signingCertFile:   "../testdata/multilevelpki/int-cert.pem",
			signingKeyFile:    "../testdata/multilevelpki/int-key.pem",
			certLifetime:      3650 * 24 * time.Hour,
			checkCertLifetime: false,
			expectedError:     "",
		},
		"EC cryptography": {
			rootCertFile:      "../testdata/multilevelpki/ecc-root-cert.pem",
			certChainFile:     []string{"../testdata/multilevelpki/ecc-int-cert-chain.pem"},
			signingCertFile:   "../testdata/multilevelpki/ecc-int-cert.pem",
			signingKeyFile:    "../testdata/multilevelpki/ecc-int-key.pem",
			certLifetime:      3650 * 24 * time.Hour,
			checkCertLifetime: false,
			expectedError:     "",
		},
		"Pass lifetime check": {
			rootCertFile:      "../testdata/multilevelpki/ecc-root-cert.pem",
			certChainFile:     []string{"../testdata/multilevelpki/ecc-int-cert-chain.pem"},
			signingCertFile:   "../testdata/multilevelpki/ecc-int-cert.pem",
			signingKeyFile:    "../testdata/multilevelpki/ecc-int-key.pem",
			certLifetime:      24 * time.Hour,
			checkCertLifetime: true,
			expectedError:     "",
		},
		"Error lifetime check": {
			rootCertFile:      "../testdata/multilevelpki/ecc-root-cert.pem",
			certChainFile:     []string{"../testdata/multilevelpki/ecc-int-cert-chain.pem"},
			signingCertFile:   "../testdata/multilevelpki/ecc-int-cert.pem",
			signingKeyFile:    "../testdata/multilevelpki/ecc-int-key.pem",
			certLifetime:      25 * time.Hour,
			checkCertLifetime: true,
			expectedError:     "requested TTL 25h0m0s is greater than the max allowed TTL 24h0m0s",
		},
	}
	defaultWorkloadCertTTL := 30 * time.Minute
	maxWorkloadCertTTL := 24 * time.Hour
	rsaKeySize := 2048

	for id, tc := range cases {
		caopts, err := NewPluggedCertIstioCAOptions(SigningCAFileBundle{tc.rootCertFile, tc.certChainFile, tc.signingCertFile, tc.signingKeyFile},
			defaultWorkloadCertTTL, maxWorkloadCertTTL, rsaKeySize)
		if err != nil {
			t.Fatalf("%s: failed to create a plugged-cert CA Options: %v", id, err)
		}

		ca, err := NewIstioCA(caopts)
		if err != nil {
			t.Fatalf("%s: got error while creating plugged-cert CA: %v", id, err)
		}
		if ca == nil {
			t.Fatalf("failed to create a plugged-cert CA.")
		}

		certPEM, privPEM, err := ca.GenKeyCert([]string{"host1", "host2"}, tc.certLifetime, tc.checkCertLifetime)
		if err != nil {
			if tc.expectedError == "" {
				t.Fatalf("[%s] Unexpected error: %v", id, err)
			}
			if err.Error() != tc.expectedError {
				t.Fatalf("[%s] Error returned does not match expectation: %v VS (expected) %v", id, err, tc.expectedError)
			}
			continue
		} else if tc.expectedError != "" {
			t.Fatalf("[%s] GenKeyCert succeeded but expected error: %v", id, tc.expectedError)
		}

		cert, err := tls.X509KeyPair(certPEM, privPEM)
		if err != nil {
			t.Fatalf("[%s] X509KeyPair error: %v", id, err)
		}

		if len(cert.Certificate) != 3 {
			t.Fatalf("[%s] unexpected number of certificates returned: %d (expected 3)", id, len(cert.Certificate))
		}
	}
}

func createCA(maxTTL time.Duration, ecSigAlg util.SupportedECSignatureAlgorithms) (*IstioCA, error) {
	// Generate root CA key and cert.
	rootCAOpts := util.CertOptions{
		IsCA:         true,
		IsSelfSigned: true,
		TTL:          time.Hour,
		Org:          "Root CA",
		RSAKeySize:   2048,
		ECSigAlg:     ecSigAlg,
	}

	rootCertBytes, rootKeyBytes, err := util.GenCertKeyFromOptions(rootCAOpts)
	if err != nil {
		return nil, err
	}

	rootCert, err := util.ParsePemEncodedCertificate(rootCertBytes)
	if err != nil {
		return nil, err
	}

	rootKey, err := util.ParsePemEncodedKey(rootKeyBytes)
	if err != nil {
		return nil, err
	}

	intermediateCAOpts := util.CertOptions{
		IsCA:         true,
		IsSelfSigned: false,
		TTL:          time.Hour,
		Org:          "Intermediate CA",
		RSAKeySize:   2048,
		SignerCert:   rootCert,
		SignerPriv:   rootKey,
		ECSigAlg:     ecSigAlg,
	}

	intermediateCert, intermediateKey, err := util.GenCertKeyFromOptions(intermediateCAOpts)
	if err != nil {
		return nil, err
	}

	bundle, err := util.NewVerifiedKeyCertBundleFromPem(
		intermediateCert, intermediateKey, intermediateCert, rootCertBytes)
	if err != nil {
		return nil, err
	}
	// Disable root cert rotator by setting root cert check interval to 0ns.
	rootCertCheckInverval := time.Duration(0)
	caOpts := &IstioCAOptions{
		DefaultCertTTL: time.Hour,
		MaxCertTTL:     maxTTL,
		KeyCertBundle:  bundle,
		RotatorConfig: &SelfSignedCARootCertRotatorConfig{
			CheckInterval: rootCertCheckInverval,
		},
	}

	return NewIstioCA(caOpts)
}

func comparePem(expectedBytes []byte, file string) bool {
	fileBytes, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	if !bytes.Equal(fileBytes, expectedBytes) {
		return false
	}
	return true
}
