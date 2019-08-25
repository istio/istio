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
	"io/ioutil"
	"reflect"
	"testing"
	"time"

	"k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"istio.io/istio/security/pkg/pki/util"

	cert "k8s.io/api/certificates/v1beta1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"

	"k8s.io/client-go/kubernetes/fake"
)

const (
	// The example certificate here can be generated through
	// the following command:
	// kubectl exec -it POD-NAME -n NAMESPACE -- cat /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
	// Its validity is 5 years.
	exampleCACert1 = `-----BEGIN CERTIFICATE-----
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
)

var (
	runtimeScheme = k8sRuntime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

func TestNewWebhookController(t *testing.T) {
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
		mutatingWebhookServiceNames   []string
		mutatingWebhookServicePorts   []int
		validatingWebhookConfigFiles  []string
		validatingWebhookConfigNames  []string
		validatingWebhookServiceNames []string
		validatingWebhookServicePorts []int
		shouldFail                    bool
	}{
		"invalid grade period ratio": {
			gracePeriodRatio:             1.5,
			k8sCaCertFile:                "./test-data/example-invalid-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			shouldFail:                   true,
		},
		"invalid CA cert path": {
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./invalid-path/invalid-file",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			shouldFail:                   true,
		},
		"valid CA cert path": {
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			shouldFail:                   false,
		},
		"invalid mutating webhook config file": {
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   []string{"./invalid-path/invalid-file"},
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			shouldFail:                   true,
		},
		"invalid validatating webhook config file": {
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			validatingWebhookConfigFiles: []string{"./invalid-path/invalid-file"},
			shouldFail:                   true,
		},
	}

	for _, tc := range testCases {
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookServiceNames, tc.mutatingWebhookServicePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if wc != nil && wc.K8sCaCertWatcher != nil {
			defer wc.K8sCaCertWatcher.Close()
		}
		if wc != nil && wc.MutatingWebhookFileWatcher != nil {
			defer wc.MutatingWebhookFileWatcher.Close()
		}
		if wc != nil && wc.ValidatingWebhookFileWatcher != nil {
			defer wc.ValidatingWebhookFileWatcher.Close()
		}
		if tc.shouldFail {
			if err == nil {
				t.Errorf("should have failed at NewWebhookController()")
			} else {
				// Should fail, skip the current case.
				continue
			}
		} else if err != nil {
			t.Errorf("should not fail at NewWebhookController(), err: %v", err)
		}
	}
}

func TestCleanUpCertGen(t *testing.T) {
	validatingWebhookConfigFiles := []string{"./test-data/example-validating-webhook-config.yaml"}
	mutatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	mutatingWebhookConfigNames := []string{"mock-mutating-webook"}
	validatingWebhookConfigNames := []string{"mock-validating-webhook"}
	mutatingWebhookServiceNames := []string{"foo"}

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
	}{
		"clean up a CSR should succeed": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			namespace:                    "foo.ns",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			mutatingWebhookSerivceNames:  mutatingWebhookServiceNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
		},
	}

	client := fake.NewSimpleClientset()
	csrName := "test-csr"

	for _, tc := range testCases {
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookSerivceNames, tc.mutatingWebhookSerivcePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if wc != nil && wc.K8sCaCertWatcher != nil {
			defer wc.K8sCaCertWatcher.Close()
		}
		if wc != nil && wc.MutatingWebhookFileWatcher != nil {
			defer wc.MutatingWebhookFileWatcher.Close()
		}
		if wc != nil && wc.ValidatingWebhookFileWatcher != nil {
			defer wc.ValidatingWebhookFileWatcher.Close()
		}
		if err != nil {
			t.Fatalf("failed at creating webhook controller: %v", err)
		}

		options := util.CertOptions{
			Host:       "test-host",
			RSAKeySize: keySize,
			IsDualUse:  false,
			PKCS8Key:   false,
		}
		csrPEM, _, err := util.GenCSR(options)
		if err != nil {
			t.Fatalf("CSR generation error (%v)", err)
		}

		k8sCSR := &cert.CertificateSigningRequest{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "certificates.k8s.io/v1beta1",
				Kind:       "CertificateSigningRequest",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: csrName,
			},
			Spec: cert.CertificateSigningRequestSpec{
				Request: csrPEM,
				Groups:  []string{"system:authenticated"},
				Usages: []cert.KeyUsage{
					cert.UsageDigitalSignature,
					cert.UsageKeyEncipherment,
					cert.UsageServerAuth,
					cert.UsageClientAuth,
				},
			},
		}
		_, err = wc.certClient.CertificateSigningRequests().Create(k8sCSR)
		if err != nil {
			t.Fatalf("error when creating CSR: %v", err)
		}

		csr, err := wc.certClient.CertificateSigningRequests().Get(csrName, metav1.GetOptions{})
		if err != nil || csr == nil {
			t.Fatalf("failed to get CSR: name (%v), err (%v), CSR (%v)", csrName, err, csr)
		}

		// The CSR should be deleted.
		err = wc.cleanUpCertGen(csrName)
		if err != nil {
			t.Errorf("cleanUpCertGen returns an error: %v", err)
		}
		_, err = wc.certClient.CertificateSigningRequests().Get(csrName, metav1.GetOptions{})
		if err == nil {
			t.Fatalf("should failed at getting CSR: name (%v)", csrName)
		}
	}
}

func TestRebuildMutatingWebhookConfig(t *testing.T) {
	mutatingWebhookConfigFiles := []string{"./test-data/example-mutating-webhook-config.yaml"}
	mutatingWebhookConfigNames := []string{"protomutate"}
	mutatingWebhookServiceNames := []string{"foo"}
	validatingWebhookConfigFiles := []string{"./test-data/example-validating-webhook-config.yaml"}
	validatingWebhookConfigNames := []string{"protovalidate"}

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
	}{
		"rebuildMutatingWebhookConfig should succeed": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			namespace:                    "foo.ns",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			mutatingWebhookSerivceNames:  mutatingWebhookServiceNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
		},
	}

	client := fake.NewSimpleClientset()

	for _, tc := range testCases {
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookSerivceNames, tc.mutatingWebhookSerivcePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if wc != nil && wc.K8sCaCertWatcher != nil {
			defer wc.K8sCaCertWatcher.Close()
		}
		if wc != nil && wc.MutatingWebhookFileWatcher != nil {
			defer wc.MutatingWebhookFileWatcher.Close()
		}
		if wc != nil && wc.ValidatingWebhookFileWatcher != nil {
			defer wc.ValidatingWebhookFileWatcher.Close()
		}
		if err != nil {
			t.Fatalf("failed at creating webhook controller: %v", err)
		}

		err = wc.rebuildMutatingWebhookConfig()
		if err != nil {
			t.Fatalf("failed to rebuild MutatingWebhookConfiguration: %v", err)
		}

		config := v1beta1.MutatingWebhookConfiguration{}
		rb, err := ioutil.ReadFile("./test-data/example-mutating-webhook-config.yaml")
		if err != nil {
			t.Fatalf("error reading example mutating webhook config: %v ", err)
		}
		_, _, err = deserializer.Decode(rb, nil, &config)
		if err != nil || len(config.Webhooks) != 1 {
			t.Fatalf("failed to decode example MutatingWebhookConfiguration: %v", err)
		}
		rb, err = ioutil.ReadFile("./test-data/example-ca-cert.pem")
		if err != nil {
			t.Fatalf("error reading example certificate file: %v ", err)
		}
		config.Webhooks[0].ClientConfig.CABundle = rb
		if !reflect.DeepEqual(&config, wc.mutatingWebhookConfig) {
			t.Errorf("the MutatingWebhookConfiguration is unexpected,"+
				"expected: %v, actual: %v", config, wc.mutatingWebhookConfig)
		}
	}
}

func TestRebuildValidatingWebhookConfig(t *testing.T) {
	mutatingWebhookConfigFiles := []string{"./test-data/example-mutating-webhook-config.yaml"}
	mutatingWebhookConfigNames := []string{"protomutate"}
	validatingWebhookServiceNames := []string{"foo"}
	validatingWebhookConfigFiles := []string{"./test-data/example-validating-webhook-config.yaml"}
	validatingWebhookConfigNames := []string{"protovalidate"}

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
	}{
		"rebuildMutatingWebhookConfig should succeed": {
			deleteWebhookConfigOnExit:     false,
			gracePeriodRatio:              0.6,
			k8sCaCertFile:                 "./test-data/example-ca-cert.pem",
			namespace:                     "foo.ns",
			mutatingWebhookConfigFiles:    mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:    mutatingWebhookConfigNames,
			validatingWebhookConfigFiles:  validatingWebhookConfigFiles,
			validatingWebhookConfigNames:  validatingWebhookConfigNames,
			validatingWebhookServiceNames: validatingWebhookServiceNames,
		},
	}

	client := fake.NewSimpleClientset()

	for _, tc := range testCases {
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookSerivceNames, tc.mutatingWebhookSerivcePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if wc != nil && wc.K8sCaCertWatcher != nil {
			defer wc.K8sCaCertWatcher.Close()
		}
		if wc != nil && wc.MutatingWebhookFileWatcher != nil {
			defer wc.MutatingWebhookFileWatcher.Close()
		}
		if wc != nil && wc.ValidatingWebhookFileWatcher != nil {
			defer wc.ValidatingWebhookFileWatcher.Close()
		}
		if err != nil {
			t.Fatalf("failed at creating webhook controller: %v", err)
		}

		err = wc.rebuildValidatingWebhookConfig()
		if err != nil {
			t.Fatalf("failed to rebuild ValidatingWebhookConfiguration: %v", err)
		}

		config := v1beta1.ValidatingWebhookConfiguration{}
		rb, err := ioutil.ReadFile("./test-data/example-validating-webhook-config.yaml")
		if err != nil {
			t.Fatalf("error reading example validating webhook config: %v ", err)
		}
		_, _, err = deserializer.Decode(rb, nil, &config)
		if err != nil || len(config.Webhooks) != 1 {
			t.Fatalf("failed to decode example ValidatingWebhookConfiguration: %v", err)
		}
		rb, err = ioutil.ReadFile("./test-data/example-ca-cert.pem")
		if err != nil {
			t.Fatalf("error reading example certificate file: %v ", err)
		}
		config.Webhooks[0].ClientConfig.CABundle = rb
		if !reflect.DeepEqual(&config, wc.validatingWebhookConfig) {
			t.Errorf("the ValidaingWebhookConfiguration is unexpected,"+
				"expected: %v, actual: %v", config, wc.validatingWebhookConfig)
		}
	}
}

func TestGetCACert(t *testing.T) {
	validatingWebhookConfigFiles := []string{"./test-data/example-validating-webhook-config.yaml"}
	mutatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	mutatingWebhookConfigNames := []string{"mock-mutating-webook"}
	validatingWebhookConfigNames := []string{"mock-validating-webhook"}
	mutatingWebhookServiceNames := []string{"foo"}

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
		expectFail                    bool
	}{
		"getCACert should succeed for a valid certificate": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			namespace:                    "foo.ns",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			mutatingWebhookSerivceNames:  mutatingWebhookServiceNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			expectFail:                   false,
		},
	}

	client := fake.NewSimpleClientset()

	for _, tc := range testCases {
		// If the CA cert. is invalid, NewWebhookController will fail.
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookSerivceNames, tc.mutatingWebhookSerivcePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if wc != nil && wc.K8sCaCertWatcher != nil {
			defer wc.K8sCaCertWatcher.Close()
		}
		if wc != nil && wc.MutatingWebhookFileWatcher != nil {
			defer wc.MutatingWebhookFileWatcher.Close()
		}
		if wc != nil && wc.ValidatingWebhookFileWatcher != nil {
			defer wc.ValidatingWebhookFileWatcher.Close()
		}
		if err != nil {
			t.Fatalf("failed at creating webhook controller: %v", err)
		}

		cert, err := wc.getCACert()
		if !tc.expectFail {
			if err != nil {
				t.Errorf("failed to get CA cert: %v", err)
			} else if !bytes.Equal(cert, []byte(exampleCACert1)) {
				t.Errorf("the CA certificate read does not match the actual certificate")
			}
		} else if err == nil {
			t.Error("expect failure on getting CA cert but succeeded")
		}
	}
}

func TestUpsertSecret(t *testing.T) {
	validatingWebhookConfigFiles := []string{"./test-data/example-validating-webhook-config.yaml"}
	mutatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	mutatingWebhookConfigNames := []string{"mock-mutating-webook"}
	validatingWebhookConfigNames := []string{"mock-validating-webhook"}
	mutatingWebhookServiceNames := []string{"foo"}

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
		scrtName                      string
		expectFaill                   bool
	}{
		"upsert a valid secret name should succeed": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			namespace:                    "foo.ns",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			mutatingWebhookSerivceNames:  mutatingWebhookServiceNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			scrtName:                     "istio.webhook.foo",
			expectFaill:                  false,
		},
		"upsert an invalid secret name should fail": {
			deleteWebhookConfigOnExit:    false,
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			namespace:                    "bar.ns",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookConfigNames:   mutatingWebhookConfigNames,
			mutatingWebhookSerivceNames:  mutatingWebhookServiceNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			validatingWebhookConfigNames: validatingWebhookConfigNames,
			scrtName:                     "istio.webhook.bar",
			expectFaill:                  true,
		},
	}

	client := fake.NewSimpleClientset()
	csr := &cert.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: "domain-cluster.local-ns--secret-mock-secret",
		},
		Status: cert.CertificateSigningRequestStatus{
			Certificate: []byte(exampleIssuedCert),
		},
	}
	client.PrependReactor("get", "certificatesigningrequests", defaultReactionFunc(csr))

	for _, tc := range testCases {
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookSerivceNames, tc.mutatingWebhookSerivcePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if wc != nil && wc.K8sCaCertWatcher != nil {
			defer wc.K8sCaCertWatcher.Close()
		}
		if wc != nil && wc.MutatingWebhookFileWatcher != nil {
			defer wc.MutatingWebhookFileWatcher.Close()
		}
		if wc != nil && wc.ValidatingWebhookFileWatcher != nil {
			defer wc.ValidatingWebhookFileWatcher.Close()
		}
		if err != nil {
			t.Errorf("failed at creating webhook controller: %v", err)
			continue
		}

		err = wc.upsertSecret(tc.scrtName, tc.namespace)
		if tc.expectFaill {
			if err == nil {
				t.Errorf("should have failed at upsertSecret")
			}
			continue
		} else if err != nil {
			t.Errorf("should not failed at upsertSecret, err: %v", err)
		}
	}
}

func TestGetServiceName(t *testing.T) {
	client := fake.NewSimpleClientset()
	mutatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	validatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	mutatingWebhookServiceNames := []string{"foo", "bar"}
	validatingWebhookServiceNames := []string{"baz"}

	testCases := map[string]struct {
		deleteWebhookConfigOnExit     bool
		gracePeriodRatio              float32
		minGracePeriod                time.Duration
		k8sCaCertFile                 string
		namespace                     string
		mutatingWebhookConfigFiles    []string
		mutatingWebhookConfigNames    []string
		mutatingWebhookServiceNames   []string
		mutatingWebhookServicePorts   []int
		validatingWebhookConfigFiles  []string
		validatingWebhookConfigNames  []string
		validatingWebhookServiceNames []string
		validatingWebhookServicePorts []int
		scrtName                      string
		expectFound                   bool
		expectedSvcName               string
	}{
		"a mutating webhook service corresponding to a secret exists": {
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookServiceNames:  mutatingWebhookServiceNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			scrtName:                     "istio.webhook.foo",
			expectFound:                  true,
			expectedSvcName:              "foo",
		},
		"a mutating service corresponding to a secret does not exists": {
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			mutatingWebhookServiceNames:  mutatingWebhookServiceNames,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			scrtName:                     "istio.webhook.baz",
			expectFound:                  false,
			expectedSvcName:              "foo",
		},
		"a validating webhook service corresponding to a secret exists": {
			gracePeriodRatio:              0.6,
			k8sCaCertFile:                 "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:    mutatingWebhookConfigFiles,
			mutatingWebhookServiceNames:   mutatingWebhookServiceNames,
			validatingWebhookConfigFiles:  validatingWebhookConfigFiles,
			validatingWebhookServiceNames: validatingWebhookServiceNames,
			scrtName:                      "istio.webhook.baz",
			expectFound:                   true,
			expectedSvcName:               "baz",
		},
		"a validating webhook service corresponding to a secret does not exists": {
			gracePeriodRatio:              0.6,
			k8sCaCertFile:                 "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:    mutatingWebhookConfigFiles,
			mutatingWebhookServiceNames:   mutatingWebhookServiceNames,
			validatingWebhookConfigFiles:  validatingWebhookConfigFiles,
			validatingWebhookServiceNames: validatingWebhookServiceNames,
			scrtName:                      "istio.webhook.barr",
			expectFound:                   false,
			expectedSvcName:               "bar",
		},
	}

	for _, tc := range testCases {
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookServiceNames, tc.mutatingWebhookServicePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if wc != nil && wc.K8sCaCertWatcher != nil {
			defer wc.K8sCaCertWatcher.Close()
		}
		if wc != nil && wc.MutatingWebhookFileWatcher != nil {
			defer wc.MutatingWebhookFileWatcher.Close()
		}
		if wc != nil && wc.ValidatingWebhookFileWatcher != nil {
			defer wc.ValidatingWebhookFileWatcher.Close()
		}
		if err != nil {
			t.Errorf("failed to create a webhook controller: %v", err)
		}

		ret, found := wc.getServiceName(tc.scrtName)
		if tc.expectFound != found {
			t.Errorf("expected found (%v) differs from the actual found (%v)", tc.expectFound, found)
			continue
		}
		if found && tc.expectedSvcName != ret {
			t.Errorf("the service name (%v) returned is not as expcted (%v)", ret, tc.expectedSvcName)
		}
	}
}

func TestGetWebhookSecretNameFromSvcname(t *testing.T) {
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
		mutatingWebhookServiceNames   []string
		mutatingWebhookServicePorts   []int
		validatingWebhookConfigFiles  []string
		validatingWebhookConfigNames  []string
		validatingWebhookServiceNames []string
		validatingWebhookServicePorts []int
		svcName                       string
		expectedScrtName              string
	}{
		"expected secret name matches the return": {
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			svcName:                      "foo",
			expectedScrtName:             "istio.webhook.foo",
		},
	}

	for _, tc := range testCases {
		wc, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookServiceNames, tc.mutatingWebhookServicePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if wc != nil && wc.K8sCaCertWatcher != nil {
			defer wc.K8sCaCertWatcher.Close()
		}
		if wc != nil && wc.MutatingWebhookFileWatcher != nil {
			defer wc.MutatingWebhookFileWatcher.Close()
		}
		if wc != nil && wc.ValidatingWebhookFileWatcher != nil {
			defer wc.ValidatingWebhookFileWatcher.Close()
		}
		if err != nil {
			t.Errorf("failed to create a webhook controller: %v", err)
		}

		ret := wc.getWebhookSecretNameFromSvcname(tc.svcName)
		if tc.expectedScrtName != ret {
			t.Errorf("the secret name (%v) returned is not as expcted (%v)", ret, tc.expectedScrtName)
		}
	}
}
