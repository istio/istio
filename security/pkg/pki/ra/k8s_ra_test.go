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

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
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

	// Steps to recreate CSR for testCSRWithCATrue
	//
	// cat > client.conf <<EOF
	// [req]
	// req_extensions = v3_req
	// distinguished_name = req_distinguished_name
	// [req_distinguished_name]
	// [ v3_req ]
	// basicConstraints = CA:TRUE
	// keyUsage = nonRepudiation, digitalSignature, keyEncipherment
	// extendedKeyUsage = clientAuth, serverAuth
	// subjectAltName = @alt_names
	// [alt_names]
	// URI = spiffe://cluster.local/ns/default/sa/bookinfo-productpage
	// EOF
	//
	// openssl ecparam -out ./key.pem -name prime256v1 -genkey
	//
	// openssl req -new -sha256 -key key.pem -out client.csr -subj /CN="" -config client.conf
	testCSRWithCATrue = `-----BEGIN CERTIFICATE REQUEST-----
MIIBUDCB9wIBADAAMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAElNlH1xq0iqhW
LjCEVV6W58UKS66yZgmcm81SDWxB2LH+eZ+61Udq6EElHd51C9XmPBoAln7TjDgO
rP4Lsxl48qCBlDCBkQYJKoZIhvcNAQkOMYGDMIGAMAwGA1UdEwQFMAMBAf8wCwYD
VR0PBAQDAgXgMB0GA1UdJQQWMBQGCCsGAQUFBwMCBggrBgEFBQcDATBEBgNVHREE
PTA7hjlzcGlmZmU6Ly9jbHVzdGVyLmxvY2FsL25zL2RlZmF1bHQvc2EvYm9va2lu
Zm8tcHJvZHVjdHBhZ2UwCgYIKoZIzj0EAwIDSAAwRQIgP8PYpt0pUOCGtz5PopBt
ZkifGDtZspkygoghA/A8hw0CIQDJyJhcijKfHN1fri9VFKarjKYDpQXD9aiGtrMf
PK0qAQ==
-----END CERTIFICATE REQUEST-----
`

	// Steps to recretae CSR for testCSRWithInvalidCN
	// cat > client-test3.conf <<EOF
	// [req]
	// req_extensions = v3_req
	// distinguished_name = req_distinguished_name
	// [req_distinguished_name]
	// [ v3_req ]
	// keyUsage = nonRepudiation, digitalSignature, keyEncipherment
	// extendedKeyUsage = clientAuth, serverAuth
	// subjectAltName = @alt_names
	// [alt_names]
	// URI = spiffe://cluster.local/ns/default/sa/bookinfo-productpage
	// EOF

	// openssl ecparam -out ./key.pem -name prime256v1 -genkey

	// openssl req -new -sha256 -key key.pem -out client-test3.csr -subj "/CN=test.test.svc.cluster.local" -config client-test3.conf
	testCSRWithInvalidCN = `-----BEGIN CERTIFICATE REQUEST-----
MIIBTzCB9gIBADAPMQ0wCwYDVQQDDAR0ZXN0MFkwEwYHKoZIzj0CAQYIKoZIzj0D
AQcDQgAEilbgGZrCywnsO9VCji1E2+9vNg5c4qSxSHeOx6v47V04k72BlemfSWYR
1dl//qZTbTVBuIRGl2C8zD3kQ+2s1KCBhDCBgQYJKoZIhvcNAQkOMXQwcjALBgNV
HQ8EBAMCBeAwHQYDVR0lBBYwFAYIKwYBBQUHAwIGCCsGAQUFBwMBMEQGA1UdEQQ9
MDuGOXNwaWZmZTovL2NsdXN0ZXIubG9jYWwvbnMvZGVmYXVsdC9zYS9ib29raW5m
by1wcm9kdWN0cGFnZTAKBggqhkjOPQQDAgNIADBFAiB0LzGRZ1xg2QxMlNlb18uB
ldHWJYmbT0gJp6ZncK/OUgIhALG0Wzg1UqGGJ3qfrqNw/QnAcp22eQO6Bc+r8plk
wQBl
-----END CERTIFICATE REQUEST-----
`
)

var (
	testCsrHostName       = spiffe.Identity{TrustDomain: "cluster.local", Namespace: "default", ServiceAccount: "bookinfo-productpage"}.String()
	TestCACertFile        = "../testdata/example-ca-cert.pem"
	mismatchCertChainFile = "../testdata/cert-chain.pem"
)

