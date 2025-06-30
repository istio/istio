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

package kube

import (
	"fmt"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	cluster2 "istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
)

func makeSecret(name string, data map[string]string, secretType corev1.SecretType) *corev1.Secret {
	bdata := map[string][]byte{}
	for k, v := range data {
		bdata[k] = []byte(v)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Data: bdata,
		Type: secretType,
	}
}

var (
	genericCert = makeSecret("generic", map[string]string{
		GenericScrtCert: "generic-cert", GenericScrtKey: "generic-key",
	}, corev1.SecretTypeTLS)
	genericMtlsCert = makeSecret("generic-mtls", map[string]string{
		GenericScrtCert: "generic-mtls-cert", GenericScrtKey: "generic-mtls-key", GenericScrtCaCert: "generic-mtls-ca",
	}, corev1.SecretTypeTLS)
	genericMtlsCertSplit = makeSecret("generic-mtls-split", map[string]string{
		GenericScrtCert: "generic-mtls-split-cert", GenericScrtKey: "generic-mtls-split-key",
	}, corev1.SecretTypeTLS)
	genericMtlsCertSplitCa = makeSecret("generic-mtls-split-cacert", map[string]string{
		GenericScrtCaCert: "generic-mtls-split-ca",
	}, corev1.SecretTypeTLS)
	overlapping = makeSecret("overlap", map[string]string{
		GenericScrtCert: "cert", GenericScrtKey: "key", GenericScrtCaCert: "main-ca",
	}, corev1.SecretTypeTLS)
	overlappingCa = makeSecret("overlap-cacert", map[string]string{
		GenericScrtCaCert: "split-ca",
	}, corev1.SecretTypeTLS)
	tlsCert = makeSecret("tls", map[string]string{
		TLSSecretCert: "tls-cert", TLSSecretKey: "tls-key", TLSSecretOcspStaple: "tls-ocsp-staple",
	}, corev1.SecretTypeTLS)
	tlsMtlsCert = makeSecret("tls-mtls", map[string]string{
		TLSSecretCert: "tls-mtls-cert", TLSSecretKey: "tls-mtls-key", TLSSecretCaCert: "tls-mtls-ca",
	}, corev1.SecretTypeTLS)
	tlsMtlsCertWithCrl = makeSecret("tls-mtls-crl", map[string]string{
		TLSSecretCert: "tls-mtls-cert", TLSSecretKey: "tls-mtls-key", TLSSecretCaCert: "tls-mtls-ca", TLSSecretCrl: "tls-mtls-crl",
	}, corev1.SecretTypeTLS)
	tlsMtlsCertSplit = makeSecret("tls-mtls-split", map[string]string{
		TLSSecretCert: "tls-mtls-split-cert", TLSSecretKey: "tls-mtls-split-key",
	}, corev1.SecretTypeTLS)
	tlsMtlsCertSplitCa = makeSecret("tls-mtls-split-cacert", map[string]string{
		TLSSecretCaCert: "tls-mtls-split-ca",
	}, corev1.SecretTypeTLS)
	tlsMtlsCertSplitCaWithCrl = makeSecret("tls-mtls-split-crl-cacert", map[string]string{
		TLSSecretCaCert: "tls-mtls-split-ca", TLSSecretCrl: "tls-mtls-split-crl",
	}, corev1.SecretTypeTLS)
	tlsMtlsCertSplitWithCrl = makeSecret("tls-mtls-split-crl", map[string]string{
		TLSSecretCert: "tls-mtls-split-crl-cert", TLSSecretKey: "tls-mtls-split-crl-key",
	}, corev1.SecretTypeTLS)
	emptyCert = makeSecret("empty-cert", map[string]string{
		TLSSecretCert: "", TLSSecretKey: "tls-key",
	}, corev1.SecretTypeTLS)
	wrongKeys = makeSecret("wrong-keys", map[string]string{
		"foo-bar": "my-cert", TLSSecretKey: "tls-key",
	}, corev1.SecretTypeTLS)
	dockerjson = makeSecret("docker-json", map[string]string{
		corev1.DockerConfigJsonKey: "docker-cred",
	}, corev1.SecretTypeDockerConfigJson)
	badDockerjson = makeSecret("bad-docker-json", map[string]string{
		"docker-key": "docker-cred",
	}, corev1.SecretTypeDockerConfigJson)
)

