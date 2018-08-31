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
	"crypto/x509"
	"io/ioutil"
	"reflect"
	"testing"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/security/pkg/pki/util"
)

var (
	cert1Pem = `
-----BEGIN CERTIFICATE-----
MIIC5jCCAc6gAwIBAgIRAO1DMLWq99XL/B2kRlNpnikwDQYJKoZIhvcNAQELBQAw
HDEaMBgGA1UEChMRazhzLmNsdXN0ZXIubG9jYWwwHhcNMTcwOTIwMjMxODQwWhcN
MTgwOTIwMjMxODQwWjAcMRowGAYDVQQKExFrOHMuY2x1c3Rlci5sb2NhbDCCASIw
DQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAKYSyDbjRlYuyyYJOuZQHiG9wOsn
M4Rx/wWTJUOQthYz3uIBnR0WSMdyJ25VdpitHqDOR4hJo33DxNmknMnXhAuyVZoq
YpoSx/UdlOBYNQivy6OCRxe3LuDbJ5+wNZ4y3OoEqMQjxWPWcL6iyaYHyVEJprMm
IhjHD9yedJaX3F7pN0hosdtkfEsBkfcK5VPx99ekbAEo8DcsopG+XvNuT4nb7ww9
wd9VtGA8upmgNOCJvkLGVHwybw67LL4T7nejdUQd9T7o7CfAXGmBlkuGWHnsbeOe
QtCfHD3+6iCmRjcSUK6AfGnfcHTjbwzGjv48JPFaNbjm2hLixC0TdAdPousCAwEA
AaMjMCEwDgYDVR0PAQH/BAQDAgIEMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcN
AQELBQADggEBAHV5DdWspKxjeE4BsjnsA3oSkTBnbmUkMGFUtIgAvSlULYy3Wl4O
bAj7VfxIegZbE3tnkuky9BwVCoBD+d2zIqCZ5Xl17+ki6cttLAFWni85cg9gX8a6
2p/EMefUYxLXEdZTw80eAB56/34Xkt6g/CnB531W8vOvjTzg25qClkA7TjVIil2+
kLAXl8xEp48cvAxX4FslgAlBPagpJYbjVM0BjQbgmGLg1rjoH/jbkQJyIabX5dSq
9fdQYxkTzYnvcvgHf4WSl/awopjsI1NhNv07+qE8ie86EoYJgXPrNtlytyqSvIXQ
2ETBxlxOg3DdlBwhBz/Hg31tCLv8E8U8fqQ=
-----END CERTIFICATE-----`

	key1Pem = `
-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAQEAphLINuNGVi7LJgk65lAeIb3A6yczhHH/BZMlQ5C2FjPe4gGd
HRZIx3InblV2mK0eoM5HiEmjfcPE2aScydeEC7JVmipimhLH9R2U4Fg1CK/Lo4JH
F7cu4Nsnn7A1njLc6gSoxCPFY9ZwvqLJpgfJUQmmsyYiGMcP3J50lpfcXuk3SGix
22R8SwGR9wrlU/H316RsASjwNyyikb5e825PidvvDD3B31W0YDy6maA04Im+QsZU
fDJvDrssvhPud6N1RB31PujsJ8BcaYGWS4ZYeext455C0J8cPf7qIKZGNxJQroB8
ad9wdONvDMaO/jwk8Vo1uObaEuLELRN0B0+i6wIDAQABAoIBAHzHVelvoFR2uips
+vU7MziU0xOcE6gq4rr0kSYP39AUzx0uqzbEnJBGY/wReJdEU+PsuXBcK9v9sLT6
atd493y2VH0N5aHwBI9V15ssi0RomW/UHchi2XUXFNF12wNvIe8u6wLcAZ5+651A
wJPf+9HIl5i5SRsmzfMsl1ri5S/lgnjUQty4GYnT/Y53uaZoquX+sUhZ3pW8SkzX
ZvKvMbj6UOiXlelDgtEGOCgftjdm916OfnQDnSOJsh/0UvM/Bn3kQJEOgwzhMy2/
+TOIB04wVN7K6ZEbSaV7gkciiDyjg0XhJqfkmOUm8kLhLFgervjrBdkUSuukdGmq
TZmP1EkCgYEA194D0hslC//Qu0XtUCcJgLV4a41U/PDYIStf92FRXcqqYGBHDtzJ
1J86BuO/cjOdp+jZBjIIoECvY3n3TCacUiKvjmszMtanwz42eFPpVgSi3pZcyBF+
cLPB08dnUWxrxA46ss1g6gjPXjUXuEFkxuogrPiNwQPuwZnjrPWa580CgYEAxPLg
oXZ7BFVUxDEUjokj9HsvSToJNAIu7XAc84Z00yJ8z/B/muCZtpC5CZ2ZhejwBioR
AbpPEVRXFs9M2W1jW2YgO8iVcXiLT+qmNnjqGZuZnhzkMC2q9RnHrRfYMUO5bVOX
bw0UqnEMo7vTLEN47FnImr6Jv9cQFXztJEVZjZcCgYAtQPrWEiC7Gj7885Tjh7uD
QwfirDdT632zvm8Y4kr3eaQsHiLnZ7vcGiFFDnu1CkMTz0mn9dc/GTBrj0cbrMB6
q5DYL3sFPmDfGmy63wR8pu4p8aWzv48dO2H37sanGC6jZERD9bBKf9xRKJo3Y2Yo
GS8Oc/DrtNJZvdQwDzERRQKBgGFd8c/hU1ABH7cezJrrEet8OxRorMQZkDmyg52h
i4AWPL5Ql8Vp5JRtWA147L1XO9LQWTgRc6WNnMCaG9QiUEyPYMAtmjRO9BC+YQ3t
GU8vrfKNNgLbkPk7lYvtjeRNJw71lJhCT0U0Pptz8CKh+NZgTNyz9kXxfPIioNqd
rnhhAoGANfiSkuFuw2+WpBvTNah+wcZDNiMbvkQVhUwRvqIM6sLhRJhVZzJkTrYu
YQTFeoqvepyHWE9e1Mb5dGFHMvXywZQR0hR2rpWxA2OgNaRhqL7Rh7th+V/owIi9
7lGXdUBnyY8tcLhla+Rbo7Y8yOsN6pp4grT1DP+8rG4G4vnJgbk=
-----END RSA PRIVATE KEY-----`
)

