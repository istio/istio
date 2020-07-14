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
	"context"
	"reflect"
	"testing"
	"time"

	"istio.io/istio/security/pkg/pki/ca"

	v1 "k8s.io/api/core/v1"

	"istio.io/istio/security/pkg/pki/util"

	cert "k8s.io/api/certificates/v1beta1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes/fake"
)

const (
	exampleExpiredCert = `-----BEGIN CERTIFICATE-----
MIIDXjCCAkagAwIBAgIQGbJDoVfdXBsPos+p8RGqZDANBgkqhkiG9w0BAQsFADBF
MQswCQYDVQQGEwJBVTETMBEGA1UECAwKU29tZS1TdGF0ZTEhMB8GA1UECgwYSW50
ZXJuZXQgV2lkZ2l0cyBQdHkgTHRkMB4XDTE5MDgxNjIxNDQzNVoXDTE5MDgxNjIx
NDQzNlowEzERMA8GA1UEChMISnVqdSBvcmcwggEiMA0GCSqGSIb3DQEBAQUAA4IB
DwAwggEKAoIBAQDYwh5Txh3pJVIjCt5JciYM4GMt3DsjF7E4JdVUUu682YpFevT8
rWJVaZjZajVQaT4IIYw+kxqf0hdVLJG11OHI3OeZ1IJe5yuV+STXks/+vEDMMLuD
vqWZl7oFIuXR6merPAPLAmxo0U5E9kp6ftfHJMK3uj1eNp/BZE/xH8QYe86kAckd
QYPsz0gW1YMdpxRG1OFmbih8CdbRUjCgHHPOxbAJOIDM4xtj8M1rFgVnyH+8NucW
DddKy63GASUphakC73hMnoEQksbVg6rdYlnYrdPcLgmcLeO+vdI5EjbXMaXy7GMk
JvTJI5KRAq+jPHOZmWHd2zAGUpLRPr8EFQp3AgMBAAGjfDB6MA4GA1UdDwEB/wQE
AwIFoDAMBgNVHRMBAf8EAjAAMB8GA1UdIwQYMBaAFKgUbNBCMgiPmanahMYt1KRO
dFX9MDkGA1UdEQEB/wQvMC2GK3NwaWZmZTovL2NsdXN0ZXIubG9jYWwvbnMvZGVm
YXVsdC9zYS9jbGllbnQwDQYJKoZIhvcNAQELBQADggEBAFEf0ZJnNz7h4MqGz720
0FShDX02WPmNSfl89gY/Q4/djTF5yfqgaDAZWf4PsDwpdMpT0eRrshpzvLrRjcd4
Ev6bXGncIwnNFtXQ4dseup+kmaYZF24zpRjyoH9owwu1T5Wb1cSrpFFby5xWuoTC
bsKlR5CZF+dHUwc1iMaj/4kuVjvt4imM0coeaOUzOcMCruO54IqsFcJg2YA80MI+
6UiM8hj8ERDls5iBNThWKKE0yva4HFh1gj5f427NP7CSikUFXs61gytlFHEjHyco
lK8KK65mLIDLshz2+6lPHFXv9tEpouEUws5lhR5O9Q+9LmfBLPE7rD2aUic8EApg
3TE=
-----END CERTIFICATE-----`
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
	exampleCACert2 = `-----BEGIN CERTIFICATE-----
MIIDDDCCAfSgAwIBAgIRALsN8ND73NbYSKTZZa4jf2EwDQYJKoZIhvcNAQELBQAw
LzEtMCsGA1UEAxMkZTNlM2RlZWQtYzIyNi00OWM2LThmOTktNDU3NmRmMzQ0YWQ1
MB4XDTE5MDYwMTE1NTU0M1oXDTI0MDUzMDE2NTU0M1owLzEtMCsGA1UEAxMkZTNl
M2RlZWQtYzIyNi00OWM2LThmOTktNDU3NmRmMzQ0YWQ1MIIBIjANBgkqhkiG9w0B
AQEFAAOCAQ8AMIIBCgKCAQEA43oeK/hS92ANjmg50LCl3tM7eYAlBB/XgCl+bfp3
KwEf+uW5yEvzSVHd2VPFI/kJJeLFrsyCRaU4FwxWcEr2Ld07DPL34oyZRRXQF0w6
4ZNSVmevBNdZLqHcoIUtR1iFJbkctE93HpGw5Kg1NXRLDu47wQtzcC3GDOEk1amu
mL916R2OcYEeOcyRDnlbLcsTYRvK5WBQsux4E0iu2Eo9GIajKmbxVLxA9fsmqG4i
/HoVkLmCg+ZRPR/66AFLPFV1J3RWp0K4HKGzBeCyd2RC+o0g8tJX3EVSuQpqzS8p
i2t71cYu/Sf5gt3wXsNHyzE6bF1o+acyzWvJlBym/HsbAQIDAQABoyMwITAOBgNV
HQ8BAf8EBAMCAgQwDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEA
kJXGkFEybCp7RxUSILuIqMtKcYcQU9ulKmLSn51VrpcRHP4SH7UJ0aXMjAdRLsop
em7YgbvToGNingqcmSJlunR3jXDecSXJLUO1xcfw6N+B2BXRgUv8wV42btr2EV6q
4HKou+MnKdrQkMUx218AT8TNPBb/Yx01m8YUS7mGUTApAhBneGEcKJ8xOznIuR5v
CihWQA9AmUvfixpXNpJc4vqiYErwIXrYpuwc79SRtLuO70vV7FCctz+4JPpR7mp9
dHMZfGO1KXMbYT9P5bm+itlWSyrnn0qK/Cn5RHBoFyY91VcQJTgABS/z5O0pZ662
sNzF00Jhi0gU7th75QT3MA==
-----END CERTIFICATE-----`
)