func TestSecretsController(t *testing.T) {
	secrets := []runtime.Object{
		genericCert,
		genericMtlsCert,
		genericMtlsCertSplit,
		genericMtlsCertSplitCa,
		overlapping,
		overlappingCa,
		tlsCert,
		tlsMtlsCert,
		tlsMtlsCertWithCrl,
		tlsMtlsCertSplit,
		tlsMtlsCertSplitCa,
		tlsMtlsCertSplitCaWithCrl,
		tlsMtlsCertSplitWithCrl,
		emptyCert,
		wrongKeys,
	}
	client := kube.NewFakeClient(secrets...)
	sc := NewCredentialsController(client, nil, true)
	client.RunAndWait(test.NewStop(t))
	cases := []struct {
		name            string
		namespace       string
		cert            string
		key             string
		staple          string
		caCert          string
		caCrl           string
		crl             string
		expectedError   string
		expectedCAError string
	}{
		{
			name:            "generic",
			namespace:       "default",
			cert:            "generic-cert",
			key:             "generic-key",
			expectedCAError: "found secret, but didn't have expected keys cacert or ca.crt; found: cert, key",
		},
		{
			name:      "generic-mtls",
			namespace: "default",
			cert:      "generic-mtls-cert",
			key:       "generic-mtls-key",
			caCert:    "generic-mtls-ca",
		},
		{
			name:            "generic-mtls-split",
			namespace:       "default",
			cert:            "generic-mtls-split-cert",
			key:             "generic-mtls-split-key",
			expectedCAError: "found secret, but didn't have expected keys cacert or ca.crt; found: cert, key",
		},
		{
			name:          "generic-mtls-split-cacert",
			namespace:     "default",
			caCert:        "generic-mtls-split-ca",
			expectedError: "found secret, but didn't have expected keys (cert and key) or (tls.crt and tls.key); found: cacert",
		},
		// The -cacert secret has precedence
		{
			name:          "overlap-cacert",
			namespace:     "default",
			caCert:        "split-ca",
			expectedError: "found secret, but didn't have expected keys (cert and key) or (tls.crt and tls.key); found: cacert",
		},
		{
			name:            "tls",
			namespace:       "default",
			cert:            "tls-cert",
			key:             "tls-key",
			staple:          "tls-ocsp-staple",
			expectedCAError: "found secret, but didn't have expected keys cacert or ca.crt; found: tls.crt, tls.key, tls.ocsp-staple, and 0 more...",
		},
		{
			name:      "tls-mtls",
			namespace: "default",
			cert:      "tls-mtls-cert",
			key:       "tls-mtls-key",
			caCert:    "tls-mtls-ca",
		},
		{
			name:      "tls-mtls-crl",
			namespace: "default",
			cert:      "tls-mtls-cert",
			key:       "tls-mtls-key",
			caCert:    "tls-mtls-ca",
			crl:       "tls-mtls-crl",
			caCrl:     "tls-mtls-crl",
		},
		{
			name:            "tls-mtls-split",
			namespace:       "default",
			cert:            "tls-mtls-split-cert",
			key:             "tls-mtls-split-key",
			expectedCAError: "found secret, but didn't have expected keys cacert or ca.crt; found: tls.crt, tls.key",
		},
		{
			name:          "tls-mtls-split-cacert",
			namespace:     "default",
			caCert:        "tls-mtls-split-ca",
			expectedError: "found secret, but didn't have expected keys (cert and key) or (tls.crt and tls.key); found: ca.crt",
		},
		{
			name:          "tls-mtls-split-crl-cacert",
			namespace:     "default",
			caCert:        "tls-mtls-split-ca",
			expectedError: "found secret, but didn't have expected keys (cert and key) or (tls.crt and tls.key); found: ca.crl, ca.crt",
			caCrl:         "tls-mtls-split-crl",
		},
		{
			name:            "tls-mtls-split-crl",
			namespace:       "default",
			cert:            "tls-mtls-split-crl-cert",
			key:             "tls-mtls-split-crl-key",
			expectedCAError: "found secret, but didn't have expected keys cacert or ca.crt; found: tls.crt, tls.key",
		},
		{
			name:            "generic",
			namespace:       "wrong-namespace",
			expectedError:   `secret wrong-namespace/generic not found`,
			expectedCAError: `secret wrong-namespace/generic not found`,
		},
		{
			name:            "empty-cert",
			namespace:       "default",
			expectedError:   `found keys "tls.crt" and "tls.key", but they were empty`,
			expectedCAError: "found secret, but didn't have expected keys cacert or ca.crt; found: tls.crt, tls.key",
		},
		{
			name:            "wrong-keys",
			namespace:       "default",
			expectedError:   `found secret, but didn't have expected keys (cert and key) or (tls.crt and tls.key); found: foo-bar, tls.key`,
			expectedCAError: "found secret, but didn't have expected keys cacert or ca.crt; found: foo-bar, tls.key",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			certInfo, err := sc.GetCertInfo(tt.name, tt.namespace)
			var actualKey []byte
			var actualCert []byte
			var actualStaple []byte
			var actualCrl []byte
			if certInfo != nil {
				actualKey = certInfo.Key
				actualCert = certInfo.Cert
				actualStaple = certInfo.Staple
				actualCrl = certInfo.CRL
			}
			if tt.key != string(actualKey) {
				t.Errorf("got key %q, wanted %q", string(actualKey), tt.key)
			}
			if tt.cert != string(actualCert) {
				t.Errorf("got cert %q, wanted %q", string(actualCert), tt.cert)
			}
			if tt.staple != string(actualStaple) {
				t.Errorf("got staple %q, wanted %q", string(actualStaple), tt.staple)
			}
			if tt.crl != string(actualCrl) {
				t.Errorf("got crl %q, wanted %q", string(actualCrl), tt.crl)
			}
			if tt.expectedError != errString(err) {
				t.Errorf("got err %q, wanted %q", errString(err), tt.expectedError)
			}
			caCertInfo, err := sc.GetCaCert(tt.name, tt.namespace)
			if caCertInfo != nil && tt.caCert != string(caCertInfo.Cert) {
				t.Errorf("got caCert %q, wanted %q", string(caCertInfo.Cert), tt.caCert)
			}
			if caCertInfo != nil && tt.caCrl != string(caCertInfo.CRL) {
				t.Errorf("got caCrl %q, wanted %q", string(caCertInfo.CRL), tt.crl)
			}
			if tt.expectedCAError != errString(err) {
				t.Errorf("got ca err %q, wanted %q", errString(err), tt.expectedCAError)
			}
		})
	}
}

