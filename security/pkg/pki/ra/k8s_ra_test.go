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

package ra

import (
	"os"
	"path"
	"testing"
	"time"

	cert "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/security/pkg/k8s/chiron"
	"istio.io/istio/security/pkg/pki/ca"
	pkiutil "istio.io/istio/security/pkg/pki/util"
)

const (
	TestCertificatePEM = `-----BEGIN CERTIFICATE-----
MIIDGDCCAgCgAwIBAgIRAKvYcPLFqnJcwtshCGfNzTswDQYJKoZIhvcNAQELBQAw
LzEtMCsGA1UEAxMkYTc1OWM3MmQtZTY3Mi00MDM2LWFjM2MtZGMwMTAwZjE1ZDVl
MB4XDTE5MDgwNjE5NTU0NVoXDTI0MDgwNDE5NTU0NVowCzEJMAcGA1UEChMAMIIB
IjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAyLIFJU5yJ5VXhbmizir+7Glm
1tVEYXKGiqYbMRbfsFm7V6Z4l00D9/eHvfTXaFpqhv6HBm31MArjYB3OaaV6krvT
whBUEPSkGBFe/eMPSFWBW27a0nw0cK2s/5yuFhTRtcUrZ9+ojJg4IS3oSm2UZ6UJ
DuNI3qwB6OlPQOcWX8uEp4eAaolD1lIbLRQYvxYrBqnyCZBLE+MJgA1/VB3dAECB
TxPtAqcwLFcvsM5ABys8yK8FrqRn5Bx54NiztgG+yU30W33xjdqzmEmuIIk4JjPU
ZQRsug7XClDvQKM6lbYcYS1td2zT08hdgURFXJ9VR64ALFp00/bvglpryu8FmQID
AQABo1MwUTAMBgNVHRMBAf8EAjAAMEEGA1UdEQQ6MDiCHHByb3RvbXV0YXRlLmlz
dGlvLXN5c3RlbS5zdmOCGHByb3RvbXV0YXRlLmlzdGlvLXN5c3RlbTANBgkqhkiG
9w0BAQsFAAOCAQEAhcVEZSuNMqMUJrWVb3b+6pmw9o1f7j6a51KWxOiIl6YuTYFS
WaR0lHSW8wLesjsjm1awWO/F3QRuYWbalANy7434GMAGF53u/uc+Z8aE3EItER9o
SpAJos6OfJqyok7JXDdOYRDD5/hBerj68R9llWzNJd27/1jZ0NF2sIE1W4QFddy/
+8YA4+IqwkWB5/LbeRznl3EjFZDpCEJk0gg5XwAR5eIEy4QU8GueTwrDkssFdBGq
0naco7/Es7CWQscYdKHAgYgk0UAyu8sGV235Uw3hlOrbZ/kqvyUmsSujgT8irmDV
e+5z6MTAO6ktvHdQlSuH6ARn47bJrZOlkttAhg==
-----END CERTIFICATE-----
`
)

var (
	testCsrHostName string = spiffe.Identity{TrustDomain: "cluster.local", Namespace: "default", ServiceAccount: "bookinfo-productpage"}.String()
	TestCACertFile  string = "../testdata/example-ca-cert.pem"
)

func defaultReactionFunc(obj runtime.Object) kt.ReactionFunc {
	return func(act kt.Action) (bool, runtime.Object, error) {
		return true, obj, nil
	}
}

func createFakeCsr(t *testing.T) []byte {
	options := pkiutil.CertOptions{
		Host:       testCsrHostName,
		RSAKeySize: 2048,
		PKCS8Key:   false,
		ECSigAlg:   pkiutil.SupportedECSignatureAlgorithms("ECDSA"),
	}
	csrPEM, _, err := pkiutil.GenCSR(options)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
		return nil
	}
	return csrPEM
}

func initFakeKubeClient(csrName, certificate string) *fake.Clientset {
	client := fake.NewSimpleClientset()
	csr := &cert.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: csrName,
		},
		Status: cert.CertificateSigningRequestStatus{
			Certificate: []byte(certificate),
		},
	}
	client.PrependReactor("get", "certificatesigningrequests", defaultReactionFunc(csr))
	return client
}

func createFakeK8sRA(client *fake.Clientset, caCertFile string) (*KubernetesRA, error) {
	defaultCertTTL := 30 * time.Minute
	maxCertTTL := time.Hour
	caSigner := "kubernates.io/kube-apiserver-client"
	raOpts := &IstioRAOptions{
		ExternalCAType: ExtCAK8s,
		DefaultCertTTL: defaultCertTTL,
		MaxCertTTL:     maxCertTTL,
		CaSigner:       caSigner,
		CaCertFile:     caCertFile,
		VerifyAppendCA: true,
		K8sClient:      client,
	}
	return NewKubernetesRA(raOpts)
}