func TestNewWebhookController(t *testing.T) {
	testCases := map[string]struct {
		gracePeriodRatio  float32
		minGracePeriod    time.Duration
		k8sCaCertFile     string
		dnsNames          []string
		secretNames       []string
		serviceNamespaces []string
		shouldFail        bool
	}{
		"invalid grade period ratio": {
			gracePeriodRatio:  1.5,
			dnsNames:          []string{"foo"},
			secretNames:       []string{"foo.secret"},
			serviceNamespaces: []string{"foo.ns"},
			k8sCaCertFile:     "./test-data/example-invalid-ca-cert.pem",
			shouldFail:        true,
		},
		"invalid CA cert path": {
			gracePeriodRatio:  0.6,
			dnsNames:          []string{"foo"},
			secretNames:       []string{"foo.secret"},
			serviceNamespaces: []string{"foo.ns"},
			k8sCaCertFile:     "./invalid-path/invalid-file",
			shouldFail:        true,
		},
		"valid CA cert path": {
			gracePeriodRatio:  0.6,
			dnsNames:          []string{"foo"},
			secretNames:       []string{"foo.secret"},
			serviceNamespaces: []string{"foo.ns"},
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			shouldFail:        false,
		},
	}

	for _, tc := range testCases {
		client := fake.NewSimpleClientset()
		_, err := NewWebhookController(tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.secretNames, tc.dnsNames, tc.serviceNamespaces)
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

func TestUpsertSecret(t *testing.T) {
	dnsNames := []string{"foo"}

	testCases := map[string]struct {
		gracePeriodRatio  float32
		minGracePeriod    time.Duration
		k8sCaCertFile     string
		dnsNames          []string
		secretNames       []string
		serviceNamespaces []string
		scrtName          string
		expectFaill       bool
	}{
		"upsert a valid secret name should succeed": {
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          dnsNames,
			secretNames:       []string{"istio.webhook.foo"},
			serviceNamespaces: []string{"foo.ns"},
			scrtName:          "istio.webhook.foo",
			expectFaill:       false,
		},
	}

	for _, tc := range testCases {
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
		certWatchTimeout = time.Millisecond
		wc, err := NewWebhookController(tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.secretNames, tc.dnsNames, tc.serviceNamespaces)
		if err != nil {
			t.Errorf("failed at creating webhook controller: %v", err)
			continue
		}

		err = wc.upsertSecret(tc.scrtName, tc.dnsNames[0], tc.serviceNamespaces[0])
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

func TestScrtDeleted(t *testing.T) {
	dnsNames := []string{"foo"}

	testCases := map[string]struct {
		gracePeriodRatio  float32
		minGracePeriod    time.Duration
		k8sCaCertFile     string
		dnsNames          []string
		secretNames       []string
		serviceNamespaces []string
	}{
		"recover a deleted secret should succeed": {
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          dnsNames,
			secretNames:       []string{"istio.webhook.foo"},
			serviceNamespaces: []string{"foo.ns"},
		},
	}

	csr := &cert.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: "domain-cluster.local-ns--secret-mock-secret",
		},
		Status: cert.CertificateSigningRequestStatus{
			Certificate: []byte(exampleIssuedCert),
		},
	}

	certWatchTimeout = time.Millisecond
	for _, tc := range testCases {
		client := fake.NewSimpleClientset()
		client.PrependReactor("get", "certificatesigningrequests", defaultReactionFunc(csr))

		wc, err := NewWebhookController(tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.secretNames, tc.dnsNames, tc.serviceNamespaces)
		if err != nil {
			t.Errorf("failed at creating webhook controller: %v", err)
			continue
		}

		_, err = client.CoreV1().Secrets(tc.serviceNamespaces[0]).Create(context.TODO(), &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: tc.secretNames[0],
				Labels: map[string]string{
					"secret": "for-testing",
				},
			},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("failed creating test secret (%v): %v", tc.secretNames[0], err)
		}
		scrt, err := client.CoreV1().Secrets(tc.serviceNamespaces[0]).Get(context.TODO(), tc.secretNames[0], metav1.GetOptions{})
		if err != nil || scrt == nil {
			t.Fatalf("failed to get test secret (%v): err (%v), secret (%v)", tc.secretNames[0], err, scrt)
		}
		err = client.CoreV1().Secrets(tc.serviceNamespaces[0]).Delete(context.TODO(), tc.secretNames[0], metav1.DeleteOptions{})
		if err != nil {
			t.Fatalf("failed deleting test secret (%v): %v", tc.secretNames[0], err)
		}
		_, err = client.CoreV1().Secrets(tc.serviceNamespaces[0]).Get(context.TODO(), tc.secretNames[0], metav1.GetOptions{})
		if err == nil {
			t.Fatal("the deleted secret should not exist")
		}

		// The secret deleted should be recovered.
		wc.scrtDeleted(scrt)
		scrt, err = client.CoreV1().Secrets(tc.serviceNamespaces[0]).Get(context.TODO(), tc.secretNames[0], metav1.GetOptions{})
		if err != nil || scrt == nil {
			t.Fatalf("after scrtDeleted(), failed to get test secret (%v): err (%v), secret (%v)",
				tc.secretNames[0], err, scrt)
		}
	}
}

func TestScrtUpdated(t *testing.T) {
	dnsNames := []string{"foo"}

	certWatchTimeout = time.Millisecond
	testCases := map[string]struct {
		gracePeriodRatio       float32
		minGracePeriod         time.Duration
		k8sCaCertFile          string
		dnsNames               []string
		secretNames            []string
		serviceNamespaces      []string
		changeCACert           bool
		invalidNewSecret       bool
		replaceWithExpiredCert bool
		expectUpdate           bool
		newScrtName            string
	}{
		"invalid new secret should not affect existing secret": {
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          dnsNames,
			serviceNamespaces: []string{"foo.ns"},
			secretNames:       []string{"istio.webhook.foo"},
			invalidNewSecret:  true,
			expectUpdate:      false,
		},
		"non-webhook secret should not be updated": {
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          dnsNames,
			secretNames:       []string{"istio.webhook.foo"},
			serviceNamespaces: []string{"foo.ns"},
			newScrtName:       "bar",
			invalidNewSecret:  false,
			expectUpdate:      false,
		},
		"expired certificate should be updated": {
			gracePeriodRatio:       0.6,
			k8sCaCertFile:          "./test-data/example-ca-cert.pem",
			dnsNames:               dnsNames,
			secretNames:            []string{"istio.webhook.foo"},
			serviceNamespaces:      []string{"foo.ns"},
			replaceWithExpiredCert: true,
			expectUpdate:           true,
		},
		"changing CA certificate should lead to updating secret": {
			gracePeriodRatio:       0.6,
			k8sCaCertFile:          "./test-data/example-ca-cert.pem",
			dnsNames:               dnsNames,
			secretNames:            []string{"istio.webhook.foo"},
			serviceNamespaces:      []string{"foo.ns"},
			changeCACert:           true,
			replaceWithExpiredCert: false,
			expectUpdate:           true,
		},
	}

	csr := &cert.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: "domain-cluster.local-ns--secret-mock-secret",
		},
		Status: cert.CertificateSigningRequestStatus{
			Certificate: []byte(exampleIssuedCert),
		},
	}

	for _, tc := range testCases {
		client := fake.NewSimpleClientset()
		client.PrependReactor("get", "certificatesigningrequests", defaultReactionFunc(csr))

		wc, err := NewWebhookController(tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.secretNames, tc.dnsNames, tc.serviceNamespaces)
		if err != nil {
			t.Errorf("failed at creating webhook controller: %v", err)
			continue
		}

		err = wc.upsertSecret(tc.secretNames[0], tc.dnsNames[0], tc.serviceNamespaces[0])
		if err != nil {
			t.Errorf("should not failed at upsertSecret, err: %v", err)
		}
		scrt, err := client.CoreV1().Secrets(tc.serviceNamespaces[0]).Get(context.TODO(), tc.secretNames[0], metav1.GetOptions{})
		if err != nil || scrt == nil {
			t.Fatalf("failed to get test secret (%v): err (%v), secret (%v)", tc.secretNames[0], err, scrt)
		}

		if tc.newScrtName != "" {
			scrt.Name = tc.newScrtName
		}
		if tc.replaceWithExpiredCert {
			scrt.Data[ca.CertChainID] = []byte(exampleExpiredCert)
		}
		if tc.changeCACert {
			scrt.Data[ca.RootCertID] = []byte(exampleCACert2)
		}

		var newScrt interface{}
		if tc.invalidNewSecret {
			// point to an invalid secret object
			newScrt = &v1.ConfigMap{}
		} else {
			newScrt = &v1.Secret{}
			scrt.DeepCopyInto(newScrt.(*v1.Secret))
		}
		wc.scrtUpdated(scrt, newScrt)

		// scrt2 is the secret after updating, which will be compared against original scrt
		scrt2, err := client.CoreV1().Secrets(tc.serviceNamespaces[0]).Get(context.TODO(), tc.secretNames[0], metav1.GetOptions{})
		if err != nil || scrt2 == nil {
			t.Fatalf("failed to get test secret (%v): err (%v), secret (%v)", tc.secretNames[0], err, scrt2)
		}
		if tc.newScrtName != "" {
			scrt2.Name = tc.newScrtName
		}
		if tc.expectUpdate {
			if reflect.DeepEqual(scrt, scrt2) {
				t.Errorf("change is expected while there is no change")
			}
		} else {
			if !reflect.DeepEqual(scrt, scrt2) {
				t.Errorf("change is not expected while there is change")
			}
		}
	}
}