func TestDockerCredentials(t *testing.T) {
	secrets := []runtime.Object{
		dockerjson,
		badDockerjson,
		genericCert,
	}
	client := kube.NewFakeClient(secrets...)
	sc := NewCredentialsController(client, nil, true)
	client.RunAndWait(test.NewStop(t))
	cases := []struct {
		name                string
		namespace           string
		expectedType        corev1.SecretType
		expectedDockerCred  string
		expectedDockerError string
	}{
		{
			name:               "docker-json",
			namespace:          "default",
			expectedType:       corev1.SecretTypeDockerConfigJson,
			expectedDockerCred: "docker-cred",
		},
		{
			name:                "bad-docker-json",
			namespace:           "default",
			expectedDockerError: "cannot find docker config at secret default/bad-docker-json",
		},
		{
			name:                "wrong-name",
			namespace:           "default",
			expectedDockerError: "secret default/wrong-name not found",
		},
		{
			name:                "generic",
			namespace:           "default",
			expectedDockerError: "type of secret default/generic is not kubernetes.io/dockerconfigjson",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dockerCred, err := sc.GetDockerCredential(tt.name, tt.namespace)
			if tt.expectedDockerCred != "" && tt.expectedDockerCred != string(dockerCred) {
				t.Errorf("got docker credential %q, want %q", string(dockerCred), tt.expectedDockerCred)
			}
			if tt.expectedDockerError != "" && tt.expectedDockerError != errString(err) {
				t.Errorf("got docker err %q, wanted %q", errString(err), tt.expectedDockerError)
			}
		})
	}
}

