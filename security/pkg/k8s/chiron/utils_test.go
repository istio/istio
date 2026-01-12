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

package chiron

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	cert "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test"
	csrctrl "istio.io/istio/pkg/test/csrctrl/controllers"
	"istio.io/istio/pkg/test/util/assert"
	pkiutil "istio.io/istio/security/pkg/pki/util"
)

const (
	// exampleCACert copied from samples/certs/ca-cert.pem
	exampleCACert = `-----BEGIN CERTIFICATE-----
MIIDnzCCAoegAwIBAgIJAON1ifrBZ2/BMA0GCSqGSIb3DQEBCwUAMIGLMQswCQYD
VQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTESMBAGA1UEBwwJU3Vubnl2YWxl
MQ4wDAYDVQQKDAVJc3RpbzENMAsGA1UECwwEVGVzdDEQMA4GA1UEAwwHUm9vdCBD
QTEiMCAGCSqGSIb3DQEJARYTdGVzdHJvb3RjYUBpc3Rpby5pbzAgFw0xODAxMjQx
OTE1NTFaGA8yMTE3MTIzMTE5MTU1MVowWTELMAkGA1UEBhMCVVMxEzARBgNVBAgT
CkNhbGlmb3JuaWExEjAQBgNVBAcTCVN1bm55dmFsZTEOMAwGA1UEChMFSXN0aW8x
ETAPBgNVBAMTCElzdGlvIENBMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKC
AQEAyzCxr/xu0zy5rVBiso9ffgl00bRKvB/HF4AX9/ytmZ6Hqsy13XIQk8/u/By9
iCvVwXIMvyT0CbiJq/aPEj5mJUy0lzbrUs13oneXqrPXf7ir3HzdRw+SBhXlsh9z
APZJXcF93DJU3GabPKwBvGJ0IVMJPIFCuDIPwW4kFAI7R/8A5LSdPrFx6EyMXl7K
M8jekC0y9DnTj83/fY72WcWX7YTpgZeBHAeeQOPTZ2KYbFal2gLsar69PgFS0Tom
ESO9M14Yit7mzB1WDK2z9g3r+zLxENdJ5JG/ZskKe+TO4Diqi5OJt/h8yspS1ck8
LJtCole9919umByg5oruflqIlQIDAQABozUwMzALBgNVHQ8EBAMCAgQwDAYDVR0T
BAUwAwEB/zAWBgNVHREEDzANggtjYS5pc3Rpby5pbzANBgkqhkiG9w0BAQsFAAOC
AQEAltHEhhyAsve4K4bLgBXtHwWzo6SpFzdAfXpLShpOJNtQNERb3qg6iUGQdY+w
A2BpmSkKr3Rw/6ClP5+cCG7fGocPaZh+c+4Nxm9suMuZBZCtNOeYOMIfvCPcCS+8
PQ/0hC4/0J3WJKzGBssaaMufJxzgFPPtDJ998kY8rlROghdSaVt423/jXIAYnP3Y
05n8TGERBj7TLdtIVbtUIx3JHAo3PWJywA6mEDovFMJhJERp9sDHIr1BbhXK1TFN
Z6HNH6gInkSSMtvC4Ptejb749PTaePRPF7ID//eq/3AH8UK50F3TQcLjEqWUsJUn
aFKltOc+RAjzDklcUPeG4Y6eMA==
-----END CERTIFICATE-----`

	// exampleIssuedCert copied from samples/certs/cert-chain.pem
	exampleIssuedCert = `-----BEGIN CERTIFICATE-----
MIIDnzCCAoegAwIBAgIJAON1ifrBZ2/BMA0GCSqGSIb3DQEBCwUAMIGLMQswCQYD
VQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTESMBAGA1UEBwwJU3Vubnl2YWxl
MQ4wDAYDVQQKDAVJc3RpbzENMAsGA1UECwwEVGVzdDEQMA4GA1UEAwwHUm9vdCBD
QTEiMCAGCSqGSIb3DQEJARYTdGVzdHJvb3RjYUBpc3Rpby5pbzAgFw0xODAxMjQx
OTE1NTFaGA8yMTE3MTIzMTE5MTU1MVowWTELMAkGA1UEBhMCVVMxEzARBgNVBAgT
CkNhbGlmb3JuaWExEjAQBgNVBAcTCVN1bm55dmFsZTEOMAwGA1UEChMFSXN0aW8x
ETAPBgNVBAMTCElzdGlvIENBMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKC
AQEAyzCxr/xu0zy5rVBiso9ffgl00bRKvB/HF4AX9/ytmZ6Hqsy13XIQk8/u/By9
iCvVwXIMvyT0CbiJq/aPEj5mJUy0lzbrUs13oneXqrPXf7ir3HzdRw+SBhXlsh9z
APZJXcF93DJU3GabPKwBvGJ0IVMJPIFCuDIPwW4kFAI7R/8A5LSdPrFx6EyMXl7K
M8jekC0y9DnTj83/fY72WcWX7YTpgZeBHAeeQOPTZ2KYbFal2gLsar69PgFS0Tom
ESO9M14Yit7mzB1WDK2z9g3r+zLxENdJ5JG/ZskKe+TO4Diqi5OJt/h8yspS1ck8
LJtCole9919umByg5oruflqIlQIDAQABozUwMzALBgNVHQ8EBAMCAgQwDAYDVR0T
BAUwAwEB/zAWBgNVHREEDzANggtjYS5pc3Rpby5pbzANBgkqhkiG9w0BAQsFAAOC
AQEAltHEhhyAsve4K4bLgBXtHwWzo6SpFzdAfXpLShpOJNtQNERb3qg6iUGQdY+w
A2BpmSkKr3Rw/6ClP5+cCG7fGocPaZh+c+4Nxm9suMuZBZCtNOeYOMIfvCPcCS+8
PQ/0hC4/0J3WJKzGBssaaMufJxzgFPPtDJ998kY8rlROghdSaVt423/jXIAYnP3Y
05n8TGERBj7TLdtIVbtUIx3JHAo3PWJywA6mEDovFMJhJERp9sDHIr1BbhXK1TFN
Z6HNH6gInkSSMtvC4Ptejb749PTaePRPF7ID//eq/3AH8UK50F3TQcLjEqWUsJUn
aFKltOc+RAjzDklcUPeG4Y6eMA==
-----END CERTIFICATE-----
`
	DefaulCertTTL = 24 * time.Hour
)