func TestRefreshSecret(t *testing.T) {
	dnsNames := []string{"foo"}

	testCases := map[string]struct {
		gracePeriodRatio  float32
		minGracePeriod    time.Duration
		k8sCaCertFile     string
		dnsNames          []string
		secretNames       []string
		serviceNamespaces []string
		changeCACert      bool
		expectUpdate      bool
	}{
		"refresh a secret with different CA cert should succeed": {
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          dnsNames,
			secretNames:       []string{"istio.webhook.foo"},
			serviceNamespaces: []string{"foo.ns"},
			changeCACert:      true,
			expectUpdate:      true,
		},
	}

	csr := &cert.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: "domain-cluster.local-ns--secret-mock-secret",
		},
		Status: cert.CertificateSigningRequestStatus{
			Certificate: []byte(exampleIssuedCert),
		},
	}

	for _, tc := range testCases {
		client := fake.NewSimpleClientset()
		client.PrependReactor("get", "certificatesigningrequests", defaultReactionFunc(csr))

		wc, err := NewWebhookController(tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.secretNames, tc.dnsNames, tc.serviceNamespaces)

		if err != nil {
			t.Errorf("failed at creating webhook controller: %v", err)
			continue
		}

		err = wc.upsertSecret(tc.secretNames[0], tc.dnsNames[0], tc.serviceNamespaces[0])
		if err != nil {
			t.Errorf("should not failed at upsertSecret, err: %v", err)
		}
		scrt, err := client.CoreV1().Secrets(tc.serviceNamespaces[0]).Get(context.TODO(), tc.secretNames[0], metav1.GetOptions{})
		if err != nil || scrt == nil {
			t.Fatalf("failed to get test secret (%v): err (%v), secret (%v)", tc.secretNames[0], err, scrt)
		}

		if tc.changeCACert {
			scrt.Data[ca.RootCertID] = []byte(exampleCACert2)
		}

		newScrt := &v1.Secret{}
		scrt.DeepCopyInto(newScrt)
		err = wc.refreshSecret(newScrt)
		if err != nil {
			t.Fatalf("failed to refresh secret (%v), err: %v", newScrt, err)
		}

		// scrt2 is the secret after refreshing, which will be compared against original scrt
		scrt2, err := client.CoreV1().Secrets(tc.serviceNamespaces[0]).Get(context.TODO(), tc.secretNames[0], metav1.GetOptions{})
		if err != nil || scrt2 == nil {
			t.Fatalf("failed to get test secret (%v): err (%v), secret (%v)", tc.secretNames[0], err, scrt2)
		}
		if tc.expectUpdate {
			if reflect.DeepEqual(scrt, scrt2) {
				t.Errorf("change is expected while there is no change")
			}
		} else {
			if !reflect.DeepEqual(scrt, scrt2) {
				t.Errorf("change is not expected while there is change")
			}
		}
	}
}