func TestK8sSignWithMeshConfig(t *testing.T) {
	cases := []struct {
		name                          string
		rootCertForMeshConfig         string
		certChain                     string
		updatedRootCertForMeshConfig  string
		expectedFail                  bool
		expectedFailOnUpdatedRootCert bool
	}{
		{
			name:                  "Root cert from mesh config and cert chain does not match",
			rootCertForMeshConfig: path.Join(env.IstioSrc, "samples/certs", "root-cert.pem"),
			certChain:             mismatchCertChainFile,
			expectedFail:          true,
		},
		{
			name:                  "Root cert is specified in mesh config and Root cert from cert chain is empty(only one leaf cert)",
			rootCertForMeshConfig: path.Join(env.IstioSrc, "samples/certs", "root-cert.pem"),
			certChain:             path.Join(env.IstioSrc, "samples/certs", "leaf-workload-foo-cert.pem"),
			expectedFail:          true,
		},
		{
			name:                  "Root cert and intermediate CA are specified in mesh config and Root cert from cert chain is empty(only one leaf cert)",
			rootCertForMeshConfig: path.Join(env.IstioSrc, "samples/certs", "workload-foo-root-certs.pem"),
			certChain:             path.Join(env.IstioSrc, "samples/certs", "leaf-workload-foo-cert.pem"),
		},
		{
			name:                  "Root cert is specified in mesh config and cert chain contains only intermediate CA(only leaf cert + intermediate CA)",
			rootCertForMeshConfig: path.Join(env.IstioSrc, "samples/certs", "root-cert.pem"),
			certChain:             path.Join(env.IstioSrc, "samples/certs", "workload-foo-cert.pem"),
		},
		{
			name:                          "Root cert is specified in mesh config and be updated to an invalid value",
			rootCertForMeshConfig:         path.Join(env.IstioSrc, "samples/certs", "root-cert.pem"),
			certChain:                     path.Join(env.IstioSrc, "samples/certs", "cert-chain.pem"),
			updatedRootCertForMeshConfig:  TestCACertFile,
			expectedFailOnUpdatedRootCert: true,
		},
		{
			name:      "Root cert is not specified in mesh config and cert chain contains only intermediate CA(only leaf cert + intermediate CA)",
			certChain: path.Join(env.IstioSrc, "samples/certs", "workload-foo-cert.pem"),
		},
		{
			name:         "Root cert is not specified in mesh config and Root cert from cert chain is empty(only one leaf cert)",
			certChain:    path.Join(env.IstioSrc, "samples/certs", "leaf-workload-foo-cert.pem"),
			expectedFail: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			csrPEM := createDefaultFakeCsr(t)
			certChainPem, err := os.ReadFile(tc.certChain)
			if err != nil {
				t.Errorf("Failed to read sample %s", tc.certChain)
			}
			client := initFakeKubeClient(t, certChainPem)
			ra, err := createFakeK8sRA(client, "")
			if err != nil {
				t.Errorf("Failed to create Fake K8s RA")
			}
			signer := "kubernetes.io/kube-apiserver-client"
			ra.certSignerDomain = "kubernetes.io"
			if tc.rootCertForMeshConfig != "" {
				rootCertPem, err := os.ReadFile(tc.rootCertForMeshConfig)
				if err != nil {
					t.Errorf("Failed to read sample %s", tc.rootCertForMeshConfig)
				}
				caCertificates := []*meshconfig.MeshConfig_CertificateData{
					{CertificateData: &meshconfig.MeshConfig_CertificateData_Pem{Pem: string(rootCertPem)}, CertSigners: []string{signer}},
				}
				ra.SetCACertificatesFromMeshConfig(caCertificates)
			}
			subjectID := spiffe.Identity{TrustDomain: "cluster.local", Namespace: "default", ServiceAccount: "bookinfo-productpage"}.String()
			certOptions := ca.CertOpts{
				SubjectIDs: []string{subjectID},
				TTL:        60 * time.Second, ForCA: false,
				CertSigner: "kube-apiserver-client",
			}
			_, err = ra.SignWithCertChain(csrPEM, certOptions)
			if (tc.expectedFail && err == nil) || (!tc.expectedFail && err != nil) {
				t.Fatalf("expected failure: %t, got %v", tc.expectedFail, err)
			}
			if tc.updatedRootCertForMeshConfig != "" {
				testCACert, err := os.ReadFile(tc.updatedRootCertForMeshConfig)
				if err != nil {
					t.Errorf("Failed to read test CA Cert file")
				}
				updatedCACertificates := []*meshconfig.MeshConfig_CertificateData{
					{CertificateData: &meshconfig.MeshConfig_CertificateData_Pem{Pem: string(testCACert)}, CertSigners: []string{signer}},
				}
				ra.SetCACertificatesFromMeshConfig(updatedCACertificates)
				// expect failure in sign since root cert in mesh config does not match
				_, err = ra.SignWithCertChain(csrPEM, certOptions)
				if err == nil && !tc.expectedFailOnUpdatedRootCert {
					t.Fatalf("expected failed, got none")
				}
			}
		})
	}
}

