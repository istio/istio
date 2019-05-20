// Copyright 2017 Istio Authors
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
	"crypto/x509"
	"encoding/base64"
	"io/ioutil"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/security/pkg/k8s/configmap"
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
	org := "test.ca.org"
	const caNamespace = "default"
	client := fake.NewSimpleClientset()
	rootCertFile := ""

	caopts, err := NewSelfSignedIstioCAOptions(context.Background(), caCertTTL, defaultCertTTL, maxCertTTL,
		org, false, caNamespace, -1, client.CoreV1(), rootCertFile)
	if err != nil {
		t.Fatalf("Failed to create a self-signed CA Options: %v", err)
	}

	ca, err := NewIstioCA(caopts)
	if err != nil {
		t.Errorf("Got error while createing self-signed CA: %v", err)
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
	caSecret, err := client.CoreV1().Secrets("default").Get(CASecret, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get secret (error: %s)", err)
	}

	signingCertFromSecret, err := util.ParsePemEncodedCertificate(caSecret.Data[caCertID])
	if err != nil {
		t.Errorf("Failed to parse cert (error: %s)", err)
	}

	if !signingCertFromSecret.Equal(signingCert) {
		t.Error("CA signing cert does not match the K8s secret")
	}

	// Check the siging cert stored in K8s configmap.
	cmc := configmap.NewController(caNamespace, client.CoreV1())
	strCertFromConfigMap, err := cmc.GetCATLSRootCert()
	if err != nil {
		t.Errorf("Cannot get the CA cert from configmap (%v)", err)
	}
	_, _, _, cert := ca.GetCAKeyCertBundle().GetAllPem()
	certFromConfigMap, err := base64.StdEncoding.DecodeString(strCertFromConfigMap)
	if err != nil {
		t.Errorf("Cannot decode the CA cert from configmap (%v)", err)
	}
	if !bytes.Equal(cert, certFromConfigMap) {
		t.Errorf("The cert in configmap is not equal to the CA signing cert: %v VS (expected) %v", certFromConfigMap, cert)
	}
}