func TestCleanUpCertGen(t *testing.T) {
	dnsNames := []string{"foo"}

	testCases := map[string]struct {
		gracePeriodRatio  float32
		minGracePeriod    time.Duration
		k8sCaCertFile     string
		dnsNames          []string
		secretNames       []string
		serviceNamespaces []string
	}{
		"clean up a CSR should succeed": {
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          dnsNames,
			secretNames:       []string{"istio.webhook.foo"},
			serviceNamespaces: []string{"foo.ns"},
		},
	}

	csrName := "test-csr"

	for _, tc := range testCases {
		client := fake.NewSimpleClientset()
		wc, err := NewWebhookController(tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.secretNames, tc.dnsNames, tc.serviceNamespaces)
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
		_, err = wc.certClient.CertificateSigningRequests().Create(context.TODO(), k8sCSR, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("error when creating CSR: %v", err)
		}

		csr, err := wc.certClient.CertificateSigningRequests().Get(context.TODO(), csrName, metav1.GetOptions{})
		if err != nil || csr == nil {
			t.Fatalf("failed to get CSR: name (%v), err (%v), CSR (%v)", csrName, err, csr)
		}

		// The CSR should be deleted.
		err = cleanUpCertGen(wc.certClient.CertificateSigningRequests(), csrName)
		if err != nil {
			t.Errorf("cleanUpCertGen returns an error: %v", err)
		}
		_, err = wc.certClient.CertificateSigningRequests().Get(context.TODO(), csrName, metav1.GetOptions{})
		if err == nil {
			t.Fatalf("should failed at getting CSR: name (%v)", csrName)
		}
	}
}