// TODO (myidpt): Test Istio CA can load plugin key/certs from secret.

func TestCreateSelfSignedIstioCAWithoutSecret(t *testing.T) {
	caCertTTL := time.Hour
	defaultCertTTL := 30 * time.Minute
	maxCertTTL := time.Hour
	org := "test.ca.org"
	dualUse := false
	caNamespace := "default"
	client := fake.NewSimpleClientset()

	caopts, err := NewSelfSignedIstioCAOptions(caCertTTL, defaultCertTTL, maxCertTTL,
		org, dualUse, caNamespace, client.CoreV1())
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
	caSecret, err := client.CoreV1().Secrets("default").Get(cASecret, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get secret (error: %s)", err)
	}

	signingCertFromSecret, err := util.ParsePemEncodedCertificate(caSecret.Data[cACertID])
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
	signingCertPem := cert1Pem
	signingKeyPem := key1Pem

	client := fake.NewSimpleClientset()
	initSecret := createSecret("default", signingCertPem, signingKeyPem, rootCertPem)
	_, err := client.CoreV1().Secrets("default").Create(initSecret)
	if err != nil {
		t.Errorf("Failed to create secret (error: %s)", err)
	}

	caCertTTL := time.Hour
	certTTL := 30 * time.Minute
	maxCertTTL := time.Hour
	org := "test.ca.org"
	caNamespace := "default"
	dualUse := false

	caopts, err := NewSelfSignedIstioCAOptions(caCertTTL, certTTL, maxCertTTL,
		org, dualUse, caNamespace, client.CoreV1())
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

	signingCert, err := util.ParsePemEncodedCertificate([]byte(signingCertPem))
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

	defaultWorkloadCertTTL := 30 * time.Minute
	maxWorkloadCertTTL := time.Hour

	caopts, err := NewPluggedCertIstioCAOptions(certChainFile, signingCertFile, signingKeyFile, rootCertFile,
		defaultWorkloadCertTTL, maxWorkloadCertTTL)
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
}