func defaultReactionFunc(obj runtime.Object) kt.ReactionFunc {
	return func(act kt.Action) (bool, runtime.Object, error) {
		return true, obj, nil
	}
}

const testSigner = "test-signer"

func runTestSigner(t test.Failer) ([]csrctrl.SignerRootCert, kube.CLIClient) {
	c := kube.NewFakeClient()
	signers, err := csrctrl.RunCSRController(testSigner, test.NewStop(t), []kube.Client{c})
	if err != nil {
		t.Fatal(err)
	}
	return signers, c
}

func TestGenKeyCertK8sCA(t *testing.T) {
	log.FindScope("default").SetOutputLevel(log.DebugLevel)
	signers, client := runTestSigner(t)
	ca := filepath.Join(t.TempDir(), "root-cert.pem")
	os.WriteFile(ca, []byte(signers[0].Rootcert), 0o666)

	_, _, _, err := GenKeyCertK8sCA(client.Kube(), "foo", ca, testSigner, true, DefaulCertTTL)
	assert.NoError(t, err)
}

func TestReadCACert(t *testing.T) {
	testCases := map[string]struct {
		certPath     string
		shouldFail   bool
		expectedCert []byte
	}{
		"cert not exist": {
			certPath:   "./invalid-path/invalid-file",
			shouldFail: true,
		},
		"cert valid": {
			certPath:     "./test-data/example-ca-cert.pem",
			shouldFail:   false,
			expectedCert: []byte(exampleCACert),
		},
		"cert invalid": {
			certPath:   "./test-data/example-invalid-ca-cert.pem",
			shouldFail: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.certPath, func(t *testing.T) {
			cert, err := readCACert(tc.certPath)
			if tc.shouldFail {
				if err == nil {
					t.Errorf("should have failed at readCACert()")
				} else {
					// Should fail, skip the current case.
					return
				}
			} else if err != nil {
				t.Errorf("failed at readCACert(): %v", err)
			}

			if !bytes.Equal(tc.expectedCert, cert) {
				t.Error("the certificate read is unexpected")
			}
		})
	}
}