func TestIsWebhookSecret(t *testing.T) {
	dnsNames := []string{"foo", "bar"}

	testCases := map[string]struct {
		gracePeriodRatio  float32
		minGracePeriod    time.Duration
		k8sCaCertFile     string
		namespace         string
		dnsNames          []string
		secretNames       []string
		serviceNamespaces []string
		scrtName          string
		scrtNameSpace     string
		expectedRet       bool
	}{
		"a valid webhook secret in valid namespace": {
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          dnsNames,
			secretNames:       []string{"istio.webhook.foo", "istio.webhook.bar"},
			serviceNamespaces: []string{"ns.foo", "ns.bar"},
			namespace:         "ns.foo",
			scrtName:          "istio.webhook.foo",
			scrtNameSpace:     "ns.foo",
			expectedRet:       true,
		},
		"an invalid webhook secret in valid namespace": {
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			namespace:         "ns.foo",
			dnsNames:          dnsNames,
			secretNames:       []string{"istio.webhook.foo", "istio.webhook.bar"},
			serviceNamespaces: []string{"ns.foo", "ns.bar"},
			scrtName:          "istio.webhook.invalid",
			scrtNameSpace:     "ns.foo",
			expectedRet:       false,
		},
		"a valid webhook secret in invalid namespace": {
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			namespace:         "ns.foo",
			dnsNames:          dnsNames,
			secretNames:       []string{"istio.webhook.foo", "istio.webhook.bar"},
			serviceNamespaces: []string{"ns.foo", "ns.bar"},
			scrtName:          "istio.webhook.foo",
			scrtNameSpace:     "ns.invalid",
			expectedRet:       false,
		},
	}

	for _, tc := range testCases {
		client := fake.NewSimpleClientset()
		wc, err := NewWebhookController(tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.secretNames, tc.dnsNames, tc.serviceNamespaces)
		if err != nil {
			t.Fatalf("failed to create a webhook controller: %v", err)
		}

		ret := wc.isWebhookSecret(tc.scrtName, tc.scrtNameSpace)
		if tc.expectedRet != ret {
			t.Fatalf("expected result (%v) differs from the actual result (%v)", tc.expectedRet, ret)
		}
		// Sleep for watchers to release the file handles
		time.Sleep(10 * time.Millisecond)
	}
}

