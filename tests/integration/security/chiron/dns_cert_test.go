//go:build integ
// +build integ

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

package chiron_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/tests/integration/security/util/secret"
)

const (
	// Specifies how long we wait before a secret becomes existent.
	secretWaitTime            = 20 * time.Second
	galleySecretName          = "dns.istio-galley-service-account"
	galleyDNSName             = "istio-galley.istio-system.svc"
	sidecarInjectorSecretName = "dns.istio-sidecar-injector-service-account"
	sidecarInjectorDNSName    = "istio-sidecar-injector.istio-system.svc"

	// This example certificate can be generated through
	// the following command:
	// kubectl exec -it POD-NAME -n NAMESPACE -- cat /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
	caCertUpdated = `-----BEGIN CERTIFICATE-----
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
	// This example certificate can be generated through
	// the following command:
	// go run security/tools/generate_cert/main.go -host="istio-galley.istio-system.svc" \
	// --mode=signer -signer-priv=root.key -signer-cert=root.pem --duration="1s"
	certExpired = `-----BEGIN CERTIFICATE-----
MIIDoDCCAoigAwIBAgIQSSLgQiNvMz7M42865LvUADANBgkqhkiG9w0BAQsFADCB
lDELMAkGA1UEBhMCVVMxCzAJBgNVBAgMAkNBMREwDwYDVQQHDAhTYW4gSm9zZTEQ
MA4GA1UECgwHZXhhbXBsZTEVMBMGA1UECwwMZXhhbXBsZS11bml0MRgwFgYDVQQD
DA93d3cuZXhhbXBsZS5jb20xIjAgBgkqhkiG9w0BCQEWE2V4YW1wbGVAZXhhbXBs
ZS5jb20wHhcNMTkxMDI0MDA1NTUxWhcNMTkxMDI0MDA1NTUyWjATMREwDwYDVQQK
EwhKdWp1IG9yZzCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBALXVyyqb
RhYy7zEsDSM63DVLfm4Hn4ovxtDietHEHelMQ2HyTzvAht2puCwU1UnzQfFRSGus
eehfGfRU1YAhJ/cXxGcuWHuDN50LyeHmlzlrNiUPJLgk5s7CttCTsJvXAbk8biyR
4w2H1yxVZMr/MgAl6YISwpaIZzB6OtYVnZBaVkap30yOroHf/VN1KG5b/uxuhADB
z80WvPWDknImO2eCwx1DbLQGsqgTYj7en/1HHuAFVNP8Fmi5ObZRQQQ/nIHmmnBB
f5kw+UgEeyAk4PFZv1f63gYJcicnV6ceh16dwosTzUylj1Ee51+1RJRocHj84t49
YGa2OplpT11+lA0CAwEAAaNuMGwwDgYDVR0PAQH/BAQDAgWgMAwGA1UdEwEB/wQC
MAAwHwYDVR0jBBgwFoAUyMJRLCKni7pg7huoLoFz1SR2HhkwKwYDVR0RAQH/BCEw
H4IdaXN0aW8tZ2FsbGV5LmlzdGlvLXN5c3RlbS5zdmMwDQYJKoZIhvcNAQELBQAD
ggEBAC3FoOP/a3sjBy1MdnITct4eNVZSz6jJq5YoKJ/LzA+f7tGoi3i90S8CMWqK
uGpkyTmNN/Br7svcla5/JOibEfZu6dguc1+gSZFW0DFvFLYaAu07Ynkieg0J3IJn
3rScr3PalIb89b1ZV+rfAScIClzeDKXDMVuZJBHGJAWnqYGBMsb19ky5xBSfgDRQ
nN8Fp3ShN6oCpflmsQGFcTb4AXZvBMge3TcDeiKtft2qprU4RUVoUuuHuOEIYnmS
IHjez5U3ZQA92bh3NorzWHxYWz9+leI1yWUbwu/5Bivg9EIxJRytAklrbHXLIEuw
ksOPXgK63Oot7wxQOuG5BX1v1yQ=
-----END CERTIFICATE-----`
)

func TestDNSCertificate(t *testing.T) {
	framework.NewTest(t).
		Features("security.control-plane.k8s-certs.dns-certificate").
		Run(func(t framework.TestContext) {
			var galleySecret, galleySecret2, sidecarInjectorSecret, sidecarInjectorSecret2 *corev1.Secret
			istio.DefaultConfigOrFail(t, t)
			cluster := t.Clusters().Default()
			istioNs := inst.Settings().SystemNamespace

			// Test that DNS certificates have been generated.
			t.NewSubTest("generateDNSCertificates").
				Run(func(t framework.TestContext) {
					t.Log("check that DNS certificates have been generated ...")
					galleySecret = kube2.WaitForSecretToExistOrFail(t, cluster.Kube(), istioNs, galleySecretName, secretWaitTime)
					sidecarInjectorSecret = kube2.WaitForSecretToExistOrFail(t, cluster.Kube(), istioNs, sidecarInjectorSecretName, secretWaitTime)
					t.Log(`checking Galley DNS certificate is valid`)
					secret.ExamineDNSSecretOrFail(t, galleySecret, galleyDNSName)
					t.Log(`checking Sidecar Injector DNS certificate is valid`)
					secret.ExamineDNSSecretOrFail(t, sidecarInjectorSecret, sidecarInjectorDNSName)
				})

			// Test certificate regeneration: if a DNS certificate is deleted, Chiron will regenerate it.
			t.NewSubTest("regenerateDNSCertificates").
				Run(func(t framework.TestContext) {
					_ = deleteSecret(cluster.Kube(), istioNs, galleySecretName)
					_ = deleteSecret(cluster.Kube(), istioNs, sidecarInjectorSecretName)
					// Sleep 5 seconds for the certificate regeneration to take place.
					t.Log(`sleep 5 seconds for the certificate regeneration to take place ...`)
					time.Sleep(5 * time.Second)
					galleySecret = kube2.WaitForSecretToExistOrFail(t, cluster.Kube(), istioNs, galleySecretName, secretWaitTime)
					sidecarInjectorSecret = kube2.WaitForSecretToExistOrFail(t, cluster.Kube(), istioNs, sidecarInjectorSecretName, secretWaitTime)
					t.Log(`checking regenerated Galley DNS certificate is valid`)
					secret.ExamineDNSSecretOrFail(t, galleySecret, galleyDNSName)
					t.Log(`checking regenerated Sidecar Injector DNS certificate is valid`)
					secret.ExamineDNSSecretOrFail(t, sidecarInjectorSecret, sidecarInjectorDNSName)
				})

			// Test certificate rotation: when the CA certificate is updated, certificates will be rotated.
			t.NewSubTest("rotateDNSCertificatesWhenCAUpdated").
				Run(func(t framework.TestContext) {
					galleySecret.Data[ca.RootCertFile] = []byte(caCertUpdated)
					if _, err := cluster.Kube().CoreV1().Secrets(istioNs).Update(context.TODO(), galleySecret, metav1.UpdateOptions{}); err != nil {
						t.Fatalf("failed to update secret (%s:%s), error: %s", istioNs, galleySecret.Name, err)
					}
					// Sleep 5 seconds for the certificate rotation to take place.
					t.Log(`sleep 5 seconds for certificate rotation to take place ...`)
					time.Sleep(5 * time.Second)
					galleySecret2 = kube2.WaitForSecretToExistOrFail(t, cluster.Kube(), istioNs, galleySecretName, secretWaitTime)
					t.Log(`checking rotated Galley DNS certificate is valid`)
					secret.ExamineDNSSecretOrFail(t, galleySecret2, galleyDNSName)
					if bytes.Equal(galleySecret2.Data[ca.CertChainFile], galleySecret.Data[ca.CertChainFile]) {
						t.Errorf("the rotated cert should be different from the original cert (%v, %v)",
							string(galleySecret2.Data[ca.CertChainFile]), string(galleySecret.Data[ca.CertChainFile]))
					}
				})

			// Test certificate rotation: when a certificate is expired, the certificate will be rotated.
			t.NewSubTest("rotateDNSCertificatesWhenCertExpired").
				Run(func(t framework.TestContext) {
					sidecarInjectorSecret.Data[ca.CertChainFile] = []byte(certExpired)
					if _, err := cluster.Kube().CoreV1().Secrets(istioNs).Update(context.TODO(), sidecarInjectorSecret, metav1.UpdateOptions{}); err != nil {
						t.Fatalf("failed to update secret (%s:%s), error: %s", istioNs, sidecarInjectorSecret.Name, err)
					}
					// Sleep 5 seconds for the certificate rotation to take place.
					t.Log(`sleep 5 seconds for expired certificate rotation to take place ...`)
					time.Sleep(5 * time.Second)
					sidecarInjectorSecret2 = kube2.WaitForSecretToExistOrFail(t, cluster.Kube(), istioNs, sidecarInjectorSecretName, secretWaitTime)
					t.Log(`checking rotated Sidecar Injector DNS certificate is valid`)
					secret.ExamineDNSSecretOrFail(t, sidecarInjectorSecret2, sidecarInjectorDNSName)
					if bytes.Equal(sidecarInjectorSecret2.Data[ca.CertChainFile],
						sidecarInjectorSecret.Data[ca.CertChainFile]) {
						t.Errorf("the rotated cert should be different from the original cert (%v, %v)",
							string(sidecarInjectorSecret2.Data[ca.CertChainFile]),
							string(sidecarInjectorSecret.Data[ca.CertChainFile]))
					}
				})
		})
}

func deleteSecret(client kubernetes.Interface, namespace, name string) (err error) {
	var immediate int64
	err = client.CoreV1().Secrets(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{GracePeriodSeconds: &immediate})
	return err
}
