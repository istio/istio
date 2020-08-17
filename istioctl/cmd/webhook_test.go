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

package cmd

import (
	"bytes"
	"context"
	"testing"

	"k8s.io/api/admissionregistration/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/security/pkg/pki/ca"
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

func TestCheckCertificate(t *testing.T) {
	testCases := map[string]struct {
		cert       string
		shouldFail bool
	}{
		"cert valid": {
			cert:       exampleCACert,
			shouldFail: false,
		},
		"cert invalid": {
			cert:       "invalid-cert",
			shouldFail: true,
		},
	}

	for tcName, tc := range testCases {
		err := checkCertificate([]byte(tc.cert))
		if tc.shouldFail {
			if err == nil {
				t.Errorf("%v: should have failed", tcName)
			}
		} else if err != nil {
			t.Errorf("%v: should not fail, but err: %v", tcName, err)
		}
	}
}

func TestVerifyCertChain(t *testing.T) {
	testCases := map[string]struct {
		cert       string
		caCert     string
		shouldFail bool
	}{
		"cert chain valid": {
			cert:       exampleIssuedCert,
			caCert:     exampleCACert,
			shouldFail: false,
		},
		"cert chain invalid": {
			cert:       exampleCACert,
			caCert:     exampleIssuedCert,
			shouldFail: true,
		},
	}

	for tcName, tc := range testCases {
		err := veriyCertChain([]byte(tc.cert), []byte(tc.caCert))
		if tc.shouldFail {
			if err == nil {
				t.Errorf("%v: should have failed", tcName)
			}
		} else if err != nil {
			t.Errorf("%v: should not fail, but err: %v", tcName, err)
		}
	}
}

func TestEnableCliOptionsValidation(t *testing.T) {
	testCases := map[string]struct {
		opt        enableCliOptions
		shouldFail bool
	}{
		"valid option 1": {
			opt: enableCliOptions{
				enableValidationWebhook:      true,
				validationWebhookConfigPath:  "config",
				validatingWebhookServiceName: "service",
				webhookSecretName:            "secret",
				webhookSecretNameSpace:       "secret-ns",
			},
			shouldFail: false,
		},
		"valid option 2": {
			opt: enableCliOptions{
				enableMutationWebhook:      true,
				mutatingWebhookConfigPath:  "config",
				mutatingWebhookServiceName: "service",
				webhookSecretName:          "secret",
				webhookSecretNameSpace:     "secret-ns",
			},
			shouldFail: false,
		},
		"valid option 3": {
			opt: enableCliOptions{
				enableValidationWebhook:      true,
				validationWebhookConfigPath:  "config",
				validatingWebhookServiceName: "service",
				enableMutationWebhook:        true,
				mutatingWebhookConfigPath:    "config",
				mutatingWebhookServiceName:   "service",
				webhookSecretName:            "secret",
				webhookSecretNameSpace:       "secret-ns",
			},
			shouldFail: false,
		},
		"invalid option 1": {
			opt: enableCliOptions{
				enableValidationWebhook:      true,
				validatingWebhookServiceName: "service",
				webhookSecretName:            "secret",
				webhookSecretNameSpace:       "secret-ns",
			},
			shouldFail: true,
		},
		"invalid option 2": {
			opt: enableCliOptions{
				enableValidationWebhook:     true,
				validationWebhookConfigPath: "config",
				webhookSecretName:           "secret",
				webhookSecretNameSpace:      "secret-ns",
			},
			shouldFail: true,
		},
		"invalid option 3": {
			opt: enableCliOptions{
				enableMutationWebhook:      true,
				mutatingWebhookServiceName: "service",
				webhookSecretName:          "secret",
				webhookSecretNameSpace:     "secret-ns",
			},
			shouldFail: true,
		},
		"invalid option 4": {
			opt: enableCliOptions{
				enableMutationWebhook:     true,
				mutatingWebhookConfigPath: "config",
				webhookSecretName:         "secret",
				webhookSecretNameSpace:    "secret-ns",
			},
			shouldFail: true,
		},
		"invalid option 5": {
			opt: enableCliOptions{
				enableValidationWebhook: false,
				enableMutationWebhook:   false,
			},
			shouldFail: true,
		},
		"invalid option 6": { // webhook secret namespace is required but missing
			opt: enableCliOptions{
				enableValidationWebhook:      true,
				validationWebhookConfigPath:  "config",
				validatingWebhookServiceName: "service",
				webhookSecretName:            "secret",
			},
			shouldFail: true,
		},
	}

	for tcName, tc := range testCases {
		err := tc.opt.Validate()
		if tc.shouldFail {
			if err == nil {
				t.Errorf("%v: opt (%v) should have failed", tcName, tc.opt)
			}
		} else if err != nil {
			t.Errorf("%v: opt (%v) should not fail, but err: %v", tcName, tc.opt, err)
		}
	}
}

func TestDisableCliOptionsValidation(t *testing.T) {
	testCases := map[string]struct {
		opt        disableCliOptions
		shouldFail bool
	}{
		"valid option 1": {
			opt: disableCliOptions{
				disableValidationWebhook:    true,
				validatingWebhookConfigName: "foo",
			},
			shouldFail: false,
		},
		"valid option 2": {
			opt: disableCliOptions{
				disableInjectionWebhook:   true,
				mutatingWebhookConfigName: "bar",
			},
			shouldFail: false,
		},
		"valid option 3": {
			opt: disableCliOptions{
				disableValidationWebhook:    true,
				validatingWebhookConfigName: "foo",
				disableInjectionWebhook:     true,
				mutatingWebhookConfigName:   "bar",
			},
			shouldFail: false,
		},
		"invalid option 1": {
			opt: disableCliOptions{
				disableValidationWebhook:    true,
				validatingWebhookConfigName: "",
			},
			shouldFail: true,
		},
		"invalid option 2": {
			opt: disableCliOptions{
				disableInjectionWebhook:   true,
				mutatingWebhookConfigName: "",
			},
			shouldFail: true,
		},
		"invalid option 3": {
			opt: disableCliOptions{
				disableValidationWebhook: false,
				disableInjectionWebhook:  false,
			},
			shouldFail: true,
		},
	}

	for tcName, tc := range testCases {
		err := tc.opt.Validate()
		if tc.shouldFail {
			if err == nil {
				t.Errorf("%v: opt (%v) should have failed", tcName, tc.opt)
			}
		} else if err != nil {
			t.Errorf("%v: opt (%v) should not fail, but err: %v", tcName, tc.opt, err)
		}
	}
}

func TestStatusCliOptionsValidation(t *testing.T) {
	testCases := map[string]struct {
		opt        statusCliOptions
		shouldFail bool
	}{
		"valid option 1": {
			opt: statusCliOptions{
				validationWebhook:           true,
				validatingWebhookConfigName: "foo",
			},
			shouldFail: false,
		},
		"valid option 2": {
			opt: statusCliOptions{
				injectionWebhook:          true,
				mutatingWebhookConfigName: "bar",
			},
			shouldFail: false,
		},
		"valid option 3": {
			opt: statusCliOptions{
				validationWebhook:           true,
				validatingWebhookConfigName: "foo",
				injectionWebhook:            true,
				mutatingWebhookConfigName:   "bar",
			},
			shouldFail: false,
		},
		"invalid option 1": {
			opt: statusCliOptions{
				validationWebhook:           true,
				validatingWebhookConfigName: "",
			},
			shouldFail: true,
		},
		"invalid option 2": {
			opt: statusCliOptions{
				injectionWebhook:          true,
				mutatingWebhookConfigName: "",
			},
			shouldFail: true,
		},
		"invalid option 3": {
			opt: statusCliOptions{
				validationWebhook: false,
				injectionWebhook:  false,
			},
			shouldFail: true,
		},
	}

	for tcName, tc := range testCases {
		err := tc.opt.Validate()
		if tc.shouldFail {
			if err == nil {
				t.Errorf("%v: opt (%v) should have failed", tcName, tc.opt)
			}
		} else if err != nil {
			t.Errorf("%v: opt (%v) should not fail, but err: %v", tcName, tc.opt, err)
		}
	}
}

func TestDisableWebhookConfig(t *testing.T) {
	var buf bytes.Buffer
	testCases := map[string]struct {
		opt                           disableCliOptions
		createValidatingWebhookConfig bool
		createMutatingWebhookConfig   bool
		shouldFail                    bool
	}{
		"disable validating webhook config": {
			opt: disableCliOptions{
				disableValidationWebhook:    true,
				validatingWebhookConfigName: "protovalidate",
			},
			createValidatingWebhookConfig: true,
			createMutatingWebhookConfig:   true,
			shouldFail:                    false,
		},
		"disable mutating webhook config": {
			opt: disableCliOptions{
				disableInjectionWebhook:   true,
				mutatingWebhookConfigName: "protomutate",
			},
			createValidatingWebhookConfig: true,
			createMutatingWebhookConfig:   true,
			shouldFail:                    false,
		},
		"disable validating and mutating webhook configs": {
			opt: disableCliOptions{
				disableValidationWebhook:    true,
				validatingWebhookConfigName: "protovalidate",
				disableInjectionWebhook:     true,
				mutatingWebhookConfigName:   "protomutate",
			},
			createValidatingWebhookConfig: true,
			createMutatingWebhookConfig:   true,
			shouldFail:                    false,
		},
		"disable non-existing validating webhook config": {
			opt: disableCliOptions{
				disableValidationWebhook:    true,
				validatingWebhookConfigName: "protovalidate",
			},
			createValidatingWebhookConfig: false,
			createMutatingWebhookConfig:   false,
			shouldFail:                    true,
		},
		"disable non-existing mutating webhook config": {
			opt: disableCliOptions{
				disableInjectionWebhook:   true,
				mutatingWebhookConfigName: "protomutate",
			},
			createValidatingWebhookConfig: false,
			createMutatingWebhookConfig:   false,
			shouldFail:                    true,
		},
	}

	for tcName, tc := range testCases {
		client := fake.NewSimpleClientset()
		if tc.createValidatingWebhookConfig {
			webhookConfig, err := buildValidatingWebhookConfig(
				[]byte(exampleCACert),
				"./testdata/webhook/example-validating-webhook-config.yaml")
			if err != nil {
				t.Fatalf("%v: err when build validatingwebhookconfiguration: %v", tcName, err)
			}
			_, err = createValidatingWebhookConfig(client, webhookConfig, &buf)
			if err != nil {
				t.Fatalf("%v: err when creating validatingwebhookconfiguration: %v", tcName, err)
			}
		}
		if tc.createMutatingWebhookConfig {
			webhookConfig, err := buildMutatingWebhookConfig(
				[]byte(exampleCACert),
				"./testdata/webhook/example-mutating-webhook-config.yaml")
			if err != nil {
				t.Fatalf("%v: err when build mutatingwebhookconfiguration: %v", tcName, err)
			}
			_, err = createMutatingWebhookConfig(client, webhookConfig, &buf)
			if err != nil {
				t.Fatalf("%v: error when creating mutatingwebhookconfiguration: %v", tcName, err)
			}
		}

		validationErr, injectionErr := disableWebhookConfig(client, &tc.opt)
		if tc.shouldFail {
			if validationErr == nil && injectionErr == nil {
				t.Errorf("%v: should have failed", tcName)
			}
		} else {
			if validationErr != nil {
				t.Errorf("%v: should not fail, but err when disabling validation webhook: %v", tcName, validationErr)
			}
			if injectionErr != nil {
				t.Errorf("%v: should not fail, but err when disabling injection webhook: %v", tcName, injectionErr)
			}
		}
	}
}

func TestBuildValidatingWebhookConfig(t *testing.T) {
	configFile := "./testdata/webhook/example-validating-webhook-config.yaml"
	configName := "protovalidate"
	webhookConfig, err := buildValidatingWebhookConfig(fakeCACert, configFile)
	if err != nil {
		t.Fatalf("err building validating webhook config %v: %v", configName, err)
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

func TestBuildMutatingWebhookConfig(t *testing.T) {
	configFile := "./testdata/webhook/example-mutating-webhook-config.yaml"
	configName := "protomutate"
	webhookConfig, err := buildMutatingWebhookConfig(fakeCACert, configFile)
	if err != nil {
		t.Fatalf("err building mutating webhook config %v: %v", configName, err)
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

func TestCreateValidatingWebhookConfig(t *testing.T) {
	var buf bytes.Buffer
	testCases := map[string]struct {
		createWebhookConfig bool
		updateWebhookConfig bool
		shouldFail          bool
	}{
		"create a valid webhook config": {
			createWebhookConfig: true,
			updateWebhookConfig: false,
			shouldFail:          false,
		},
		"update a valid webhook config": {
			createWebhookConfig: true,
			updateWebhookConfig: true,
			shouldFail:          false,
		},
	}

	for tcName, tc := range testCases {
		client := fake.NewSimpleClientset()
		var webhookConfig *v1beta1.ValidatingWebhookConfiguration
		var err error
		if tc.createWebhookConfig {
			webhookConfig, err = buildValidatingWebhookConfig(
				[]byte(exampleCACert),
				"./testdata/webhook/example-validating-webhook-config.yaml")
			if err != nil {
				t.Fatalf("%v: err when build ValidatingWebhookConfiguration: %v", tcName, err)
			}
			_, err = createValidatingWebhookConfig(client, webhookConfig, &buf)
			if err != nil {
				t.Fatalf("%v: error when creating ValidatingWebhookConfiguration: %v", tcName, err)
			}
			if len(webhookConfig.Webhooks) == 0 {
				t.Fatalf("%v: empty webhooks in the webhook configuration", tcName)
			}
		}
		if tc.updateWebhookConfig {
			webhookConfig.Webhooks[0].ClientConfig.CABundle = []byte("new-ca-bundle")
			updated, err := createValidatingWebhookConfig(client, webhookConfig, &buf)
			if err != nil {
				t.Fatalf("%v: error when updating ValidatingWebhookConfiguration: %v", tcName, err)
			}
			if len(updated.Webhooks) == 0 {
				t.Fatalf("%v: empty webhooks in the updated webhook configuration", tcName)
			}
			if string(updated.Webhooks[0].ClientConfig.CABundle) != "new-ca-bundle" {
				t.Fatalf("%v: the updated CA bundle does not match new-ca-bundle", tcName)
			}
		}
	}
}

func TestCreateMutatingWebhookConfig(t *testing.T) {
	var buf bytes.Buffer
	testCases := map[string]struct {
		createWebhookConfig bool
		updateWebhookConfig bool
		shouldFail          bool
	}{
		"create a valid webhook config": {
			createWebhookConfig: true,
			updateWebhookConfig: false,
			shouldFail:          false,
		},
		"update a valid webhook config": {
			createWebhookConfig: true,
			updateWebhookConfig: true,
			shouldFail:          false,
		},
	}

	for tcName, tc := range testCases {
		client := fake.NewSimpleClientset()
		var webhookConfig *v1beta1.MutatingWebhookConfiguration
		var err error
		if tc.createWebhookConfig {
			webhookConfig, err = buildMutatingWebhookConfig(
				[]byte(exampleCACert),
				"./testdata/webhook/example-mutating-webhook-config.yaml")
			if err != nil {
				t.Fatalf("%v: err when build MutatingWebhookConfiguration: %v", tcName, err)
			}
			_, err = createMutatingWebhookConfig(client, webhookConfig, &buf)
			if err != nil {
				t.Fatalf("%v: error when creating MutatingWebhookConfiguration: %v", tcName, err)
			}
			if len(webhookConfig.Webhooks) == 0 {
				t.Fatalf("%v: empty webhooks in the webhook configuration", tcName)
			}
		}
		if tc.updateWebhookConfig {
			webhookConfig.Webhooks[0].ClientConfig.CABundle = []byte("new-ca-bundle")
			updated, err := createMutatingWebhookConfig(client, webhookConfig, &buf)
			if err != nil {
				t.Fatalf("%v: error when updating MutatingWebhookConfiguration: %v", tcName, err)
			}
			if len(updated.Webhooks) == 0 {
				t.Fatalf("%v: empty webhooks in the updated webhook configuration", tcName)
			}
			if string(updated.Webhooks[0].ClientConfig.CABundle) != "new-ca-bundle" {
				t.Fatalf("%v: the updated CA bundle does not match new-ca-bundle", tcName)
			}
		}
	}
}

func TestDisplayValidationWebhookConfig(t *testing.T) {
	var buf bytes.Buffer
	testCases := map[string]struct {
		opt                           statusCliOptions
		createValidatingWebhookConfig bool
		shouldFail                    bool
	}{
		"display validating webhook config": {
			opt: statusCliOptions{
				validationWebhook:           true,
				validatingWebhookConfigName: "protovalidate",
			},
			createValidatingWebhookConfig: true,
			shouldFail:                    false,
		},
		"display non-existing validating webhook config": {
			opt: statusCliOptions{
				validationWebhook:           true,
				validatingWebhookConfigName: "protovalidate",
			},
			createValidatingWebhookConfig: false,
			shouldFail:                    true,
		},
	}

	for tcName, tc := range testCases {
		client := fake.NewSimpleClientset()
		if tc.createValidatingWebhookConfig {
			webhookConfig, err := buildValidatingWebhookConfig(
				[]byte(exampleCACert),
				"./testdata/webhook/example-validating-webhook-config.yaml")
			if err != nil {
				t.Fatalf("%v: err when build validatingwebhookconfiguration: %v", tcName, err)
			}
			_, err = createValidatingWebhookConfig(client, webhookConfig, &buf)
			if err != nil {
				t.Fatalf("%v: error when creating validatingwebhookconfiguration: %v", tcName, err)
			}
		}

		validationErr := displayValidationWebhookConfig(client, &tc.opt, &buf)
		if tc.shouldFail {
			if validationErr == nil {
				t.Errorf("%v: should have failed", tcName)
			}
		} else {
			if validationErr != nil {
				t.Errorf("%v: should not fail, but err when displaying validation webhook: %v", tcName, validationErr)
			}
		}
	}
}

func TestDisplayMutationWebhookConfig(t *testing.T) {
	var buf bytes.Buffer
	testCases := map[string]struct {
		opt                         statusCliOptions
		createMutatingWebhookConfig bool
		shouldFail                  bool
	}{
		"disable mutating webhook config": {
			opt: statusCliOptions{
				injectionWebhook:          true,
				mutatingWebhookConfigName: "protomutate",
			},
			createMutatingWebhookConfig: true,
			shouldFail:                  false,
		},
		"disable non-existing mutating webhook config": {
			opt: statusCliOptions{
				injectionWebhook:          true,
				mutatingWebhookConfigName: "protomutate",
			},
			createMutatingWebhookConfig: false,
			shouldFail:                  true,
		},
	}

	for tcName, tc := range testCases {
		client := fake.NewSimpleClientset()

		if tc.createMutatingWebhookConfig {
			webhookConfig, err := buildMutatingWebhookConfig(
				[]byte(exampleCACert),
				"./testdata/webhook/example-mutating-webhook-config.yaml")
			if err != nil {
				t.Fatalf("%v: err when build mutatingwebhookconfiguration: %v", tcName, err)
			}
			_, err = createMutatingWebhookConfig(client, webhookConfig, &buf)
			if err != nil {
				t.Fatalf("%v: error when creating mutatingwebhookconfiguration: %v", tcName, err)
			}
		}

		injectionErr := displayMutationWebhookConfig(client, &tc.opt, &buf)
		if tc.shouldFail {
			if injectionErr == nil {
				t.Errorf("%v: should have failed", tcName)
			}
		} else {
			if injectionErr != nil {
				t.Errorf("%v: should not fail, but err when displaying injection webhook: %v", tcName, injectionErr)
			}
		}
	}
}

func TestDisplayWebhookConfig(t *testing.T) {
	var buf bytes.Buffer
	testCases := map[string]struct {
		opt                           statusCliOptions
		createValidatingWebhookConfig bool
		createMutatingWebhookConfig   bool
		shouldFail                    bool
	}{
		"display validating webhook config": {
			opt: statusCliOptions{
				validationWebhook:           true,
				validatingWebhookConfigName: "protovalidate",
			},
			createValidatingWebhookConfig: true,
			createMutatingWebhookConfig:   true,
			shouldFail:                    false,
		},
		"display mutating webhook config": {
			opt: statusCliOptions{
				injectionWebhook:          true,
				mutatingWebhookConfigName: "protomutate",
			},
			createValidatingWebhookConfig: true,
			createMutatingWebhookConfig:   true,
			shouldFail:                    false,
		},
		"display validating and mutating webhook configs": {
			opt: statusCliOptions{
				validationWebhook:           true,
				validatingWebhookConfigName: "protovalidate",
				injectionWebhook:            true,
				mutatingWebhookConfigName:   "protomutate",
			},
			createValidatingWebhookConfig: true,
			createMutatingWebhookConfig:   true,
			shouldFail:                    false,
		},
		"display non-existing validating webhook config": {
			opt: statusCliOptions{
				validationWebhook:           true,
				validatingWebhookConfigName: "protovalidate",
			},
			createValidatingWebhookConfig: false,
			createMutatingWebhookConfig:   false,
			shouldFail:                    true,
		},
		"display non-existing mutating webhook config": {
			opt: statusCliOptions{
				injectionWebhook:          true,
				mutatingWebhookConfigName: "protomutate",
			},
			createValidatingWebhookConfig: false,
			createMutatingWebhookConfig:   false,
			shouldFail:                    true,
		},
	}

	for tcName, tc := range testCases {
		client := fake.NewSimpleClientset()
		if tc.createValidatingWebhookConfig {
			webhookConfig, err := buildValidatingWebhookConfig(
				[]byte(exampleCACert),
				"./testdata/webhook/example-validating-webhook-config.yaml")
			if err != nil {
				t.Fatalf("%v: err when build validatingwebhookconfiguration: %v", tcName, err)
			}
			_, err = createValidatingWebhookConfig(client, webhookConfig, &buf)
			if err != nil {
				t.Fatalf("%v: error when creating validatingwebhookconfiguration: %v", tcName, err)
			}
		}
		if tc.createMutatingWebhookConfig {
			webhookConfig, err := buildMutatingWebhookConfig(
				[]byte(exampleCACert),
				"./testdata/webhook/example-mutating-webhook-config.yaml")
			if err != nil {
				t.Fatalf("%v: err when build mutatingwebhookconfiguration: %v", tcName, err)
			}
			_, err = createMutatingWebhookConfig(client, webhookConfig, &buf)
			if err != nil {
				t.Fatalf("%v: error when creating mutatingwebhookconfiguration: %v", tcName, err)
			}
		}

		validationErr, injectionErr := displayWebhookConfig(client, &tc.opt, &buf)
		if tc.shouldFail {
			if validationErr == nil && injectionErr == nil {
				t.Errorf("%v: should have failed", tcName)
			}
		} else {
			if validationErr != nil {
				t.Errorf("%v: should not fail, but err when displaying validation webhook: %v", tcName, validationErr)
			}
			if injectionErr != nil {
				t.Errorf("%v: should not fail, but err when displaying injection webhook: %v", tcName, injectionErr)
			}
		}
	}
}

func TestReadCertFromSecret(t *testing.T) {
	testCases := map[string]struct {
		createSecret bool
		shouldFail   bool
	}{
		"read a valid secret": {
			createSecret: true,
			shouldFail:   false,
		},
		"read an invalid secret": {
			createSecret: false,
			shouldFail:   true,
		},
	}

	for tcName, tc := range testCases {
		client := fake.NewSimpleClientset()

		if tc.createSecret {
			secret := &v1.Secret{
				Data: map[string][]byte{},
				ObjectMeta: metav1.ObjectMeta{
					Annotations: nil,
					Name:        "foo",
					Namespace:   "bar",
				},
				Type: "test-secret",
			}
			secret.Data = map[string][]byte{
				ca.CertChainID:  []byte("dummy-cert"),
				ca.PrivateKeyID: []byte("dummy-key"),
				ca.RootCertID:   []byte("dummy-root"),
			}
			_, err := client.CoreV1().Secrets("bar").Create(context.TODO(), secret, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("%v: error when creating secret foo: %v", tcName, err)
			}
		}
		cert, err := readCertFromSecret(client, "foo", "bar")

		if tc.shouldFail {
			if err == nil {
				t.Errorf("%v: should have failed", tcName)
			}
		} else {
			if err != nil {
				t.Errorf("%v: should not fail, but err: %v", tcName, err)
			}
			if len(cert) == 0 {
				t.Errorf("%v: should not fail, but read returns empty cert", tcName)
			}
		}
	}
}

func TestReadCACertFromSA(t *testing.T) {
	testCases := map[string]struct {
		create     bool
		shouldFail bool
	}{
		"read a valid service account": {
			create:     true,
			shouldFail: false,
		},
		"read an invalid service account": {
			create:     false,
			shouldFail: true,
		},
	}

	for tcName, tc := range testCases {
		client := fake.NewSimpleClientset()
		if tc.create {
			secret := &v1.Secret{
				Data: map[string][]byte{},
				ObjectMeta: metav1.ObjectMeta{
					Annotations: nil,
					Name:        "foo",
					Namespace:   "bar",
				},
				Type: "test-secret",
			}
			secret.Data = map[string][]byte{
				ca.CertChainID:   []byte("dummy-cert"),
				ca.PrivateKeyID:  []byte("dummy-key"),
				ca.RootCertID:    []byte("dummy-root"),
				caKeyInK8sSecret: []byte("dummy-cert"),
			}
			_, err := client.CoreV1().Secrets("bar").Create(context.TODO(), secret, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("%v: error when creating secret foo: %v", tcName, err)
			}
			sa := &v1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: nil,
					Name:        "foo",
					Namespace:   "bar",
				},
			}
			sa.Secrets = append(sa.Secrets, v1.ObjectReference{
				Name:      "foo",
				Namespace: "bar",
			})
			_, err = client.CoreV1().ServiceAccounts("bar").Create(context.TODO(), sa, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("%v: error when creating service account foo: %v", tcName, err)
			}
		}

		cert, err := readCACertFromSA(client, "bar", "foo")
		if tc.shouldFail {
			if err == nil {
				t.Errorf("%v: should have failed", tcName)
			}
		} else {
			if err != nil {
				t.Errorf("%v: should not fail, but err: %v", tcName, err)
			}
			if len(cert) == 0 {
				t.Errorf("%v: should not fail, but read returns empty cert", tcName)
			}
		}
	}
}