func TestSignCSRForWorkload(t *testing.T) {
	host := "spiffe://example.com/ns/foo/sa/bar"
	opts := util.CertOptions{
		Host:       host,
		Org:        "istio.io",
		RSAKeySize: 2048,
		IsCA:       false,
	}
	csrPEM, keyPEM, err := util.GenCSR(opts)
	if err != nil {
		t.Error(err)
	}

	ca, err := createCA(time.Hour, false)
	if err != nil {
		t.Error(err)
	}

	requestedTTL := 30 * time.Minute
	certPEM, signErr := ca.Sign(csrPEM, requestedTTL, false)
	if signErr != nil {
		t.Error(err)
	}

	fields := &util.VerifyFields{
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		IsCA:        false,
		Host:        host,
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
	expected, err := util.BuildSubjectAltNameExtension(host)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(expected, san) {
		t.Errorf("Unexpected extensions: wanted %v but got %v", expected, san)
	}
}

func TestSignCSRForCA(t *testing.T) {
	host := "spiffe://example.com/ns/foo/sa/baz"
	opts := util.CertOptions{
		Host:       host,
		Org:        "istio.io",
		RSAKeySize: 2048,
		IsCA:       true,
	}
	csrPEM, keyPEM, err := util.GenCSR(opts)
	if err != nil {
		t.Error(err)
	}

	ca, err := createCA(365*24*time.Hour, true)
	if err != nil {
		t.Error(err)
	}

	requestedTTL := 30 * 24 * time.Hour
	certPEM, signErr := ca.Sign(csrPEM, requestedTTL, true)
	if signErr != nil {
		t.Error(err)
	}

	fields := &util.VerifyFields{
		KeyUsage: x509.KeyUsageCertSign,
		IsCA:     true,
		Host:     host,
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
	expected, err := util.BuildSubjectAltNameExtension(host)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(expected, san) {
		t.Errorf("Unexpected extensions: wanted %v but got %v", expected, san)
	}
}

func TestSignCSRTTLError(t *testing.T) {
	host := "spiffe://example.com/ns/foo/sa/bar"
	opts := util.CertOptions{
		Host:       host,
		Org:        "istio.io",
		RSAKeySize: 2048,
	}
	csrPEM, _, err := util.GenCSR(opts)
	if err != nil {
		t.Error(err)
	}

	ca, err := createCA(2*time.Hour, false)
	if err != nil {
		t.Error(err)
	}

	ttl := 3 * time.Hour

	cert, signErr := ca.Sign(csrPEM, ttl, false)
	if cert != nil {
		t.Errorf("Expected null cert be obtained a non-null cert.")
	}
	expectedErr := "requested TTL 3h0m0s is greater than the max allowed TTL 2h0m0s"
	if signErr.(*Error).Error() != expectedErr {
		t.Errorf("Expected error: %s but got error: %s.", signErr.(*Error).Error(), expectedErr)
	}
}

func createCA(maxTTL time.Duration, multicluster bool) (*IstioCA, error) {
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

// TODO(wattli): move the two functions below as a util function to share with secret_test.go
func createSecret(namespace, signingCert, signingKey, rootCert string) *v1.Secret {
	return &v1.Secret{
		Data: map[string][]byte{
			cACertID:       []byte(signingCert),
			cAPrivateKeyID: []byte(signingKey),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cASecret,
			Namespace: namespace,
		},
		Type: istioCASecretType,
	}
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