func TestGetCACert(t *testing.T) {
	dnsNames := []string{"foo"}

	testCases := map[string]struct {
		gracePeriodRatio  float32
		minGracePeriod    time.Duration
		k8sCaCertFile     string
		namespace         string
		dnsNames          []string
		secretNames       []string
		serviceNamespaces []string
		expectFail        bool
	}{
		"getCACert should succeed for a valid certificate": {
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			namespace:         "foo.ns",
			dnsNames:          dnsNames,
			secretNames:       []string{"istio.webook.foo"},
			serviceNamespaces: []string{"foo.ns"},
			expectFail:        false,
		},
	}

	for _, tc := range testCases {
		client := fake.NewSimpleClientset()
		// If the CA cert. is invalid, NewWebhookController will fail.
		wc, err := NewWebhookController(tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.secretNames, tc.dnsNames, tc.serviceNamespaces)
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

func TestGetDNSName(t *testing.T) {
	dnsNames := []string{"foo", "bar", "baz"}
	serviceNamespaces := []string{"foo.ns", "bar.ns", "baz.ns"}

	testCases := map[string]struct {
		gracePeriodRatio  float32
		minGracePeriod    time.Duration
		k8sCaCertFile     string
		dnsNames          []string
		secretNames       []string
		serviceNamespaces []string
		scrtName          string
		expectFound       bool
		expectedSvcName   string
	}{
		"a service corresponding to a secret exists": {
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          dnsNames,
			secretNames:       []string{"istio.webhook.foo", "istio.webhook.bar", "istio.webhoo.baz"},
			serviceNamespaces: serviceNamespaces,
			scrtName:          "istio.webhook.foo",
			expectFound:       true,
			expectedSvcName:   "foo",
		},
		"a service corresponding to a secret does not exists": {
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          dnsNames,
			secretNames:       []string{"istio.webhook.foo", "istio.webhook.bar", "istio.webhoo.baz"},
			serviceNamespaces: serviceNamespaces,
			scrtName:          "istio.webhook.barr",
			expectFound:       false,
			expectedSvcName:   "bar",
		},
	}

	for _, tc := range testCases {
		client := fake.NewSimpleClientset()
		wc, err := NewWebhookController(tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.secretNames, tc.dnsNames, serviceNamespaces)
		if err != nil {
			t.Errorf("failed to create a webhook controller: %v", err)
		}

		ret, found := wc.getDNSName(tc.scrtName)
		if tc.expectFound != found {
			t.Errorf("expected found (%v) differs from the actual found (%v)", tc.expectFound, found)
			continue
		}
		if found && tc.expectedSvcName != ret {
			t.Errorf("the service name (%v) returned is not as expcted (%v)", ret, tc.expectedSvcName)
		}
	}
}