func createDefaultFakeCsr(t *testing.T) []byte {
	return createFakeCsr(t, "")
}

func createFakeCsr(t *testing.T, org string) []byte {
	options := pkiutil.CertOptions{
		Host:       testCsrHostName,
		RSAKeySize: 2048,
		PKCS8Key:   false,
		ECSigAlg:   pkiutil.SupportedECSignatureAlgorithms("ECDSA"),
		Org:        org,
	}
	csrPEM, _, err := pkiutil.GenCSR(options)
	if err != nil {
		t.Fatalf("Error creating fake CSR: %v", err)
		return nil
	}
	return csrPEM
}

func initFakeKubeClient(t test.Failer, certificate []byte) kube.CLIClient {
	client := kube.NewFakeClient()
	client.RunAndWait(test.NewStop(t))
	ctx := test.NewContext(t)
	w, _ := client.Kube().CertificatesV1().CertificateSigningRequests().Watch(ctx, metav1.ListOptions{})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case r := <-w.ResultChan():
				csr := r.Object.(*cert.CertificateSigningRequest).DeepCopy()
				if csr.Status.Certificate != nil {
					continue
				}
				csr.Status.Certificate = certificate
				// fake clientset doesn't handle resource version, so we need to delay the update
				// to make sure watchers can catch the event
				time.Sleep(time.Millisecond)
				client.Kube().CertificatesV1().CertificateSigningRequests().UpdateStatus(ctx, csr, metav1.UpdateOptions{})
			}
		}
	}()
	return client
}

func createFakeK8sRA(client kube.Client, caCertFile string) (*KubernetesRA, error) {
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
		K8sClient:      client.Kube(),
	}
	return NewKubernetesRA(raOpts)
}

// TestK8sSign : Verify that ra.k8sSign returns a valid certPEM while using k8s Fake Client to create a CSR
func TestK8sSign(t *testing.T) {
	csrPEM := createDefaultFakeCsr(t)
	client := initFakeKubeClient(t, []byte(TestCertificatePEM))
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
	csrPEM := createDefaultFakeCsr(t)
	csrPEMWithInvalidOrg := createFakeCsr(t, "Invalid-Org")

	client := initFakeKubeClient(t, []byte(TestCertificatePEM))
	_, err := createFakeK8sRA(client, TestCACertFile)
	if err != nil {
		t.Errorf("Validation CSR failed")
	}
	var testSubjectIDs []string

	// Test Case 1
	testSubjectIDs = []string{testCsrHostName, "Random-Host-Name"}
	if !ValidateCSR(csrPEM, testSubjectIDs) {
		t.Errorf("Test 1: CSR Validation failed. Expected success")
	}

	// Test Case 2
	testSubjectIDs = []string{"Random-Host-Name"}
	if ValidateCSR(csrPEM, testSubjectIDs) {
		t.Errorf("Test 2: CSR Validation failed. CSR validation" +
			" succeeded when expected failure due to mismatch in SANs")
	}

	// Test Case 3
	testSubjectIDs = []string{testCsrHostName}
	if ValidateCSR(csrPEMWithInvalidOrg, testSubjectIDs) {
		t.Errorf("Test 3: CSR Validation failed. CSR validation" +
			" succeeded when expected failure due invalid Org")
	}

	// Independently creating CSRs for tests for fields that are not configurable
	// via GenCSR() - CN and CA
	// Test Case 4
	if ValidateCSR([]byte(testCSRWithInvalidCN), testSubjectIDs) {
		t.Errorf("Test 4: CSR Validation failed. CSR validation" +
			" succeeded when expected failure due invalid CN")
	}

	// Test Case 5
	if ValidateCSR([]byte(testCSRWithCATrue), testSubjectIDs) {
		t.Errorf("Test 5: CSR Validation failed. CSR validation" +
			" succeeded when expected failure due to basic constraint" +
			" CA being set to true")
	}
}