func TestCreateSelfSignedIstioCAWithSecret(t *testing.T) {
	rootCertPem := cert1Pem
	// Use the same signing cert and root cert for self-signed CA.
	signingCertPem := []byte(cert1Pem)
	signingKeyPem := []byte(key1Pem)

	client := fake.NewSimpleClientset()
	initSecret := BuildSecret("", CASecret, "default", nil, nil, nil, signingCertPem, signingKeyPem, istioCASecretType)
	_, err := client.CoreV1().Secrets("default").Create(initSecret)
	if err != nil {
		t.Errorf("Failed to create secret (error: %s)", err)
	}

	caCertTTL := time.Hour
	certTTL := 30 * time.Minute
	maxCertTTL := time.Hour
	org := "test.ca.org"
	caNamespace := "default"
	const rootCertFile = ""

	caopts, err := NewSelfSignedIstioCAOptions(context.Background(), caCertTTL, certTTL, maxCertTTL,
		org, false, caNamespace, -1, client.CoreV1(), rootCertFile)
	if err != nil {
		t.Fatalf("Failed to create a self-signed CA Options: %v", err)
	}

	ca, err := NewIstioCA(caopts)
	if err != nil {
		t.Errorf("Got error while createing self-signed CA: %v", err)
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

	// Check the siging cert stored in K8s configmap.
	cmc := configmap.NewController(caNamespace, client.CoreV1())
	strCertFromConfigMap, err := cmc.GetCATLSRootCert()
	if err != nil {
		t.Errorf("Cannot get the CA cert from configmap (%v)", err)
	}
	_, _, _, cert := ca.GetCAKeyCertBundle().GetAllPem()
	certFromConfigMap, err := base64.StdEncoding.DecodeString(strCertFromConfigMap)
	if err != nil {
		t.Errorf("Cannot decode the CA cert from configmap (%v)", err)
	}
	if !bytes.Equal(cert, certFromConfigMap) {
		t.Errorf("The cert in configmap is not equal to the CA signing cert: %v VS (expected) %v", certFromConfigMap, cert)
	}
}

func TestCreateSelfSignedIstioCAReadSigningCertOnly(t *testing.T) {
	rootCertPem := cert1Pem
	// Use the same signing cert and root cert for self-signed CA.
	signingCertPem := []byte(cert1Pem)
	signingKeyPem := []byte(key1Pem)

	caCertTTL := time.Hour
	certTTL := 30 * time.Minute
	maxCertTTL := time.Hour
	org := "test.ca.org"
	caNamespace := "default"
	const rootCertFile = ""

	client := fake.NewSimpleClientset()

	// Should abort with timeout.
	expectedErr := "secret waiting thread is terminated"
	ctx0, cancel0 := context.WithTimeout(context.Background(), time.Millisecond*50)
	defer cancel0()
	_, err := NewSelfSignedIstioCAOptions(ctx0, caCertTTL, certTTL, maxCertTTL,
		org, false, caNamespace, time.Millisecond*10, client.CoreV1(), rootCertFile)
	if err == nil {
		t.Errorf("Expected error, but succeeded.")
	} else if err.Error() != expectedErr {
		t.Errorf("Unexpected error message: %s VS (expected) %s", err.Error(), expectedErr)
		return
	}

	// Should succeed once secret is ready.
	secret := BuildSecret("", CASecret, "default", nil, nil, nil, signingCertPem, signingKeyPem, istioCASecretType)
	_, err = client.CoreV1().Secrets("default").Create(secret)
	if err != nil {
		t.Errorf("Failed to create secret (error: %s)", err)
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	caopts, err := NewSelfSignedIstioCAOptions(ctx1, caCertTTL, certTTL, maxCertTTL,
		org, false, caNamespace, time.Millisecond*10, client.CoreV1(), rootCertFile)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	ca, err := NewIstioCA(caopts)
	if err != nil {
		t.Errorf("Got error while createing self-signed CA: %v", err)
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
	certChainFile := "../testdata/multilevelpki/int2-cert-chain.pem"
	signingCertFile := "../testdata/multilevelpki/int2-cert.pem"
	signingKeyFile := "../testdata/multilevelpki/int2-key.pem"
	caNamespace := "default"

	defaultWorkloadCertTTL := 30 * time.Minute
	maxWorkloadCertTTL := time.Hour

	client := fake.NewSimpleClientset()

	caopts, err := NewPluggedCertIstioCAOptions(certChainFile, signingCertFile, signingKeyFile, rootCertFile,
		defaultWorkloadCertTTL, maxWorkloadCertTTL, caNamespace, client.CoreV1())
	if err != nil {
		t.Fatalf("Failed to create a plugged-cert CA Options: %v", err)
	}

	ca, err := NewIstioCA(caopts)
	if err != nil {
		t.Errorf("Got error while createing plugged-cert CA: %v", err)
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
	if !comparePem(certChainBytes, certChainFile) {
		t.Errorf("Failed to verify loading of cert chain pem.")
	}
	if !comparePem(rootCertBytes, rootCertFile) {
		t.Errorf("Failed to verify loading of root cert pem.")
	}

	// Check the siging cert stored in K8s configmap.
	cmc := configmap.NewController(caNamespace, client.CoreV1())
	strCertFromConfigMap, err := cmc.GetCATLSRootCert()
	if err != nil {
		t.Errorf("Cannot get the CA cert from configmap (%v)", err)
	}
	_, _, cert, _ := ca.GetCAKeyCertBundle().GetAllPem()
	certFromConfigMap, err := base64.StdEncoding.DecodeString(strCertFromConfigMap)
	if err != nil {
		t.Errorf("Cannot decode the CA cert from configmap (%v)", err)
	}
	if !bytes.Equal(cert, certFromConfigMap) {
		t.Errorf("The cert in configmap is not equal to the CA signing cert: %v VS (expected) %v", certFromConfigMap, cert)
	}
}

// TODO: merge tests for SignCSR.
func TestSignCSRForWorkload(t *testing.T) {
	subjectID := "spiffe://example.com/ns/foo/sa/bar"
	opts := util.CertOptions{
		// This value is not used, instead, subjectID should be used in certificate.
		Host:       "spiffe://different.com/test",
		RSAKeySize: 2048,
		IsCA:       false,
	}
	csrPEM, keyPEM, err := util.GenCSR(opts)
	if err != nil {
		t.Error(err)
	}

	ca, err := createCA(time.Hour)
	if err != nil {
		t.Error(err)
	}

	requestedTTL := 30 * time.Minute
	certPEM, signErr := ca.Sign(csrPEM, []string{subjectID}, requestedTTL, false)
	if signErr != nil {
		t.Error(err)
	}

	fields := &util.VerifyFields{
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		IsCA:        false,
		Host:        subjectID,
	}
	_, _, certChainBytes, rootCertBytes := ca.GetCAKeyCertBundle().GetAll()
	if err = util.VerifyCertificate(
		keyPEM, append(certPEM, certChainBytes...), rootCertBytes, fields); err != nil {
		t.Error(err)
	}

	cert, err := util.ParsePemEncodedCertificate(certPEM)
	if err != nil {
		t.Error(err)
	}

	if ttl := cert.NotAfter.Sub(cert.NotBefore); ttl != requestedTTL {
		t.Errorf("Unexpected certificate TTL (expecting %v, actual %v)", requestedTTL, ttl)
	}
	san := util.ExtractSANExtension(cert.Extensions)
	if san == nil {
		t.Errorf("No SAN extension is found in the certificate")
	}
	expected, err := util.BuildSubjectAltNameExtension(subjectID)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(expected, san) {
		t.Errorf("Unexpected extensions: wanted %v but got %v", expected, san)
	}
}

func TestSignCSRForCA(t *testing.T) {
	subjectID := "spiffe://example.com/ns/foo/sa/baz"
	opts := util.CertOptions{
		RSAKeySize: 2048,
		IsCA:       true,
	}
	csrPEM, keyPEM, err := util.GenCSR(opts)
	if err != nil {
		t.Error(err)
	}

	ca, err := createCA(365 * 24 * time.Hour)
	if err != nil {
		t.Error(err)
	}

	requestedTTL := 30 * 24 * time.Hour
	certPEM, signErr := ca.Sign(csrPEM, []string{subjectID}, requestedTTL, true)
	if signErr != nil {
		t.Error(err)
	}

	fields := &util.VerifyFields{
		KeyUsage: x509.KeyUsageCertSign,
		IsCA:     true,
		Host:     subjectID,
	}
	_, _, certChainBytes, rootCertBytes := ca.GetCAKeyCertBundle().GetAll()
	if err = util.VerifyCertificate(
		keyPEM, append(certPEM, certChainBytes...), rootCertBytes, fields); err != nil {
		t.Error(err)
	}

	cert, err := util.ParsePemEncodedCertificate(certPEM)
	if err != nil {
		t.Error(err)
	}

	if ttl := cert.NotAfter.Sub(cert.NotBefore); ttl != requestedTTL {
		t.Errorf("Unexpected certificate TTL (expecting %v, actual %v)", requestedTTL, ttl)
	}
	san := util.ExtractSANExtension(cert.Extensions)
	if san == nil {
		t.Errorf("No SAN extension is found in the certificate")
	}
	expected, err := util.BuildSubjectAltNameExtension(subjectID)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(expected, san) {
		t.Errorf("Unexpected extensions: wanted %v but got %v", expected, san)
	}
}

func TestSignCSRTTLError(t *testing.T) {
	subjectID := "spiffe://example.com/ns/foo/sa/bar"
	opts := util.CertOptions{
		Org:        "istio.io",
		RSAKeySize: 2048,
	}
	csrPEM, _, err := util.GenCSR(opts)
	if err != nil {
		t.Error(err)
	}

	ca, err := createCA(2 * time.Hour)
	if err != nil {
		t.Error(err)
	}

	ttl := 3 * time.Hour

	cert, signErr := ca.Sign(csrPEM, []string{subjectID}, ttl, false)
	if cert != nil {
		t.Errorf("Expected null cert be obtained a non-null cert.")
	}
	expectedErr := "requested TTL 3h0m0s is greater than the max allowed TTL 2h0m0s"
	if signErr.(*Error).Error() != expectedErr {
		t.Errorf("Expected error: %s but got error: %s.", signErr.(*Error).Error(), expectedErr)
	}
}

func TestAppendRootCerts(t *testing.T) {
	root1 := "root-cert-1"
	expRootCerts := `root-cert-1
root-cert-2
root-cert-3`
	rootCerts, err := appendRootCerts([]byte(root1), "./root-certs-for-testing.pem")
	if err != nil {
		t.Errorf("appendRootCerts() returns an error: %v", err)
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
	rootCerts, err := appendRootCerts(root1, "./root-certs-for-testing.pem")
	if err != nil {
		t.Errorf("appendRootCerts() returns an error: %v", err)
	} else if expRootCerts != string(rootCerts) {
		t.Errorf("the root certificates do not match. Expect:%v. Actual:%v.",
			expRootCerts, string(rootCerts))
	}
}

func createCA(maxTTL time.Duration) (*IstioCA, error) {
	// Generate root CA key and cert.
	rootCAOpts := util.CertOptions{
		IsCA:         true,
		IsSelfSigned: true,
		TTL:          time.Hour,
		Org:          "Root CA",
		RSAKeySize:   2048,
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
	caOpts := &IstioCAOptions{
		CertTTL:       time.Hour,
		MaxCertTTL:    maxTTL,
		KeyCertBundle: bundle,
	}

	return NewIstioCA(caOpts)
}

func comparePem(expectedBytes []byte, file string) bool {
	fileBytes, err := ioutil.ReadFile(file)
	if err != nil {
		return false
	}
	if !bytes.Equal(fileBytes, expectedBytes) {
		return false
	}
	return true
}