func errString(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

func allowIdentities(c kube.Client, identities ...string) {
	allowed := sets.New(identities...)
	c.Kube().(*fake.Clientset).Fake.PrependReactor("create", "subjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		a := action.(k8stesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)
		if allowed.Contains(a.Spec.User) {
			return true, &authorizationv1.SubjectAccessReview{
				Status: authorizationv1.SubjectAccessReviewStatus{
					Allowed: true,
				},
			}, nil
		}
		return true, &authorizationv1.SubjectAccessReview{
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed: false,
				Reason:  fmt.Sprintf("user %s cannot access secrets", a.Spec.User),
			},
		}, nil
	})
}

func TestForCluster(t *testing.T) {
	stop := test.NewStop(t)
	localClient := kube.NewFakeClient()
	localClient.RunAndWait(stop)
	remoteClient := kube.NewFakeClient()
	remoteClient.RunAndWait(stop)
	mc := multicluster.NewFakeController()
	sc := NewMulticluster("local", mc)
	mc.Add("local", localClient, stop)
	mc.Add("remote", remoteClient, stop)
	mc.Add("remote2", remoteClient, stop)
	cases := []struct {
		cluster cluster2.ID
		allowed bool
	}{
		{"local", true},
		{"remote", true},
		{"remote2", true},
		{"invalid", false},
	}
	for _, tt := range cases {
		t.Run(string(tt.cluster), func(t *testing.T) {
			_, err := sc.ForCluster(tt.cluster)
			if (err == nil) != tt.allowed {
				t.Fatalf("expected allowed=%v, got err=%v", tt.allowed, err)
			}
		})
	}
}

func TestAuthorize(t *testing.T) {
	stop := test.NewStop(t)
	localClient := kube.NewFakeClient()
	localClient.RunAndWait(stop)
	remoteClient := kube.NewFakeClient()
	remoteClient.RunAndWait(stop)
	allowIdentities(localClient, "system:serviceaccount:ns-local:sa-allowed")
	allowIdentities(remoteClient, "system:serviceaccount:ns-remote:sa-allowed")
	mc := multicluster.NewFakeController()
	sc := NewMulticluster("local", mc)
	mc.Add("local", localClient, stop)
	mc.Add("remote", remoteClient, stop)
	cases := []struct {
		sa      string
		ns      string
		cluster cluster2.ID
		allowed bool
	}{
		{"sa-denied", "ns-local", "local", false},
		{"sa-allowed", "ns-local", "local", true},
		{"sa-denied", "ns-local", "remote", false},
		{"sa-allowed", "ns-local", "remote", false},
		{"sa-denied", "ns-remote", "local", false},
		{"sa-allowed", "ns-remote", "local", false},
		{"sa-denied", "ns-remote", "remote", false},
		{"sa-allowed", "ns-remote", "remote", true},
	}
	for _, tt := range cases {
		t.Run(fmt.Sprintf("%v/%v/%v", tt.sa, tt.ns, tt.cluster), func(t *testing.T) {
			con, err := sc.ForCluster(tt.cluster)
			if err != nil {
				t.Fatal(err)
			}
			got := con.Authorize(tt.sa, tt.ns)
			if (got == nil) != tt.allowed {
				t.Fatalf("expected allowed=%v, got error=%v", tt.allowed, got)
			}
		})
	}
}