func TestK8sSignWithMeshConfig(t *testing.T) {
	csrPEM := createFakeCsr(t)
	csrName := chiron.GenCsrName()
	rootCertPem, err := os.ReadFile(path.Join(env.IstioSrc, "samples/certs", "root-cert.pem"))
	if err != nil {
		t.Errorf("Failed to read sample root-cert.pem")
	}
	certChainPem, err := os.ReadFile(path.Join(env.IstioSrc, "samples/certs", "cert-chain.pem"))
	if err != nil {
		t.Errorf("Failed to read sample cert-chain.pem")
	}
	client := initFakeKubeClient(csrName, string(certChainPem))
	ra, err := createFakeK8sRA(client, "")
	if err != nil {
		t.Errorf("Failed to create Fake K8s RA")
	}
	signer := "kubernates.io/kube-apiserver-client"
	ra.certSignerDomain = "kubernates.io"
	caCertificates := []*meshconfig.MeshConfig_CertificateData{
		{CertificateData: &meshconfig.MeshConfig_CertificateData_Pem{Pem: string(rootCertPem)}, CertSigners: []string{signer}},
	}
	ra.SetCACertificatesFromMeshConfig(caCertificates)
	subjectID := spiffe.Identity{TrustDomain: "cluster.local", Namespace: "default", ServiceAccount: "bookinfo-productpage"}.String()
	certOptions := ca.CertOpts{
		SubjectIDs: []string{subjectID},
		TTL:        60 * time.Second, ForCA: false,
		CertSigner: "kube-apiserver-client",
	}
	// expect to sign back successfully
	_, err = ra.SignWithCertChain(csrPEM, certOptions)
	if err != nil {
		t.Errorf("K8s CA Signing CSR With Root Cert In Meshconfig failed")
	}
	testCACert, err := os.ReadFile(TestCACertFile)
	if err != nil {
		t.Errorf("Failed to read test CA Cert file")
	}
	updatedCACertificates := []*meshconfig.MeshConfig_CertificateData{
		{CertificateData: &meshconfig.MeshConfig_CertificateData_Pem{Pem: string(testCACert)}, CertSigners: []string{signer}},
	}
	ra.SetCACertificatesFromMeshConfig(updatedCACertificates)
	// expect failure in sign since root cert in mesh config does not match
	_, err = ra.SignWithCertChain(csrPEM, certOptions)
	if err == nil {
		t.Errorf("K8s CA Signing CSR With Root Cert In Meshconfig failed")
	}
}

// TestK8sSign : Verify that ra.k8sSign returns a valid certPEM while using k8s Fake Client to create a CSR
func TestK8sSign(t *testing.T) {
	csrPEM := createFakeCsr(t)
	csrName := chiron.GenCsrName()
	client := initFakeKubeClient(csrName, TestCertificatePEM)
	r, err := createFakeK8sRA(client, TestCACertFile)
	if err != nil {
		t.Errorf("Validation CSR failed")
	}
	subjectID := spiffe.Identity{TrustDomain: "cluster.local", Namespace: "default", ServiceAccount: "bookinfo-productpage"}.String()
	_, err = r.Sign(csrPEM, ca.CertOpts{
		SubjectIDs: []string{subjectID},
		TTL:        60 * time.Second, ForCA: false,
	})
	if err != nil {
		t.Errorf("K8s CA Signing CSR failed")
	}
}

func TestValidateCSR(t *testing.T) {
	csrPEM := createFakeCsr(t)
	csrName := chiron.GenCsrName()
	client := initFakeKubeClient(csrName, TestCertificatePEM)
	_, err := createFakeK8sRA(client, TestCACertFile)
	if err != nil {
		t.Errorf("Validation CSR failed")
	}
	var testSubjectIDs []string

	// Test Case 1
	testSubjectIDs = []string{testCsrHostName, "Random-Host-Name"}
	if !ValidateCSR(csrPEM, testSubjectIDs) {
		t.Errorf("Test 1: CSR Validation failed")
	}

	// Test Case 2
	testSubjectIDs = []string{"Random-Host-Name"}
	if ValidateCSR(csrPEM, testSubjectIDs) {
		t.Errorf("Test 2: CSR Validation failed")
	}
}
