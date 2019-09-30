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

package cmd

import (
	"bytes"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
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

	for _, tc := range testCases {
		err := checkCertificate([]byte(tc.cert))
		if tc.shouldFail {
			if err == nil {
				t.Errorf("should have failed")
			}
		} else if err != nil {
			t.Errorf("should not fail, but err: %v", err)
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

	for _, tc := range testCases {
		err := veriyCertChain([]byte(tc.cert), []byte(tc.caCert))
		if tc.shouldFail {
			if err == nil {
				t.Errorf("should have failed")
			}
		} else if err != nil {
			t.Errorf("should not fail, but err: %v", err)
		}
	}
}

func TestBuildValidatingWebhookConfig(t *testing.T) {
	configFile := "./testdata/webhook/example-validating-webhook-config.yaml"
	configName := "proto-validate"
	webhookConfig, err := buildValidatingWebhookConfig(fakeCACert, configFile, configName)
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

	for _, tc := range testCases {
		err := tc.opt.Validate()
		if tc.shouldFail {
			if err == nil {
				t.Errorf("should have failed")
			}
		} else if err != nil {
			t.Errorf("should not fail, but err: %v", err)
		}
	}
}

func TestDisableWebhookConfig(t *testing.T) {
	testCases := map[string]struct {
		opt                           disableCliOptions
		createValidatingWebhookConfig bool
		createMutatingWebhookConfig   bool
		shouldFail                    bool
	}{
		"disable validating webhook config": {
			opt: disableCliOptions{
				disableValidationWebhook:    true,
				validatingWebhookConfigName: "foo",
			},
			createValidatingWebhookConfig: true,
			createMutatingWebhookConfig:   true,
			shouldFail:                    false,
		},
		"disable mutating webhook config": {
			opt: disableCliOptions{
				disableInjectionWebhook:   true,
				mutatingWebhookConfigName: "bar",
			},
			createValidatingWebhookConfig: true,
			createMutatingWebhookConfig:   true,
			shouldFail:                    false,
		},
		"disable validating and mutating webhook configs": {
			opt: disableCliOptions{
				disableValidationWebhook:    true,
				validatingWebhookConfigName: "foo",
				disableInjectionWebhook:     true,
				mutatingWebhookConfigName:   "bar",
			},
			createValidatingWebhookConfig: true,
			createMutatingWebhookConfig:   true,
			shouldFail:                    false,
		},
		"disable non-existing validating webhook config": {
			opt: disableCliOptions{
				disableValidationWebhook:    true,
				validatingWebhookConfigName: "foo",
			},
			createValidatingWebhookConfig: false,
			createMutatingWebhookConfig:   false,
			shouldFail:                    true,
		},
		"disable non-existing mutating webhook config": {
			opt: disableCliOptions{
				disableInjectionWebhook:   true,
				mutatingWebhookConfigName: "bar",
			},
			createValidatingWebhookConfig: false,
			createMutatingWebhookConfig:   false,
			shouldFail:                    true,
		},
	}

	for _, tc := range testCases {
		client := fake.NewSimpleClientset()
		if tc.createValidatingWebhookConfig {
			webhookConfig, err := buildValidatingWebhookConfig(
				[]byte(exampleCACert),
				"./testdata/webhook/example-validating-webhook-config.yaml",
				tc.opt.validatingWebhookConfigName,
			)
			if err != nil {
				t.Fatalf("err when build validatingwebhookconfiguration: %v", err)
			}
			_, err = createValidatingWebhookConfig(client, webhookConfig)
			if err != nil {
				t.Fatalf("error when creating validatingwebhookconfiguration: %v", err)
			}
		}
		if tc.createMutatingWebhookConfig {
			webhookConfig, err := buildMutatingWebhookConfig(
				[]byte(exampleCACert),
				"./testdata/webhook/example-mutating-webhook-config.yaml",
				tc.opt.mutatingWebhookConfigName,
			)
			if err != nil {
				t.Fatalf("err when build mutatingwebhookconfiguration: %v", err)
			}
			_, err = createMutatingWebhookConfig(client, webhookConfig)
			if err != nil {
				t.Fatalf("error when creating mutatingwebhookconfiguration: %v", err)
			}
		}

		err := disableWebhookConfig(client, &tc.opt)
		if tc.shouldFail {
			if err == nil {
				t.Errorf("should have failed")
			}
		} else if err != nil {
			t.Errorf("should not fail, but err: %v", err)
		}
	}
}
