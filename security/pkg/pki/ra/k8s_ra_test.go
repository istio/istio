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
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
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
)

var (
	testCsrHostName       = spiffe.Identity{TrustDomain: "cluster.local", Namespace: "default", ServiceAccount: "bookinfo-productpage"}.String()
	TestCACertFile        = "../testdata/example-ca-cert.pem"
	mismatchCertChainFile = "../testdata/cert-chain.pem"

	// The OID for the BasicConstraints extension (See
	// https://www.alvestrand.no/objectid/2.5.29.19.html).
	oidBasicConstraints = asn1.ObjectIdentifier{2, 5, 29, 19}
)

// basicConstraints is a structure that represents the ASN.1 encoding of the
// BasicConstraints extension.
// The structure is borrowed from
// https://github.com/golang/go/blob/master/src/crypto/x509/x509.go#L975
type basicConstraints struct {
	IsCA       bool `asn1:"optional"`
	MaxPathLen int  `asn1:"optional,default:-1"`
}

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
					t.Fatal("expected failed, got none")
				}
			}
		})
	}
}

// createDefaultFakeCsr create a default fake CSR
func createDefaultFakeCsr(t *testing.T) []byte {
	return createFakeCsr(t, "")
}

// createFakeCsr creates a fake CSR with the given org
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

// genTestCSR generates a test CSR with more configurability than supported by the GenCSR() function
// For basic CSR generation, use createFakeCsr() since this is more consistent with how istio-proxy
// generated CSRs
func genTestCSR(t *testing.T, options pkiutil.CertOptions, cn string) []byte {
	// GenCSRTemplates configures the following fields:
	// - CN if IsDualUse is set to true
	// - ExtraExtensions for SANs using host values
	// - Organization to the value of certOpts.Org
	csr, err := pkiutil.GenCSRTemplate(options)
	if err != nil {
		t.Fatalf("Error creating fake CSR: %v", err)
		return nil
	}
	// Set DNSNames
	csr.DNSNames = append(csr.DNSNames, options.DNSNames)

	// Set IsCA
	basicConstraints, err := marshalBasicConstraints(options.IsCA)
	if err != nil {
		t.Fatalf("Error marshaling basic constraints: %v", err)
	}
	csr.Extensions = append(csr.Extensions, *basicConstraints)

	// Allow setting CN even if isDualUse is false
	if !options.IsDualUse && cn != "" {
		csr.Subject.CommonName = cn
	}

	// Set certOptions not being tested
	if options.RSAKeySize == 0 {
		// MinimumRsaKeySize is 2048
		options.RSAKeySize = 2048
	}
	if options.ECSigAlg == "" {
		options.ECSigAlg = pkiutil.SupportedECSignatureAlgorithms("ECDSA")
	}

	// Generate private key for the CSR
	// copied from GenCSR() in security/pkg/pki/util/generate_csr.go
	var priv any
	if options.ECSigAlg != "" {
		switch options.ECSigAlg {
		case pkiutil.EcdsaSigAlg:
			var curve elliptic.Curve
			switch options.ECCCurve {
			case pkiutil.P384Curve:
				curve = elliptic.P384()
			default:
				curve = elliptic.P256()
			}
			priv, err = ecdsa.GenerateKey(curve, rand.Reader)
			if err != nil {
				t.Fatalf("EC key generation failed (%v)", err)
				return nil
			}
		default:
			t.Fatal("csr cert generation fails due to unsupported EC signature algorithm")
			return nil
		}
	} else {
		if options.RSAKeySize < pkiutil.MinimumRsaKeySize {
			t.Fatalf("requested key size does not meet the minimum required size of %d (requested: %d)", pkiutil.MinimumRsaKeySize, options.RSAKeySize)
			return nil
		}

		priv, err = rsa.GenerateKey(rand.Reader, options.RSAKeySize)
		if err != nil {
			t.Fatalf("RSA key generation failed (%v)", err)
			return nil
		}
	}

	// CreateCertificateRequest creates a new certificate using template and private key
	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, csr, crypto.PrivateKey(priv))
	if err != nil {
		t.Fatalf("x509 CreateCertificateRequest (%v)", err)
		return nil
	}

	return csrBytes
}

// marshalBasicConstraints marshals the isCA value into a BasicConstraints extension.
func marshalBasicConstraints(isCA bool) (*pkix.Extension, error) {
	ext := &pkix.Extension{Id: oidBasicConstraints, Critical: true}
	// Leaving MaxPathLen as zero indicates that no maximum path
	// length is desired, unless MaxPathLenZero is set. A value of
	// -1 causes encoding/asn1 to omit the value as desired.
	var err error
	ext.Value, err = asn1.Marshal(basicConstraints{isCA, -1})
	return ext, err
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
	caSigner := "kubernetes.io/kube-apiserver-client"
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

	testCSRWithInvalidCN := genTestCSR(t, pkiutil.CertOptions{Host: testCsrHostName}, "test.test.svc.cluster.local")
	testCSRWithCATrue := genTestCSR(t, pkiutil.CertOptions{Host: testCsrHostName, IsCA: true}, "")
	testCSRWithDNSHostNames := genTestCSR(t, pkiutil.CertOptions{Host: testCsrHostName, DNSNames: "test.test.svc.cluster.local"}, "")

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

	// Independently creating CSRs with GetTestCSR for tests for
	// fields that are not configurable via GenCSR()
	// Test Case 4
	if ValidateCSR(testCSRWithInvalidCN, testSubjectIDs) {
		t.Errorf("Test 4: CSR Validation failed. CSR validation" +
			" succeeded when expected failure due invalid CN")
	}

	// Test Case 5
	if ValidateCSR(testCSRWithCATrue, testSubjectIDs) {
		t.Errorf("Test 5: CSR Validation failed. CSR validation" +
			" succeeded when expected failure due to basic constraint" +
			" CA being set to true")
	}

	// Test Case 6
	if ValidateCSR(testCSRWithDNSHostNames, testSubjectIDs) {
		t.Errorf("Test 5: CSR Validation failed. CSR validation" +
			" succeeded when expected failure due to DNSHostNames")
	}
}