func TestSubmitCSR(t *testing.T) {
	testCases := map[string]struct {
		gracePeriodRatio float32
		minGracePeriod   time.Duration
		k8sCaCertFile    string
		dnsNames         []string
		secretNames      []string

		secretName      string
		secretNameSpace string
		expectFail      bool
	}{
		"submitting a CSR without duplicate should succeed": {
			gracePeriodRatio: 0.6,
			k8sCaCertFile:    "./test-data/example-ca-cert.pem",
			dnsNames:         []string{"foo"},
			secretNames:      []string{"istio.webhook.foo"},
			secretName:       "mock-secret",
			secretNameSpace:  "mock-secret-namespace",
			expectFail:       false,
		},
	}

	for tcName, tc := range testCases {
		t.Run(tcName, func(t *testing.T) {
			client := fake.NewClientset()
			csr := &cert.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "domain-cluster.local-ns--secret-mock-secret",
				},
				Status: cert.CertificateSigningRequestStatus{
					Certificate: []byte(exampleIssuedCert),
				},
			}
			client.PrependReactor("get", "certificatesigningrequests", defaultReactionFunc(csr))

			usages := []cert.KeyUsage{
				cert.UsageDigitalSignature,
				cert.UsageKeyEncipherment,
				cert.UsageServerAuth,
				cert.UsageClientAuth,
			}
			r, err := submitCSR(client, []byte("test-pem"), "test-signer",
				usages, DefaulCertTTL)
			if tc.expectFail {
				assert.Error(t, err)
			} else if err != nil || r == nil {
				t.Errorf("test case (%s) failed unexpectedly: %v", tcName, err)
			}
		})
	}
}

func TestReadSignedCertificate(t *testing.T) {
	testCases := []struct {
		name              string
		gracePeriodRatio  float32
		minGracePeriod    time.Duration
		k8sCaCertFile     string
		secretNames       []string
		dnsNames          []string
		serviceNamespaces []string

		secretName      string
		secretNameSpace string

		invalidCert     bool
		expectFail      bool
		certificateData []byte
	}{
		{
			name:              "read signed cert should succeed",
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          []string{"foo"},
			secretNames:       []string{"istio.webhook.foo"},
			serviceNamespaces: []string{"foo.ns"},
			secretName:        "mock-secret",
			secretNameSpace:   "mock-secret-namespace",
			invalidCert:       false,
			expectFail:        false,
			certificateData:   []byte(exampleIssuedCert),
		},
		{
			name:              "read invalid signed cert should fail",
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          []string{"foo"},
			secretNames:       []string{"istio.webhook.foo"},
			serviceNamespaces: []string{"foo.ns"},
			secretName:        "mock-secret",
			secretNameSpace:   "mock-secret-namespace",
			invalidCert:       true,
			expectFail:        true,
			certificateData:   []byte("invalid-cert"),
		},
		{
			name:              "read empty signed cert should fail",
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          []string{"foo"},
			secretNames:       []string{"istio.webhook.foo"},
			serviceNamespaces: []string{"foo.ns"},
			secretName:        "mock-secret",
			secretNameSpace:   "mock-secret-namespace",
			invalidCert:       true,
			expectFail:        true,
			certificateData:   []byte(""),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			log.FindScope("default").SetOutputLevel(log.DebugLevel)
			client := initFakeKubeClient(t, tc.certificateData)

			// 4. Read the signed certificate
			_, _, err := SignCSRK8s(client.Kube(), createFakeCsr(t), "fake-signer", []cert.KeyUsage{cert.UsageAny}, "fake.com",
				tc.k8sCaCertFile, true, true, 1*time.Second)

			if tc.expectFail {
				if err == nil {
					t.Fatal("should have failed at updateMutatingWebhookConfig")
				}
			} else if err != nil {
				t.Fatalf("failed at updateMutatingWebhookConfig: %v", err)
			}
		})
	}
}

func createFakeCsr(t *testing.T) []byte {
	options := pkiutil.CertOptions{
		Host:       "fake.com",
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

func initFakeKubeClient(t test.Failer, certificate []byte) kube.CLIClient {
	client := kube.NewFakeClient()
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
					log.Debugf("test signer skip, already signed: %v", csr.Name)
					continue
				}
				if approved(csr) {
					// This is a pretty terrible hack, but client-go fake doesn't properly support list+watch,
					// so any updates in between the list and watch would be missed. So give some time for the watch to start
					time.Sleep(time.Millisecond * 25)
					csr.Status.Certificate = certificate
					_, err := client.Kube().CertificatesV1().CertificateSigningRequests().UpdateStatus(ctx, csr, metav1.UpdateOptions{})
					log.Debugf("test signer sign %v: %v", csr.Name, err)
				} else {
					log.Debugf("test signer skip, not approved: %v", csr.Name)
				}
			}
		}
	}()
	return client
}

func approved(csr *cert.CertificateSigningRequest) bool {
	return GetCondition(csr.Status.Conditions, cert.CertificateApproved).Status == corev1.ConditionTrue
}

func GetCondition(conditions []cert.CertificateSigningRequestCondition, condition cert.RequestConditionType) cert.CertificateSigningRequestCondition {
	for _, cond := range conditions {
		if cond.Type == condition {
			return cond
		}
	}
	return cert.CertificateSigningRequestCondition{}
}