func TestSecretsControllerMulticluster(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	secretsLocal := []runtime.Object{
		tlsCert,
		tlsMtlsCert,
		tlsMtlsCertSplit,
		tlsMtlsCertSplitCa,
	}
	tlsCertModified := makeSecret("tls", map[string]string{
		TLSSecretCert: "tls-cert-mod", TLSSecretKey: "tls-key",
	}, corev1.SecretTypeTLS)
	secretsRemote := []runtime.Object{
		tlsCertModified,
		genericCert,
		genericMtlsCert,
		genericMtlsCertSplit,
		genericMtlsCertSplitCa,
	}

	localClient := kube.NewFakeClient(secretsLocal...)
	remoteClient := kube.NewFakeClient(secretsRemote...)
	otherRemoteClient := kube.NewFakeClient()
	mc := multicluster.NewFakeController()
	sc := NewMulticluster("local", mc)
	mc.Add("local", localClient, stop)
	mc.Add("remote", remoteClient, stop)
	mc.Add("other", otherRemoteClient, stop)

	// normally the remote secrets controller would start these
	localClient.RunAndWait(stop)
	remoteClient.RunAndWait(stop)
	otherRemoteClient.RunAndWait(stop)

	cases := []struct {
		name      string
		namespace string
		cluster   cluster2.ID
		cert      string
		key       string
		caCert    string
	}{
		// From local cluster
		// These are only in remote cluster, we do not have access
		{"generic", "default", "local", "", "", ""},
		{"generic-mtls", "default", "local", "", "", ""},
		{"generic-mtls-split", "default", "local", "", "", ""},
		{"generic-mtls-split-cacert", "default", "local", "", "", ""},
		// These are in local cluster, we can access
		{"tls", "default", "local", "tls-cert", "tls-key", ""},
		{"tls-mtls", "default", "local", "tls-mtls-cert", "tls-mtls-key", "tls-mtls-ca"},
		{"tls-mtls-split", "default", "local", "tls-mtls-split-cert", "tls-mtls-split-key", ""},
		{"tls-mtls-split-cacert", "default", "local", "", "", "tls-mtls-split-ca"},
		{"generic", "wrong-namespace", "local", "", "", ""},

		// From remote cluster
		// We can access all credentials - local and remote
		{"generic", "default", "remote", "generic-cert", "generic-key", ""},
		{"generic-mtls", "default", "remote", "generic-mtls-cert", "generic-mtls-key", "generic-mtls-ca"},
		{"generic-mtls-split", "default", "remote", "generic-mtls-split-cert", "generic-mtls-split-key", ""},
		{"generic-mtls-split-cacert", "default", "remote", "", "", "generic-mtls-split-ca"},
		// This is present in local and remote, but with a different value. We have the remote.
		{"tls", "default", "remote", "tls-cert-mod", "tls-key", ""},
		{"tls-mtls", "default", "remote", "tls-mtls-cert", "tls-mtls-key", "tls-mtls-ca"},
		{"tls-mtls-split", "default", "remote", "tls-mtls-split-cert", "tls-mtls-split-key", ""},
		{"tls-mtls-split-cacert", "default", "remote", "", "", "tls-mtls-split-ca"},
		{"generic", "wrong-namespace", "remote", "", "", ""},

		// From other remote cluster
		// We have no in cluster credentials; can only access those in config cluster
		{"generic", "default", "other", "", "", ""},
		{"generic-mtls", "default", "other", "", "", ""},
		{"generic-mtls-split", "default", "other", "", "", ""},
		{"generic-mtls-split-cacert", "default", "other", "", "", ""},
		{"tls", "default", "other", "tls-cert", "tls-key", ""},
		{"tls-mtls", "default", "other", "tls-mtls-cert", "tls-mtls-key", "tls-mtls-ca"},
		{"tls-mtls-split", "default", "other", "tls-mtls-split-cert", "tls-mtls-split-key", ""},
		{"tls-mtls-split-cacert", "default", "other", "", "", "tls-mtls-split-ca"},
		{"generic", "wrong-namespace", "other", "", "", ""},
	}
	for _, tt := range cases {
		t.Run(fmt.Sprintf("%s-%v", tt.name, tt.cluster), func(t *testing.T) {
			con, err := sc.ForCluster(tt.cluster)
			if err != nil {
				t.Fatal(err)
			}
			certInfo, _ := con.GetCertInfo(tt.name, tt.namespace)
			var actualKey []byte
			var actualCert []byte
			if certInfo != nil {
				actualKey = certInfo.Key
				actualCert = certInfo.Cert
			}
			if tt.key != string(actualKey) {
				t.Errorf("got key %q, wanted %q", string(actualKey), tt.key)
			}
			if tt.cert != string(actualCert) {
				t.Errorf("got cert %q, wanted %q", string(actualCert), tt.cert)
			}
			caCertInfo, err := con.GetCaCert(tt.name, tt.namespace)
			if caCertInfo != nil && tt.caCert != string(caCertInfo.Cert) {
				t.Errorf("got caCert %q, wanted %q with err %v", string(caCertInfo.Cert), tt.caCert, err)
			}
		})
	}
}
