// Copyright 2019 Istio Authors
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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"

	certificates "k8s.io/api/certificates/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
)

const (
	exampleCACert = `-----BEGIN CERTIFICATE-----
MIIDCzCCAfOgAwIBAgIQbfOzhcKTldFipQ1X2WXpHDANBgkqhkiG9w0BAQsFADAv
MS0wKwYDVQQDEyRhNzU5YzcyZC1lNjcyLTQwMzYtYWMzYy1kYzAxMDBmMTVkNWUw
HhcNMTkwNTE2MjIxMTI2WhcNMjQwNTE0MjMxMTI2WjAvMS0wKwYDVQQDEyRhNzU5
YzcyZC1lNjcyLTQwMzYtYWMzYy1kYzAxMDBmMTVkNWUwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQC6sSAN80Ci0DYFpNDumGYoejMQai42g6nSKYS+ekvs
E7uT+eepO74wj8o6nFMNDu58+XgIsvPbWnn+3WtUjJfyiQXxmmTg8om4uY1C7R1H
gMsrL26pUaXZ/lTE8ZV5CnQJ9XilagY4iZKeptuZkxrWgkFBD7tr652EA3hmj+3h
4sTCQ+pBJKG8BJZDNRrCoiABYBMcFLJsaKuGZkJ6KtxhQEO9QxJVaDoSvlCRGa8R
fcVyYQyXOZ+0VHZJQgaLtqGpiQmlFttpCwDiLfMkk3UAd79ovkhN1MCq+O5N7YVt
eVQWaTUqUV2tKUFvVq21Zdl4dRaq+CF5U8uOqLY/4Kg9AgMBAAGjIzAhMA4GA1Ud
DwEB/wQEAwICBDAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQCg
oF71Ey2b1QY22C6BXcANF1+wPzxJovFeKYAnUqwh3rF7pIYCS/adZXOKlgDBsbcS
MxAGnCRi1s+A7hMYj3sQAbBXttc31557lRoJrx58IeN5DyshT53t7q4VwCzuCXFT
3zRHVRHQnO6LHgZx1FuKfwtkhfSXDyYU2fQYw2Hcb9krYU/alViVZdE0rENXCClq
xO7AQk5MJcGg6cfE5wWAKU1ATjpK4CN+RTn8v8ODLoI2SW3pfsnXxm93O+pp9HN4
+O+1PQtNUWhCfh+g6BN2mYo2OEZ8qGSxDlMZej4YOdVkW8PHmFZTK0w9iJKqM5o1
V6g5gZlqSoRhICK09tpc
-----END CERTIFICATE-----`

	exampleIssuedCert = `-----BEGIN CERTIFICATE-----
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
	fakeCACert = []byte("fake-CA-cert")
)

type mockTLSServer struct {
	httpServer *httptest.Server
}

func defaultReactionFunc(obj runtime.Object) kt.ReactionFunc {
	return func(act kt.Action) (bool, runtime.Object, error) {
		return true, obj, nil
	}
}

func TestGenKeyCertK8sCA(t *testing.T) {
	mutatingWebhookConfigFiles := []string{"./test-data/example-mutating-webhook-config.yaml"}
	validatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	mutatingWebhookConfigNames := []string{"mock-mutating-webook"}
	validatingWebhookConfigNames := []string{"mock-validating-webhook"}

	client := fake.NewSimpleClientset()
	csr := &certificates.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: "domain-cluster.local-ns--secret-mock-secret",
		},
		Status: certificates.CertificateSigningRequestStatus{
			Certificate: []byte(exampleIssuedCert),
		},
	}
	client.PrependReactor("get", "certificatesigningrequests", defaultReactionFunc(csr))

	testCases := map[string]struct {
		deleteWebhookConfigOnExit     bool
		gracePeriodRatio              float32
		minGracePeriod                time.Duration
		k8sCaCertFile                 string
		namespace                     string
		mutatingWebhookConfigFiles    []string
		mutatingWebhookConfigNames    []string
		mutatingWebhookSerivceNames   []string
		mutatingWebhookSerivcePorts   []int
		validatingWebhookConfigFiles  []string
		validatingWebhookConfigNames  []string
		validatingWebhookServiceNames []string
		validatingWebhookServicePorts []int

		secretName      string
		secretNameSpace string
		svcName         string

		expectFaill bool
	}{
		"gen cert should succeed": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			secretName:                   "mock-secret",
			secretNameSpace:              "mock-secret-namespace",
			svcName:                      "mock-service-name",
			expectFaill:                  false,
		},
	}

	for _, tc := range testCases {
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookSerivceNames, tc.mutatingWebhookSerivcePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if err != nil {
			t.Errorf("failed at creating webhook controller: %v", err)
			continue
		}

		_, _, _, err = genKeyCertK8sCA(wc, tc.secretName, tc.namespace, tc.svcName)
		if tc.expectFaill {
			if err == nil {
				t.Errorf("should have failed at updateMutatingWebhookConfig")
			}
			continue
		} else if err != nil {
			t.Errorf("failed at updateMutatingWebhookConfig: %v", err)
			continue
		}
	}
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
		cert, err := readCACert(tc.certPath)
		if tc.shouldFail {
			if err == nil {
				t.Errorf("should have failed at readCACert()")
			} else {
				// Should fail, skip the current case.
				continue
			}
		} else if err != nil {
			t.Errorf("failed at readCACert(): %v", err)
		}

		if !bytes.Equal(tc.expectedCert, cert) {
			t.Error("the certificate read is unexpected")
		}
	}
}

func TestIsTCPReachable(t *testing.T) {
	server1 := newMockTLSServer(t)
	defer server1.httpServer.Close()
	server2 := newMockTLSServer(t)
	defer server2.httpServer.Close()

	host := "127.0.0.1"
	port1, err := getServerPort(server1.httpServer)
	if err != nil {
		t.Fatalf("error to get the server 1 port: %v", err)
	}
	port2, err := getServerPort(server2.httpServer)
	if err != nil {
		t.Fatalf("error to get the server 2 port: %v", err)
	}

	// Server 1 should be reachable, since it is not closed.
	if !isTCPReachable(host, port1) {
		t.Fatal("server 1 is unreachable")
	}

	// After closing server 2, server 2 should not be reachable
	server2.httpServer.Close()
	if isTCPReachable(host, port2) {
		t.Fatal("server 2 is reachable")
	}
}

func TestRebuildMutatingWebhookConfigHelper(t *testing.T) {
	configFile := "./test-data/example-mutating-webhook-config.yaml"
	configName := "proto-mutate"
	webhookConfig, err := rebuildMutatingWebhookConfigHelper(fakeCACert, configFile, configName)
	if err != nil {
		t.Fatalf("err rebuilding mutating webhook config: %v", err)
	}
	if webhookConfig.Name != configName {
		t.Fatalf("webhookConfig.Name (%v) is different from %v", webhookConfig.Name, configName)
	}
	for i := range webhookConfig.Webhooks {
		if !bytes.Equal(webhookConfig.Webhooks[i].ClientConfig.CABundle, fakeCACert) {
			t.Fatalf("webhookConfig CA bundle(%v) is different from %v",
				webhookConfig.Webhooks[i].ClientConfig.CABundle, fakeCACert)
		}
	}
}

func TestRebuildValidatingWebhookConfigHelper(t *testing.T) {
	configFile := "./test-data/example-validating-webhook-config.yaml"
	configName := "proto-validate"
	webhookConfig, err := rebuildValidatingWebhookConfigHelper(fakeCACert, configFile, configName)
	if err != nil {
		t.Fatalf("err rebuilding mutating webhook config: %v", err)
	}
	if webhookConfig.Name != configName {
		t.Fatalf("webhookConfig.Name (%v) is different from %v", webhookConfig.Name, configName)
	}
	for i := range webhookConfig.Webhooks {
		if !bytes.Equal(webhookConfig.Webhooks[i].ClientConfig.CABundle, fakeCACert) {
			t.Fatalf("webhookConfig CA bundle(%v) is different from %v",
				webhookConfig.Webhooks[i].ClientConfig.CABundle, fakeCACert)
		}
	}
}

func TestCreateOrUpdateMutatingWebhookConfig(t *testing.T) {
	client := fake.NewSimpleClientset()
	emptyMutatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	mutatingWebhookConfigFiles := []string{"./test-data/example-mutating-webhook-config.yaml"}
	validatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	mutatingWebhookConfigNames := []string{"mock-mutating-webook"}
	validatingWebhookConfigNames := []string{"mock-validating-webhook"}

	testCases := map[string]struct {
		deleteWebhookConfigOnExit     bool
		gracePeriodRatio              float32
		minGracePeriod                time.Duration
		k8sCaCertFile                 string
		namespace                     string
		mutatingWebhookConfigFiles    []string
		mutatingWebhookConfigNames    []string
		mutatingWebhookSerivceNames   []string
		mutatingWebhookSerivcePorts   []int
		validatingWebhookConfigFiles  []string
		validatingWebhookConfigNames  []string
		validatingWebhookServiceNames []string
		validatingWebhookServicePorts []int

		rebuildConfig bool
		expectFaill   bool
	}{
		"nil mutatingwebhookconfiguration should fail": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			rebuildConfig:                false,
			expectFaill:                  true,
		},
		"empty mutatingwebhookconfiguration should succeed": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   emptyMutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			rebuildConfig:                true,
			expectFaill:                  false,
		},
		"non-empty mutatingwebhookconfiguration should succeed": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			rebuildConfig:                true,
			expectFaill:                  false,
		},
	}

	for _, tc := range testCases {
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookSerivceNames, tc.mutatingWebhookSerivcePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if err != nil {
			t.Errorf("failed at creating webhook controller: %v", err)
			continue
		}

		if tc.rebuildConfig {
			err := wc.rebuildMutatingWebhookConfig()
			if err != nil {
				t.Errorf("failed at rebuilding webhook config: %v", err)
				continue
			}
		}

		err = createOrUpdateMutatingWebhookConfig(wc)
		if tc.expectFaill {
			if err == nil {
				t.Errorf("should have failed at createOrUpdateMutatingWebhookConfig")
			}
			continue
		} else if err != nil {
			t.Errorf("failed at createOrUpdateMutatingWebhookConfig: %v", err)
			continue
		}
	}
}

func TestCreateOrUpdateValidatingWebhookConfig(t *testing.T) {
	client := fake.NewSimpleClientset()
	emptyValidatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	validatingWebhookConfigFiles := []string{"./test-data/example-validating-webhook-config.yaml"}
	mutatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	mutatingWebhookConfigNames := []string{"mock-mutating-webook"}
	validatingWebhookConfigNames := []string{"mock-validating-webhook"}

	testCases := map[string]struct {
		deleteWebhookConfigOnExit     bool
		gracePeriodRatio              float32
		minGracePeriod                time.Duration
		k8sCaCertFile                 string
		namespace                     string
		mutatingWebhookConfigFiles    []string
		mutatingWebhookConfigNames    []string
		mutatingWebhookSerivceNames   []string
		mutatingWebhookSerivcePorts   []int
		validatingWebhookConfigFiles  []string
		validatingWebhookConfigNames  []string
		validatingWebhookServiceNames []string
		validatingWebhookServicePorts []int

		rebuildConfig bool
		expectFaill   bool
	}{
		"nil mutatingwebhookconfiguration should fail": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			rebuildConfig:                false,
			expectFaill:                  true,
		},
		"empty mutatingwebhookconfiguration should succeed": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			validatingWebhookConfigFiles: emptyValidatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			rebuildConfig:                true,
			expectFaill:                  false,
		},
		"non-empty mutatingwebhookconfiguration should succeed": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			rebuildConfig:                true,
			expectFaill:                  false,
		},
	}

	for _, tc := range testCases {
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookSerivceNames, tc.mutatingWebhookSerivcePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if err != nil {
			t.Errorf("failed at creating webhook controller: %v", err)
			continue
		}

		if tc.rebuildConfig {
			err := wc.rebuildValidatingWebhookConfig()
			if err != nil {
				t.Errorf("failed at rebuilding webhook config: %v", err)
				continue
			}
		}

		err = createOrUpdateValidatingWebhookConfig(wc)
		if tc.expectFaill {
			if err == nil {
				t.Errorf("should have failed at createOrUpdateValidatingWebhookConfig")
			}
			continue
		} else if err != nil {
			t.Errorf("failed at createOrUpdateValidatingWebhookConfig: %v", err)
			continue
		}
	}
}

func TestUpdateMutatingWebhookConfig(t *testing.T) {
	client := fake.NewSimpleClientset()
	emptyMutatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	mutatingWebhookConfigFiles := []string{"./test-data/example-mutating-webhook-config.yaml"}
	validatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	mutatingWebhookConfigNames := []string{"mock-mutating-webook"}
	validatingWebhookConfigNames := []string{"mock-validating-webhook"}

	testCases := map[string]struct {
		deleteWebhookConfigOnExit     bool
		gracePeriodRatio              float32
		minGracePeriod                time.Duration
		k8sCaCertFile                 string
		namespace                     string
		mutatingWebhookConfigFiles    []string
		mutatingWebhookConfigNames    []string
		mutatingWebhookSerivceNames   []string
		mutatingWebhookSerivcePorts   []int
		validatingWebhookConfigFiles  []string
		validatingWebhookConfigNames  []string
		validatingWebhookServiceNames []string
		validatingWebhookServicePorts []int

		expectFaill bool
	}{
		"empty mutatingwebhookconfiguration should succeed": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   emptyMutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			expectFaill:                  false,
		},
		"non-empty mutatingwebhookconfiguration should succeed": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			expectFaill:                  false,
		},
	}

	for _, tc := range testCases {
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookSerivceNames, tc.mutatingWebhookSerivcePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if err != nil {
			t.Errorf("failed at creating webhook controller: %v", err)
			continue
		}

		err = updateMutatingWebhookConfig(wc)
		if tc.expectFaill {
			if err == nil {
				t.Errorf("should have failed at updateMutatingWebhookConfig")
			}
			continue
		} else if err != nil {
			t.Errorf("failed at updateMutatingWebhookConfig: %v", err)
			continue
		}
	}
}

func TestUpdateValidatingWebhookConfig(t *testing.T) {
	client := fake.NewSimpleClientset()
	emptyValidatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	validatingWebhookConfigFiles := []string{"./test-data/example-validating-webhook-config.yaml"}
	mutatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	mutatingWebhookConfigNames := []string{"mock-mutating-webook"}
	validatingWebhookConfigNames := []string{"mock-validating-webhook"}

	testCases := map[string]struct {
		deleteWebhookConfigOnExit     bool
		gracePeriodRatio              float32
		minGracePeriod                time.Duration
		k8sCaCertFile                 string
		namespace                     string
		mutatingWebhookConfigFiles    []string
		mutatingWebhookConfigNames    []string
		mutatingWebhookSerivceNames   []string
		mutatingWebhookSerivcePorts   []int
		validatingWebhookConfigFiles  []string
		validatingWebhookConfigNames  []string
		validatingWebhookServiceNames []string
		validatingWebhookServicePorts []int

		expectFaill bool
	}{
		"empty mutatingwebhookconfiguration should succeed": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			validatingWebhookConfigFiles: emptyValidatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			expectFaill:                  false,
		},
		"non-empty mutatingwebhookconfiguration should succeed": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			expectFaill:                  false,
		},
	}

	for _, tc := range testCases {
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookSerivceNames, tc.mutatingWebhookSerivcePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if err != nil {
			t.Errorf("failed at creating webhook controller: %v", err)
			continue
		}

		err = updateValidatingWebhookConfig(wc)
		if tc.expectFaill {
			if err == nil {
				t.Errorf("should have failed at updateValidatingWebhookConfig")
			}
			continue
		} else if err != nil {
			t.Errorf("failed at updateValidatingWebhookConfig: %v", err)
			continue
		}
	}
}

func TestUpdateCertAndWebhookConfig(t *testing.T) {
	client := fake.NewSimpleClientset()
	validatingWebhookConfigFiles := []string{"./test-data/example-validating-webhook-config.yaml"}
	mutatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	mutatingWebhookConfigNames := []string{"mock-mutating-webook"}
	validatingWebhookConfigNames := []string{"mock-validating-webhook"}

	testCases := map[string]struct {
		deleteWebhookConfigOnExit     bool
		gracePeriodRatio              float32
		minGracePeriod                time.Duration
		k8sCaCertFile                 string
		newK8sCaCertFile              string
		namespace                     string
		mutatingWebhookConfigFiles    []string
		mutatingWebhookConfigNames    []string
		mutatingWebhookSerivceNames   []string
		mutatingWebhookSerivcePorts   []int
		validatingWebhookConfigFiles  []string
		validatingWebhookConfigNames  []string
		validatingWebhookServiceNames []string
		validatingWebhookServicePorts []int

		expectFaill                 bool
		expectEmptyMutateCABundle   bool
		expectEmptyValidateCABundle bool
	}{
		"same CA certificates should succeed": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			newK8sCaCertFile:             "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			expectFaill:                  false,
			expectEmptyMutateCABundle:    true,
			expectEmptyValidateCABundle:  true,
		},
		"change to invalid CA certificate should fail": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			newK8sCaCertFile:             "./invalid-path/invalid",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			expectFaill:                  true,
			expectEmptyMutateCABundle:    true,
			expectEmptyValidateCABundle:  true,
		},
		"change to valid CA certificate should succeed": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert2.pem",
			newK8sCaCertFile:             "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			expectFaill:                  false,
			expectEmptyMutateCABundle:    true,
			expectEmptyValidateCABundle:  false,
		},
	}

	for _, tc := range testCases {
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookSerivceNames, tc.mutatingWebhookSerivcePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if err != nil {
			t.Errorf("failed at creating webhook controller: %v", err)
			continue
		}

		// Change the CA certificate
		wc.k8sCaCertFile = tc.newK8sCaCertFile
		err = updateCertAndWebhookConfig(wc)
		if tc.expectFaill {
			if err == nil {
				t.Errorf("should have failed at updateCertAndWebhookConfig")
			}
			continue
		} else if err != nil {
			t.Errorf("failed at updateCertAndWebhookConfig: %v", err)
			continue
		}

		if tc.expectEmptyMutateCABundle {
			if wc.mutatingWebhookConfig != nil &&
				len(wc.mutatingWebhookConfig.Webhooks) > 0 &&
				len(wc.mutatingWebhookConfig.Webhooks[0].ClientConfig.CABundle) > 0 {
				t.Error("CA bundle of mutating webhook is unexpected: should be empty")
			}
		} else if wc.mutatingWebhookConfig == nil || len(wc.mutatingWebhookConfig.Webhooks) == 0 ||
			len(wc.mutatingWebhookConfig.Webhooks[0].ClientConfig.CABundle) == 0 {
			t.Error("CA bundle of mutating webhook is unexpected: should be non-empty")
		}

		if tc.expectEmptyValidateCABundle {
			if wc.validatingWebhookConfig != nil && len(wc.validatingWebhookConfig.Webhooks) > 0 &&
				len(wc.validatingWebhookConfig.Webhooks[0].ClientConfig.CABundle) > 0 {
				t.Error("CA bundle of validating webhook is unexpected: should be empty")
			}
		} else if wc.validatingWebhookConfig == nil || len(wc.validatingWebhookConfig.Webhooks) == 0 ||
			len(wc.validatingWebhookConfig.Webhooks[0].ClientConfig.CABundle) == 0 {
			t.Error("CA bundle of validating webhook is unexpected: should be non-empty")
		}
	}
}

func TestReloadCACert(t *testing.T) {
	client := fake.NewSimpleClientset()
	mutatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	validatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}

	testCases := map[string]struct {
		deleteWebhookConfigOnExit     bool
		gracePeriodRatio              float32
		minGracePeriod                time.Duration
		k8sCaCertFile                 string
		namespace                     string
		mutatingWebhookConfigFiles    []string
		mutatingWebhookConfigNames    []string
		mutatingWebhookSerivceNames   []string
		mutatingWebhookSerivcePorts   []int
		validatingWebhookConfigFiles  []string
		validatingWebhookConfigNames  []string
		validatingWebhookServiceNames []string
		validatingWebhookServicePorts []int

		expectFaill   bool
		expectChanged bool
	}{
		"reload from valid CA cert path": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			expectFaill:                  false,
			expectChanged:                false,
		},
	}

	for _, tc := range testCases {
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookSerivceNames, tc.mutatingWebhookSerivcePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if err != nil {
			t.Errorf("failed at creating webhook controller: %v", err)
			continue
		}
		changed, err := reloadCACert(wc)
		if tc.expectFaill {
			if err == nil {
				t.Errorf("should have failed at reloading CA cert")
			}
			continue
		} else if err != nil {
			t.Errorf("failed at reloading CA cert: %v", err)
			continue
		}
		if tc.expectChanged {
			if !changed {
				t.Error("expect changed but not changed")
			}
		} else {
			if changed {
				t.Error("expect unchanged but changed")
			}
		}
	}
}

// newMockTLSServer creates a mock TLS server for testing purpose.
func newMockTLSServer(t *testing.T) *mockTLSServer {
	server := &mockTLSServer{}

	handler := http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		t.Logf("request: %+v", *req)
		switch req.URL.Path {
		default:
			t.Logf("The request contains path: %v", req.URL)
			resp.WriteHeader(http.StatusOK)
		}
	})

	server.httpServer = httptest.NewTLSServer(handler)

	t.Logf("Serving TLS at: %v", server.httpServer.URL)

	return server
}

// Get the server port from server.URL (e.g., https://127.0.0.1:36253)
func getServerPort(server *httptest.Server) (int, error) {
	strs := strings.Split(server.URL, ":")
	if len(strs) < 2 {
		return 0, fmt.Errorf("server.URL is invalid: %v", server.URL)
	}
	port, err := strconv.Atoi(strs[len(strs)-1])
	if err != nil {
		return 0, fmt.Errorf("error to extract port from URL: %v", server.URL)
	}
	return port, nil
}
